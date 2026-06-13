// zapclub Telegram bridge
//
// Bidirectional chat between a Telegram group and a zapclub NIP-29 club,
// plus queue management (/add, /np, /queue) from Telegram.
//
// The bot owns a zapclub club and acts as its permanent DJ:
//   - Club chat (kind 9) is forwarded to the Telegram group and vice-versa.
//   - now_playing (kind 30100) triggers a "🎵 Now playing" message on track change.
//   - /add <yt-url|query> appends a track to the bot's DJ queue (kind 30103).
//   - Kind 30102 heartbeats keep the bot on stage so the conductor plays its queue.
//
// Required env vars: BOT_TOKEN, TELEGRAM_CHAT_ID, BOT_NSEC, BOT_CLUB_ID
// Optional:          RELAY_URL (default wss://relay.zapclub.io)
//                    RELAY_API (default https://relay.zapclub.io)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip13"
	"github.com/nbd-wtf/go-nostr/nip19"
)

// ── Config ───────────────────────────────────────────────────────────────────

type Config struct {
	TGToken  string
	TGChatID int64
	Nsec     string
	ClubID   string
	RelayURL string
	RelayAPI string
}

func loadConfig() Config {
	chatID, _ := strconv.ParseInt(mustEnv("TELEGRAM_CHAT_ID"), 10, 64)
	return Config{
		TGToken:  mustEnv("BOT_TOKEN"),
		TGChatID: chatID,
		Nsec:     mustEnv("BOT_NSEC"),
		ClubID:   mustEnv("BOT_CLUB_ID"),
		RelayURL: envOr("RELAY_URL", "wss://relay.zapclub.io"),
		RelayAPI: envOr("RELAY_API", "https://relay.zapclub.io"),
	}
}

// ── Track ─────────────────────────────────────────────────────────────────────

type Track struct {
	VideoID  string
	Title    string
	Duration int
}

// ── Bridge ────────────────────────────────────────────────────────────────────

type Bridge struct {
	cfg      Config
	sk       string // hex secret key
	pk       string // hex public key
	tg       *tgbotapi.BotAPI
	mu       sync.Mutex
	profiles map[string]string // pubkey → display name cache
	lastNP   string            // last now-playing track ref (e.g. "yt:abc") — dedup
	ownIDs   map[string]bool   // event IDs we published — avoid echo
	queue    []Track           // current bot DJ queue
	since    int64             // stage join time (ms) — persisted across heartbeats
}

func newBridge(cfg Config) *Bridge {
	_, raw, err := nip19.Decode(cfg.Nsec)
	if err != nil {
		log.Fatalf("decode BOT_NSEC: %v", err)
	}
	sk := raw.(string)
	pk, err := nostr.GetPublicKey(sk)
	if err != nil {
		log.Fatalf("derive pubkey: %v", err)
	}
	log.Printf("[bot] nostr pubkey: %s…", pk[:16])

	tg, err := tgbotapi.NewBotAPI(cfg.TGToken)
	if err != nil {
		log.Fatalf("telegram init: %v", err)
	}
	log.Printf("[bot] telegram: @%s", tg.Self.UserName)

	return &Bridge{
		cfg:      cfg,
		sk:       sk,
		pk:       pk,
		tg:       tg,
		profiles: make(map[string]string),
		ownIDs:   make(map[string]bool),
		since:    time.Now().UnixMilli(),
	}
}

// run reconnects to the relay indefinitely.
func (b *Bridge) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := b.session(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[bot] relay disconnected: %v — retry in 15s", err)
			select {
			case <-time.After(15 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}
}

func (b *Bridge) session(ctx context.Context) error {
	relay, err := nostr.RelayConnect(ctx, b.cfg.RelayURL)
	if err != nil {
		return err
	}
	defer relay.Close()
	log.Printf("[bot] connected to %s", b.cfg.RelayURL)

	// NIP-42 AUTH — wait up to 2 s for relay to send the challenge, then respond
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := relay.Auth(ctx, func(event *nostr.Event) error {
		event.PubKey = b.pk
		return event.Sign(b.sk)
	}); err != nil {
		log.Printf("[bot] AUTH err: %v", err)
	}

	// Join club if not already a member (NIP-29 open clubs: auto-approved)
	b.joinClub(ctx, relay)

	// Load bot's existing queue from relay
	b.loadQueue(ctx, relay)

	// Subscribe to club chat + now_playing
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds: []int{9, 30100},
			Tags:  nostr.TagMap{"h": {b.cfg.ClubID}},
			Limit: 0,
		},
	})
	if err != nil {
		return err
	}

	// Stage heartbeat — keep bot on stage as DJ
	go b.stageLoop(ctx, relay)

	// Telegram → relay
	go b.tgLoop(ctx, relay)

	// Relay → Telegram
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-sub.Events:
			if !ok {
				return fmt.Errorf("subscription closed")
			}
			b.handleNostrEvent(ctx, ev)
		case <-sub.EndOfStoredEvents:
			// ignore EOSE
		}
	}
}

// ── Relay → Telegram ─────────────────────────────────────────────────────────

func (b *Bridge) handleNostrEvent(ctx context.Context, ev *nostr.Event) {
	switch ev.Kind {
	case 9: // chat
		b.mu.Lock()
		own := ev.PubKey == b.pk || b.ownIDs[ev.ID]
		b.mu.Unlock()
		if own {
			return
		}
		name := b.displayName(ctx, ev.PubKey)
		text := fmt.Sprintf("💬 *%s*: %s", escMD(name), escMD(ev.Content))
		msg := tgbotapi.NewMessage(b.cfg.TGChatID, text)
		msg.ParseMode = "MarkdownV2"
		if _, err := b.tg.Send(msg); err != nil {
			log.Printf("[bot] tg send err: %v", err)
		}

	case 30100: // now_playing
		trackTag := ev.Tags.GetFirst([]string{"track"})
		if trackTag == nil || len(*trackTag) < 2 {
			return
		}
		ref := (*trackTag)[1]
		b.mu.Lock()
		changed := b.lastNP != ref
		b.lastNP = ref
		b.mu.Unlock()
		if !changed {
			return // heartbeat with same track — suppress
		}
		title := ev.Content
		if title == "" {
			title = strings.TrimPrefix(ref, "yt:")
		}
		text := "🎵 *Now playing:* " + escMD(title)
		msg := tgbotapi.NewMessage(b.cfg.TGChatID, text)
		msg.ParseMode = "MarkdownV2"
		_, _ = b.tg.Send(msg)
	}
}

// ── Telegram → Relay ─────────────────────────────────────────────────────────

func (b *Bridge) tgLoop(ctx context.Context, relay *nostr.Relay) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	ch := b.tg.GetUpdatesChan(u)
	for {
		select {
		case <-ctx.Done():
			return
		case upd, ok := <-ch:
			if !ok {
				return
			}
			if upd.Message == nil || upd.Message.Chat.ID != b.cfg.TGChatID {
				continue
			}
			b.handleTGMessage(ctx, relay, upd.Message)
		}
	}
}

func (b *Bridge) handleTGMessage(ctx context.Context, relay *nostr.Relay, msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	from := msg.From.FirstName
	if msg.From.UserName != "" {
		from = "@" + msg.From.UserName
	}

	// Command handling
	cmd, arg, _ := strings.Cut(text, " ")
	cmd = strings.SplitN(cmd, "@", 2)[0] // strip @botname suffix

	switch cmd {
	case "/start", "/help":
		b.cmdHelp(msg.Chat.ID)
	case "/add":
		b.cmdAdd(ctx, relay, msg.Chat.ID, from, strings.TrimSpace(arg))
	case "/np":
		b.cmdNP(msg.Chat.ID)
	case "/queue":
		b.cmdQueue(msg.Chat.ID)
	default:
		if strings.HasPrefix(text, "/") {
			return // unknown command — ignore silently
		}
		// Regular message → club chat
		b.postChat(ctx, relay, fmt.Sprintf("[TG:%s] %s", from, text))
	}
}

// ── Commands ──────────────────────────────────────────────────────────────────

func (b *Bridge) cmdAdd(ctx context.Context, relay *nostr.Relay, chatID int64, from, query string) {
	reply := func(s string) { _, _ = b.tg.Send(tgbotapi.NewMessage(chatID, s)) }

	if query == "" {
		reply("Usage: /add <YouTube URL or search query>")
		return
	}
	reply("🔍 Searching…")

	tracks, err := b.ytSearch(ctx, query)
	if err != nil || len(tracks) == 0 {
		reply("❌ Nothing found for: " + query)
		return
	}
	t := tracks[0]

	b.mu.Lock()
	b.queue = append(b.queue, t)
	q := make([]Track, len(b.queue))
	copy(q, b.queue)
	b.mu.Unlock()

	if err := b.publishQueue(ctx, relay, q); err != nil {
		reply("❌ Relay error: " + err.Error())
		return
	}
	reply(fmt.Sprintf("✅ Added by %s:\n%s", from, t.Title))
}

func (b *Bridge) cmdNP(chatID int64) {
	b.mu.Lock()
	np := b.lastNP
	b.mu.Unlock()
	if np == "" {
		_, _ = b.tg.Send(tgbotapi.NewMessage(chatID, "Nothing playing right now."))
		return
	}
	vid := strings.TrimPrefix(np, "yt:")
	_, _ = b.tg.Send(tgbotapi.NewMessage(chatID, "🎵 https://youtu.be/"+vid))
}

func (b *Bridge) cmdQueue(chatID int64) {
	b.mu.Lock()
	q := make([]Track, len(b.queue))
	copy(q, b.queue)
	b.mu.Unlock()

	if len(q) == 0 {
		_, _ = b.tg.Send(tgbotapi.NewMessage(chatID, "Queue is empty. Use /add to add tracks."))
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 Queue (%d tracks):\n", len(q)))
	for i, t := range q {
		if i >= 20 {
			fmt.Fprintf(&sb, "… and %d more\n", len(q)-20)
			break
		}
		fmt.Fprintf(&sb, "%d. %s\n", i+1, t.Title)
	}
	_, _ = b.tg.Send(tgbotapi.NewMessage(chatID, sb.String()))
}

func (b *Bridge) cmdHelp(chatID int64) {
	help := "🎧 *zapclub\\.io* — Sunnyhill Basement\n\n" +
		"I'm the club DJ\\. Add tracks, I'll play them in the mix\\.\n\n" +
		"*/add* _<YouTube URL or search query>_\n" +
		"*/np* — current track\n" +
		"*/queue* — bot's queue\n\n" +
		"Any other message is forwarded to the club chat\\.\n" +
		"👉 https://zapclub\\.io/club/c6f0f845fb2a3792"
	msg := tgbotapi.NewMessage(chatID, help)
	msg.ParseMode = "MarkdownV2"
	_, _ = b.tg.Send(msg)
}

// ── Nostr publishing ──────────────────────────────────────────────────────────

// postChat publishes a kind 9 chat message with NIP-13 PoW (relay requires 10 bits).
func (b *Bridge) postChat(ctx context.Context, relay *nostr.Relay, text string) {
	ev := nostr.Event{
		PubKey:    b.pk,
		Kind:      9,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      nostr.Tags{{"h", b.cfg.ClubID}},
		Content:   text,
	}
	// Mine PoW first (relay requires 10 bits for kind 9)
	if powTag, err := nip13.DoWork(ctx, ev, 10); err != nil {
		log.Printf("[bot] pow err: %v", err)
	} else {
		ev.Tags = append(ev.Tags, powTag)
	}
	if err := ev.Sign(b.sk); err != nil {
		log.Printf("[bot] sign err: %v", err)
		return
	}
	b.mu.Lock()
	b.ownIDs[ev.ID] = true
	// Trim ownIDs to last 100 entries to avoid unbounded growth
	if len(b.ownIDs) > 100 {
		for id := range b.ownIDs {
			delete(b.ownIDs, id)
			break
		}
	}
	b.mu.Unlock()
	if err := relay.Publish(ctx, ev); err != nil {
		log.Printf("[bot] chat publish err: %v", err)
	}
}

// publishQueue sends the bot's kind 30103 DJ queue to the relay.
func (b *Bridge) publishQueue(ctx context.Context, relay *nostr.Relay, tracks []Track) error {
	tags := nostr.Tags{
		{"h", b.cfg.ClubID},
		{"d", b.cfg.ClubID},
	}
	for _, t := range tracks {
		tags = append(tags, nostr.Tag{
			"track",
			"yt:" + t.VideoID,
			t.Title,
			strconv.Itoa(t.Duration),
		})
	}
	ev := nostr.Event{
		PubKey:    b.pk,
		Kind:      30103,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}
	_ = ev.Sign(b.sk)
	return relay.Publish(ctx, ev)
}

// stageLoop sends kind 30102 heartbeats every 25 s to keep the bot on stage.
func (b *Bridge) stageLoop(ctx context.Context, relay *nostr.Relay) {
	b.publishStage(ctx, relay)
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			b.publishStage(ctx, relay)
		}
	}
}

func (b *Bridge) publishStage(ctx context.Context, relay *nostr.Relay) {
	ev := nostr.Event{
		PubKey:    b.pk,
		Kind:      30102,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", b.cfg.ClubID},
			{"d", b.cfg.ClubID},
			{"since", strconv.FormatInt(b.since, 10)},
		},
		Content: "",
	}
	_ = ev.Sign(b.sk)
	if err := relay.Publish(ctx, ev); err != nil {
		log.Printf("[bot] stage heartbeat err: %v", err)
	}
}

// joinClub sends a NIP-29 kind 9021 join-request so the bot becomes a member
// of the club. For open clubs the relay approves automatically. Safe to call
// every session — redundant joins are ignored.
func (b *Bridge) joinClub(ctx context.Context, relay *nostr.Relay) {
	ev := nostr.Event{
		PubKey:    b.pk,
		Kind:      9021,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      nostr.Tags{{"h", b.cfg.ClubID}},
		Content:   "",
	}
	_ = ev.Sign(b.sk)
	if err := relay.Publish(ctx, ev); err != nil {
		// "duplicate: already a member" is expected — not a real error
		log.Printf("[bot] join club: %v", err)
	} else {
		log.Printf("[bot] joined club %s", b.cfg.ClubID)
	}
}

// ── Queue: load existing from relay on connect ────────────────────────────────

func (b *Bridge) loadQueue(ctx context.Context, relay *nostr.Relay) {
	lCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	sub, err := relay.Subscribe(lCtx, []nostr.Filter{{
		Kinds:   []int{30103},
		Authors: []string{b.pk},
		Tags:    nostr.TagMap{"d": {b.cfg.ClubID}},
		Limit:   1,
	}})
	if err != nil {
		return
	}
	defer sub.Unsub()

	select {
	case ev := <-sub.Events:
		var q []Track
		for _, tag := range ev.Tags {
			if len(tag) >= 4 && tag[0] == "track" && strings.HasPrefix(tag[1], "yt:") {
				dur, _ := strconv.Atoi(tag[3])
				active := len(tag) < 5 || tag[4] != "off"
				if active {
					q = append(q, Track{
						VideoID:  strings.TrimPrefix(tag[1], "yt:"),
						Title:    tag[2],
						Duration: dur,
					})
				}
			}
		}
		b.mu.Lock()
		b.queue = q
		b.mu.Unlock()
		log.Printf("[bot] loaded queue: %d active tracks", len(q))
	case <-lCtx.Done():
		log.Println("[bot] no existing queue found")
	}

	// Also try to load existing stage event to preserve `since`
	sub2, err := relay.Subscribe(lCtx, []nostr.Filter{{
		Kinds:   []int{30102},
		Authors: []string{b.pk},
		Tags:    nostr.TagMap{"d": {b.cfg.ClubID}},
		Limit:   1,
	}})
	if err != nil {
		return
	}
	defer sub2.Unsub()
	select {
	case ev := <-sub2.Events:
		if sinceTag := ev.Tags.GetFirst([]string{"since"}); sinceTag != nil && len(*sinceTag) >= 2 {
			if s, err := strconv.ParseInt((*sinceTag)[1], 10, 64); err == nil {
				b.since = s
				log.Printf("[bot] restored since: %d", s)
			}
		}
	case <-lCtx.Done():
	}
}

// ── YouTube search ────────────────────────────────────────────────────────────

var ytIDRe = regexp.MustCompile(`(?:v=|youtu\.be/|shorts/|embed/)([a-zA-Z0-9_-]{11})`)

type searchResult struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Duration int    `json:"duration"`
}

func (b *Bridge) ytSearch(ctx context.Context, query string) ([]Track, error) {
	// Direct YouTube URL → extract video ID, enrich title via oEmbed
	if m := ytIDRe.FindStringSubmatch(query); m != nil {
		vid := m[1]
		apiURL := b.cfg.RelayAPI + "/yt-search?ids=" + vid
		tracks, err := b.fetchSearch(ctx, apiURL)
		if err == nil && len(tracks) > 0 {
			return tracks, nil
		}
		return []Track{{VideoID: vid, Title: vid, Duration: 0}}, nil
	}
	// Text search
	apiURL := b.cfg.RelayAPI + "/yt-search?q=" + url.QueryEscape(query)
	return b.fetchSearch(ctx, apiURL)
}

func (b *Bridge) fetchSearch(ctx context.Context, apiURL string) ([]Track, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Response is []searchResult
	var results []searchResult
	if err := json.Unmarshal(body, &results); err == nil && len(results) > 0 {
		out := make([]Track, len(results))
		for i, r := range results {
			out[i] = Track{VideoID: r.ID, Title: r.Title, Duration: r.Duration}
		}
		return out, nil
	}
	// oEmbed enrichment response: map[videoId]title
	var m map[string]string
	if err := json.Unmarshal(body, &m); err == nil && len(m) > 0 {
		out := make([]Track, 0, len(m))
		for id, title := range m {
			out = append(out, Track{VideoID: id, Title: title})
		}
		return out, nil
	}
	n := len(body)
	if n > 120 {
		n = 120
	}
	return nil, fmt.Errorf("unexpected response: %s", body[:n])
}

// ── Display name lookup ───────────────────────────────────────────────────────

func (b *Bridge) displayName(ctx context.Context, pk string) string {
	b.mu.Lock()
	name, ok := b.profiles[pk]
	b.mu.Unlock()
	if ok {
		return name
	}
	go b.fetchProfile(pk)
	return pk[:8] + "…"
}

func (b *Bridge) fetchProfile(pk string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	r, err := nostr.RelayConnect(ctx, "wss://relay.nostr.band")
	if err != nil {
		return
	}
	defer r.Close()

	sub, err := r.Subscribe(ctx, []nostr.Filter{{
		Kinds:   []int{0},
		Authors: []string{pk},
		Limit:   1,
	}})
	if err != nil {
		return
	}
	defer sub.Unsub()

	select {
	case ev := <-sub.Events:
		var meta struct {
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
		}
		if err := json.Unmarshal([]byte(ev.Content), &meta); err != nil {
			return
		}
		name := meta.DisplayName
		if name == "" {
			name = meta.Name
		}
		if name != "" {
			b.mu.Lock()
			b.profiles[pk] = name
			b.mu.Unlock()
		}
	case <-ctx.Done():
	}
}

// ── Telegram MarkdownV2 escaping ──────────────────────────────────────────────

var mdReplacer = strings.NewReplacer(
	`_`, `\_`, `*`, `\*`, `[`, `\[`, `]`, `\]`,
	`(`, `\(`, `)`, `\)`, `~`, `\~`, "`", "\\`",
	`>`, `\>`, `#`, `\#`, `+`, `\+`, `-`, `\-`,
	`=`, `\=`, `|`, `\|`, `{`, `\{`, `}`, `\}`,
	`.`, `\.`, `!`, `\!`,
)

func escMD(s string) string { return mdReplacer.Replace(s) }

// ── Helpers ───────────────────────────────────────────────────────────────────

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("required env var %s not set", k)
	}
	return v
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// ── genkey ────────────────────────────────────────────────────────────────────

// genkey prints a fresh Nostr keypair (nsec + npub) for use as BOT_NSEC.
func genkey() {
	sk := nostr.GeneratePrivateKey()
	pk, err := nostr.GetPublicKey(sk)
	if err != nil {
		log.Fatalf("genkey: %v", err)
	}
	nsec, _ := nip19.EncodePrivateKey(sk)
	npub, _ := nip19.EncodePublicKey(pk)
	fmt.Printf("BOT_NSEC=%s\n", nsec)
	fmt.Printf("# npub (read-only public key):\n# %s\n", npub)
	fmt.Printf("# hex pubkey:\n# %s\n", pk)
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	if len(os.Args) > 1 && os.Args[1] == "genkey" {
		genkey()
		return
	}

	cfg := loadConfig()
	b := newBridge(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Println("[bot] starting zapclub telegram bridge")
	b.run(ctx)
	log.Println("[bot] stopped")
}

// zapclub Telegram bridge
//
// One-way bridge: now_playing events notify the Telegram group; queue management
// (/add, /np, /queue) works from Telegram. No chat mirroring in either direction.
//
// The bot owns a zapclub club and acts as its permanent DJ:
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
	"github.com/nbd-wtf/go-nostr/nip19"
)

// ── Config ───────────────────────────────────────────────────────────────────

type Config struct {
	TGToken    string
	TGChatID   int64
	Nsec       string
	ClubID     string
	RelayURL   string
	RelayAPI   string
	NWCUrl     string // optional: nostr+walletconnect://... — enables payment gate
	TrackPrice int64  // sats per track (default 10), used when NWCUrl is set
}

func loadConfig() Config {
	chatID, _ := strconv.ParseInt(mustEnv("TELEGRAM_CHAT_ID"), 10, 64)
	price, _ := strconv.ParseInt(envOr("TRACK_PRICE", "10"), 10, 64)
	if price <= 0 {
		price = 10
	}
	return Config{
		TGToken:    mustEnv("BOT_TOKEN"),
		TGChatID:   chatID,
		Nsec:       mustEnv("BOT_NSEC"),
		ClubID:     mustEnv("BOT_CLUB_ID"),
		RelayURL:   envOr("RELAY_URL", "wss://relay.zapclub.io"),
		RelayAPI:   envOr("RELAY_API", "https://relay.zapclub.io"),
		NWCUrl:     os.Getenv("BOT_NWC"),
		TrackPrice: price,
	}
}

// ── Track ─────────────────────────────────────────────────────────────────────

type Track struct {
	VideoID  string
	Title    string
	Duration int
}

// ── Bridge ────────────────────────────────────────────────────────────────────

// PendingPayment holds tracks queued behind a Lightning invoice awaiting payment.
type PendingPayment struct {
	ChatID int64
	From   string
	Tracks []Track
	Hash   string
}

type Bridge struct {
	cfg             Config
	sk              string // hex secret key
	pk              string // hex public key
	tg              *tgbotapi.BotAPI
	mu              sync.Mutex
	relay           *nostr.Relay // current live relay — nil while reconnecting
	profiles        map[string]string
	clubName        string
	lastNP          string
	lastTitle       string
	queue           []Track
	since           int64
	onStage         bool
	pendingSearches map[int64][]Track
	pendingFrom     map[int64]string
	nwc             *NWCClient
	pendingPayments map[string]*PendingPayment // keyed by payment_hash
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

	var nwcClient *NWCClient
	if cfg.NWCUrl != "" {
		c, err := parseNWCURL(cfg.NWCUrl)
		if err != nil {
			log.Fatalf("invalid BOT_NWC: %v", err)
		}
		nwcClient = c
		log.Printf("[bot] NWC payment gate: %d sats/track", cfg.TrackPrice)
	}

	return &Bridge{
		cfg:             cfg,
		sk:              sk,
		pk:              pk,
		tg:              tg,
		profiles:        make(map[string]string),
		since:           time.Now().UnixMilli(),
		onStage:         true,
		pendingSearches: make(map[int64][]Track),
		pendingFrom:     make(map[int64]string),
		nwc:             nwcClient,
		pendingPayments: make(map[string]*PendingPayment),
	}
}

// getRelay returns the current relay or nil if disconnected.
func (b *Bridge) getRelay() *nostr.Relay {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.relay
}

func (b *Bridge) setRelay(r *nostr.Relay) {
	b.mu.Lock()
	b.relay = r
	b.mu.Unlock()
}

// run starts the persistent Telegram polling loop once, then reconnects the
// relay indefinitely in the foreground. Decoupling means a relay disconnect
// never breaks Telegram command handling.
func (b *Bridge) run(ctx context.Context) {
	// Telegram polling: one goroutine for the entire process lifetime.
	// Restarted internally if the update channel closes (e.g. 409 Conflict).
	go b.tgPollLoop(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := b.session(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[bot] relay disconnected: %v — retry in 15s", err)
			b.setRelay(nil)
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

	// NIP-42 AUTH
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

	b.publishProfile(ctx, relay)
	b.joinClub(ctx, relay)
	b.loadClubName(ctx, relay)
	b.loadQueue(ctx, relay)

	// Expose the live relay to command handlers.
	b.setRelay(relay)

	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds: []int{30100},
			Tags:  nostr.TagMap{"h": {b.cfg.ClubID}},
			Limit: 1,
		},
		{
			Kinds: []int{30103},
			Tags:  nostr.TagMap{"h": {b.cfg.ClubID}, "d": {b.cfg.ClubID}},
			Limit: 1,
		},
		{
			Kinds: []int{30103},
			Tags:  nostr.TagMap{"h": {b.cfg.ClubID}, "d": {b.pk + ":" + b.cfg.ClubID}},
			Limit: 1,
		},
	})
	if err != nil {
		b.setRelay(nil)
		return err
	}

	sesCtx, sesCancel := context.WithCancel(ctx)
	defer sesCancel()

	go b.stageLoop(sesCtx, relay)

	for {
		select {
		case <-ctx.Done():
			b.setRelay(nil)
			return nil
		case ev, ok := <-sub.Events:
			if !ok {
				b.setRelay(nil)
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
	case 30100:
		trackTag := ev.Tags.GetFirst([]string{"track"})
		if trackTag == nil || len(*trackTag) < 2 {
			return
		}
		ref := (*trackTag)[1]
		title := ev.Content
		if title == "" {
			title = strings.TrimPrefix(ref, "yt:")
		}
		b.mu.Lock()
		b.lastNP = ref
		b.lastTitle = title
		b.mu.Unlock()

	case 30103:
		// Ignore relay-authored markTrackOff updates (different pubkey).
		// Only react to our own queue events so we don't leave stage the
		// instant the conductor marks a track off on our behalf.
		if ev.PubKey != b.pk {
			break
		}
		total, active := 0, 0
		for _, t := range ev.Tags {
			if len(t) >= 2 && t[0] == "track" {
				total++
				if len(t) < 5 || t[4] != "off" {
					active++
				}
			}
		}
		if total > 0 && active == 0 {
			b.mu.Lock()
			wasOn := b.onStage
			b.onStage = false
			b.mu.Unlock()
			if wasOn {
				log.Printf("[bot] queue exhausted (total=%d all off) — leaving stage", total)
				b.leaveStage(ctx, nil) // nil → falls back to getRelay(), which is set by then
			}
		}
	}
}

// ── Telegram polling — persistent, independent of relay sessions ──────────────

// tgPollLoop runs for the lifetime of the process. If the updates channel
// closes (e.g. Telegram 409 Conflict on startup overlap), it restarts after
// a short back-off.
func (b *Bridge) tgPollLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		log.Printf("[bot] TG polling: starting GetUpdatesChan")
		u := tgbotapi.NewUpdate(0)
		u.Timeout = 60
		ch := b.tg.GetUpdatesChan(u)

		closed := false
		for !closed {
			select {
			case <-ctx.Done():
				b.tg.StopReceivingUpdates()
				return
			case upd, ok := <-ch:
				if !ok {
					log.Printf("[bot] TG updates channel closed — restarting in 5s")
					closed = true
					break
				}
				if upd.CallbackQuery != nil {
					go b.handleCallback(ctx, upd.CallbackQuery)
					continue
				}
				if upd.Message == nil {
					continue
				}
				log.Printf("[bot] TG msg from chat=%d user=%s: %q",
					upd.Message.Chat.ID, upd.Message.From.UserName, upd.Message.Text)
				if upd.Message.Chat.ID != b.cfg.TGChatID {
					log.Printf("[bot] TG msg ignored: chat %d != configured %d",
						upd.Message.Chat.ID, b.cfg.TGChatID)
					continue
				}
				go b.handleTGMessage(ctx, upd.Message)
			}
		}

		b.tg.StopReceivingUpdates()
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bridge) handleTGMessage(ctx context.Context, msg *tgbotapi.Message) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	from := msg.From.FirstName
	if msg.From.UserName != "" {
		from = "@" + msg.From.UserName
	}

	cmd, arg, _ := strings.Cut(text, " ")
	cmd = strings.SplitN(cmd, "@", 2)[0]

	log.Printf("[bot] TG cmd=%q arg=%q from=%s", cmd, arg, from)

	relay := b.getRelay()

	switch cmd {
	case "/start", "/help":
		b.cmdHelp(msg.Chat.ID)
	case "/add":
		if relay == nil {
			b.send(msg.Chat.ID, "⏳ Reconnecting to relay… try again in a moment.")
			return
		}
		b.cmdAdd(ctx, relay, msg.Chat.ID, from, strings.TrimSpace(arg))
	case "/addplaylist", "/addlist":
		if relay == nil {
			b.send(msg.Chat.ID, "⏳ Reconnecting to relay… try again in a moment.")
			return
		}
		b.cmdAddPlaylist(ctx, relay, msg.Chat.ID, from, strings.TrimSpace(arg))
	case "/np":
		b.cmdNP(msg.Chat.ID)
	case "/queue":
		b.cmdQueue(msg.Chat.ID)
	case "/remove":
		if relay == nil {
			b.send(msg.Chat.ID, "⏳ Reconnecting to relay… try again in a moment.")
			return
		}
		b.cmdRemove(ctx, relay, msg.Chat.ID, strings.TrimSpace(arg))
	}
}

func (b *Bridge) send(chatID int64, text string) {
	_, _ = b.tg.Send(tgbotapi.NewMessage(chatID, text))
}

// ── Commands ──────────────────────────────────────────────────────────────────

func (b *Bridge) cmdAdd(ctx context.Context, relay *nostr.Relay, chatID int64, from, query string) {
	if query == "" {
		b.send(chatID, "Usage: /add <YouTube URL or search query>")
		return
	}

	if ytIDRe.MatchString(query) {
		b.send(chatID, "🔍 Looking up…")
		tracks, err := b.ytSearch(ctx, query)
		if err != nil || len(tracks) == 0 {
			b.send(chatID, "❌ Could not fetch video info.")
			return
		}
		if b.nwc != nil {
			b.requestPayment(ctx, chatID, from, []Track{tracks[0]})
		} else {
			b.addTrack(ctx, relay, chatID, from, tracks[0], true)
		}
		return
	}

	log.Printf("[bot] search: %q", query)
	b.send(chatID, "🔍 Searching…")
	tracks, err := b.ytSearch(ctx, query)
	if err != nil || len(tracks) == 0 {
		log.Printf("[bot] search fail: %q err=%v tracks=%d", query, err, len(tracks))
		b.send(chatID, "❌ Nothing found for: "+query)
		return
	}
	n := len(tracks)
	if n > 5 {
		n = 5
	}
	results := tracks[:n]

	b.mu.Lock()
	b.pendingSearches[chatID] = results
	b.pendingFrom[chatID] = from
	b.mu.Unlock()

	var rows [][]tgbotapi.InlineKeyboardButton
	for i, t := range results {
		label := fmt.Sprintf("%d. %s", i+1, t.Title)
		if len(label) > 64 {
			label = label[:61] + "…"
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("add:%d", i)),
		))
	}
	prompt := "Pick a track:"
	if b.nwc != nil {
		prompt = fmt.Sprintf("Pick a track (%d sats):", b.cfg.TrackPrice)
	}
	msg := tgbotapi.NewMessage(chatID, prompt)
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	_, _ = b.tg.Send(msg)
}

func (b *Bridge) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	ack := tgbotapi.NewCallback(cb.ID, "")
	_, _ = b.tg.Request(ack)

	if !strings.HasPrefix(cb.Data, "add:") {
		return
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(cb.Data, "add:"))
	if err != nil {
		return
	}

	chatID := cb.Message.Chat.ID
	b.mu.Lock()
	results := b.pendingSearches[chatID]
	from := b.pendingFrom[chatID]
	b.mu.Unlock()

	if results == nil || idx < 0 || idx >= len(results) {
		b.send(chatID, "❌ Selection expired. Search again.")
		return
	}

	b.mu.Lock()
	delete(b.pendingSearches, chatID)
	delete(b.pendingFrom, chatID)
	b.mu.Unlock()

	if b.nwc != nil {
		// Edit message to show we're generating the invoice, then send payment request.
		editText := fmt.Sprintf("⚡ Creating invoice for:\n%s", results[idx].Title)
		edit := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, editText)
		_, _ = b.tg.Send(edit)
		b.requestPayment(ctx, chatID, from, []Track{results[idx]})
		return
	}

	relay := b.getRelay()
	if relay == nil {
		b.send(chatID, "⏳ Reconnecting to relay… try again in a moment.")
		return
	}

	b.mu.Lock()
	clubName := b.clubName
	b.mu.Unlock()
	club := clubName
	if club == "" {
		club = b.cfg.ClubID
	}
	editText := fmt.Sprintf("✅ Added by %s:\n%s\n\n📍 %s\nhttps://zapclub.io/club/%s",
		from, results[idx].Title, club, b.cfg.ClubID)
	edit := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, editText)
	_, _ = b.tg.Send(edit)

	b.addTrack(ctx, relay, chatID, from, results[idx], false)
}

func (b *Bridge) addTrack(ctx context.Context, relay *nostr.Relay, chatID int64, from string, t Track, notify bool) {
	b.mu.Lock()
	wasOffStage := !b.onStage
	b.mu.Unlock()

	var current []Track
	if !wasOffStage {
		current, _ = b.fetchCurrentTracks(ctx, relay)
	}

	b.mu.Lock()
	before := len(b.queue)
	if wasOffStage {
		b.queue = nil
	} else if current != nil {
		b.queue = current
	}
	b.queue = append(b.queue, t)
	b.onStage = true
	q := make([]Track, len(b.queue))
	copy(q, b.queue)
	b.mu.Unlock()
	log.Printf("[bot] addTrack: relay=%d mem=%d after=%d new=%s wasOff=%v", len(current), before, len(q), t.VideoID, wasOffStage)

	if err := b.publishQueue(ctx, relay, q); err != nil {
		b.send(chatID, "❌ Relay error: "+err.Error())
		return
	}
	if wasOffStage {
		log.Printf("[bot] rejoining stage after new track added")
		b.publishStage(ctx, relay)
	}
	b.mu.Lock()
	clubName := b.clubName
	b.mu.Unlock()
	club := clubName
	if club == "" {
		club = b.cfg.ClubID
	}
	if notify {
		msg := fmt.Sprintf("✅ Added by %s:\n%s\n\n📍 %s\nhttps://zapclub.io/club/%s",
			from, t.Title, club, b.cfg.ClubID)
		b.send(chatID, msg)
	}
}

func (b *Bridge) cmdAddPlaylist(ctx context.Context, relay *nostr.Relay, chatID int64, from, arg string) {
	if arg == "" {
		b.send(chatID, "Usage: /addplaylist <YouTube playlist URL>")
		return
	}
	m := ytListRe.FindStringSubmatch(arg)
	if m == nil {
		b.send(chatID, "❌ No playlist ID found in URL. Use a youtube.com/playlist?list=… link.")
		return
	}
	listID := m[1]
	b.send(chatID, fmt.Sprintf("📋 Fetching playlist %s…", listID))

	apiURL := b.cfg.RelayAPI + "/yt-playlist?list=" + listID
	tracks, err := b.fetchSearch(ctx, apiURL)
	if err != nil || len(tracks) == 0 {
		b.send(chatID, "❌ Could not load playlist. Make sure it's public.")
		return
	}

	if b.nwc != nil {
		sats := int64(len(tracks)) * b.cfg.TrackPrice
		b.send(chatID, fmt.Sprintf("📋 %d tracks found — costs %d sats total.", len(tracks), sats))
		b.requestPayment(ctx, chatID, from, tracks)
		return
	}

	current, _ := b.fetchCurrentTracks(ctx, relay)
	b.mu.Lock()
	if current != nil {
		b.queue = current
	}
	b.queue = append(b.queue, tracks...)
	q := make([]Track, len(b.queue))
	copy(q, b.queue)
	b.mu.Unlock()

	if err := b.publishQueue(ctx, relay, q); err != nil {
		b.send(chatID, "❌ Relay error: "+err.Error())
		return
	}
	b.mu.Lock()
	clubName := b.clubName
	b.mu.Unlock()
	club := clubName
	if club == "" {
		club = b.cfg.ClubID
	}
	b.send(chatID, fmt.Sprintf("✅ Added %d tracks by %s\n\n📍 %s\nhttps://zapclub.io/club/%s",
		len(tracks), from, club, b.cfg.ClubID))
}

// ── NWC payment gate ──────────────────────────────────────────────────────────

// requestPayment creates a Lightning invoice via NWC, sends it to the chat,
// and starts a background goroutine polling for payment confirmation.
func (b *Bridge) requestPayment(ctx context.Context, chatID int64, from string, tracks []Track) {
	sats := int64(len(tracks)) * b.cfg.TrackPrice
	desc := fmt.Sprintf("zapclub: %s adding %d track(s)", from, len(tracks))

	invoice, hash, err := b.nwc.MakeInvoice(ctx, sats, desc)
	if err != nil {
		b.send(chatID, "❌ Could not create invoice: "+err.Error())
		return
	}
	log.Printf("[nwc] invoice created hash=%.8s… sats=%d from=%s tracks=%d", hash, sats, from, len(tracks))

	var lines strings.Builder
	for i, t := range tracks {
		if i >= 3 {
			fmt.Fprintf(&lines, "… and %d more", len(tracks)-3)
			break
		}
		if i > 0 {
			lines.WriteByte('\n')
		}
		lines.WriteString("• " + t.Title)
	}

	noun := "track"
	if len(tracks) > 1 {
		noun = fmt.Sprintf("%d tracks", len(tracks))
	}
	text := fmt.Sprintf("⚡ Pay <b>%d sats</b> to add %s:\n%s\n\n⏰ Expires in 5 minutes.",
		sats, noun, lines.String())
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("⚡ Pay with Alby Go", "alby:"+invoice),
		),
	)
	_, _ = b.tg.Send(msg)

	// Send invoice as a separate message for easy copy-paste.
	inv := tgbotapi.NewMessage(chatID, "<code>"+invoice+"</code>")
	inv.ParseMode = "HTML"
	inv.DisableWebPagePreview = true
	_, _ = b.tg.Send(inv)

	b.mu.Lock()
	b.pendingPayments[hash] = &PendingPayment{ChatID: chatID, From: from, Tracks: tracks, Hash: hash}
	b.mu.Unlock()

	go b.pollPayment(ctx, hash)
}

// pollPayment checks every 5 s whether the invoice has been paid, times out after 5 min.
func (b *Bridge) pollPayment(ctx context.Context, hash string) {
	timeout := time.After(5 * time.Minute)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timeout:
			b.mu.Lock()
			pp := b.pendingPayments[hash]
			delete(b.pendingPayments, hash)
			b.mu.Unlock()
			if pp != nil {
				b.send(pp.ChatID, "⏰ Payment timed out. Use /add again to retry.")
			}
			return
		case <-tick.C:
			settled, err := b.nwc.LookupInvoice(ctx, hash)
			if err != nil {
				log.Printf("[nwc] poll hash=%.8s… err=%v", hash, err)
				continue
			}
			log.Printf("[nwc] poll hash=%.8s… settled=%v", hash, settled)
			if !settled {
				continue
			}
			log.Printf("[nwc] payment confirmed hash=%.8s…", hash)
			b.onPaymentReceived(ctx, hash)
			return
		}
	}
}

// onPaymentReceived fires when a pending invoice is confirmed paid.
func (b *Bridge) onPaymentReceived(ctx context.Context, hash string) {
	b.mu.Lock()
	pp := b.pendingPayments[hash]
	delete(b.pendingPayments, hash)
	b.mu.Unlock()
	if pp == nil {
		return
	}
	log.Printf("[nwc] payment received: %d sats from %s (%d tracks)", int64(len(pp.Tracks))*b.cfg.TrackPrice, pp.From, len(pp.Tracks))

	relay := b.getRelay()
	if relay == nil {
		b.send(pp.ChatID, "⚡ Paid! Relay is reconnecting — tracks will be added shortly.")
		return
	}

	if len(pp.Tracks) == 1 {
		b.addTrack(ctx, relay, pp.ChatID, pp.From, pp.Tracks[0], false)
	} else {
		// Batch: sync from relay first, then append all tracks at once.
		b.mu.Lock()
		wasOffStage := !b.onStage
		b.mu.Unlock()
		var current []Track
		if !wasOffStage {
			current, _ = b.fetchCurrentTracks(ctx, relay)
		}
		b.mu.Lock()
		if wasOffStage {
			b.queue = nil
		} else if current != nil {
			b.queue = current
		}
		b.queue = append(b.queue, pp.Tracks...)
		b.onStage = true
		q := make([]Track, len(b.queue))
		copy(q, b.queue)
		b.mu.Unlock()
		if err := b.publishQueue(ctx, relay, q); err != nil {
			b.send(pp.ChatID, "❌ Relay error: "+err.Error())
			return
		}
		if wasOffStage {
			b.publishStage(ctx, relay)
		}
	}

	b.mu.Lock()
	clubName := b.clubName
	b.mu.Unlock()
	club := clubName
	if club == "" {
		club = b.cfg.ClubID
	}
	var confirm string
	if len(pp.Tracks) == 1 {
		confirm = fmt.Sprintf("✅ Added by %s:\n%s\n\n📍 %s\nhttps://zapclub.io/club/%s",
			pp.From, pp.Tracks[0].Title, club, b.cfg.ClubID)
	} else {
		confirm = fmt.Sprintf("✅ Added %d tracks by %s\n\n📍 %s\nhttps://zapclub.io/club/%s",
			len(pp.Tracks), pp.From, club, b.cfg.ClubID)
	}
	b.send(pp.ChatID, confirm)
}

func (b *Bridge) cmdRemove(ctx context.Context, relay *nostr.Relay, chatID int64, arg string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		b.send(chatID, "Usage: /remove <videoId>")
		return
	}
	if m := ytIDRe.FindStringSubmatch(arg); m != nil {
		arg = m[1]
	}
	current, _ := b.fetchCurrentTracks(ctx, relay)
	if current == nil {
		b.send(chatID, "❌ No queue found on relay")
		return
	}
	var next []Track
	removed := ""
	for _, t := range current {
		if t.VideoID == arg {
			removed = t.Title
			continue
		}
		next = append(next, t)
	}
	if removed == "" {
		b.send(chatID, fmt.Sprintf("❌ Track %s not found in queue (%d tracks)", arg, len(current)))
		return
	}
	b.mu.Lock()
	b.queue = next
	b.mu.Unlock()
	if err := b.publishQueue(ctx, relay, next); err != nil {
		b.send(chatID, "❌ Relay error: "+err.Error())
		return
	}
	log.Printf("[bot] /remove: vid=%s title=%s remaining=%d", arg, removed, len(next))
	b.send(chatID, fmt.Sprintf("🗑 Removed: %s\n%d tracks remaining", removed, len(next)))
}

func (b *Bridge) cmdNP(chatID int64) {
	b.mu.Lock()
	np := b.lastNP
	title := b.lastTitle
	clubName := b.clubName
	b.mu.Unlock()
	if np == "" {
		b.send(chatID, "Nothing playing right now.")
		return
	}
	vid := strings.TrimPrefix(np, "yt:")
	text := "🎵 " + title + "\n" + "https://youtu.be/" + vid
	if clubName != "" {
		text += "\n📍 " + clubName + " · zapclub.io"
	}
	b.send(chatID, text)
}

func (b *Bridge) cmdQueue(chatID int64) {
	b.mu.Lock()
	q := make([]Track, len(b.queue))
	copy(q, b.queue)
	b.mu.Unlock()

	if len(q) == 0 {
		b.send(chatID, "Queue is empty. Use /add to add tracks.")
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
	b.send(chatID, sb.String())
}

func (b *Bridge) cmdHelp(chatID int64) {
	addNote := ""
	if b.nwc != nil {
		addNote = fmt.Sprintf(" \\(%d sats per track\\)", b.cfg.TrackPrice)
	}
	help := "I'm the club DJ\\. Everyone in this chat can add tracks — I'll play them all in a shared mix\\.\n\n" +
		"*How to add a track:*\n" +
		"• */add* _search query_ — search YouTube and pick a track" + addNote + "\n" +
		"  _Example:_ `/add boards of canada`\n" +
		"• */add* _YouTube URL_ — add a specific video directly\n" +
		"  _Example:_ `/add https://youtu\\.be/abc123`\n" +
		"• */addplaylist* _playlist URL_ — add an entire YouTube playlist\n\n" +
		"*Other commands:*\n" +
		"• */np* — what's playing right now\n" +
		"• */queue* — see the current queue\n" +
		"• */remove* _videoId_ — remove a track from the queue\n\n" +
		"👉 Listen at zapclub\\.io/club/" + escMD(b.cfg.ClubID)
	msg := tgbotapi.NewMessage(chatID, help)
	msg.ParseMode = "MarkdownV2"
	msg.DisableWebPagePreview = true
	_, _ = b.tg.Send(msg)
}

// ── Nostr publishing ──────────────────────────────────────────────────────────

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

// publishProfile publishes a kind-0 metadata event to public Nostr relays marking this account
// as a bot. The frontend fetches profiles from public relays, not from our NIP-29 relay.
// Clients use the "bot" field to suppress the DJ stage-ring on the dancefloor.
func (b *Bridge) publishProfile(ctx context.Context, _ *nostr.Relay) {
	ev := nostr.Event{
		PubKey:    b.pk,
		Kind:      0,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      nostr.Tags{},
		Content:   `{"name":"zapclub bot","about":"Telegram music bot for zapclub.io","picture":"https://image.nostr.build/44e4467056b140f13f72cfbfe16e8e83fc3c252d2b5605786616e078344619f8.gif","bot":true}`,
	}
	_ = ev.Sign(b.sk)

	pubRelays := []string{"wss://nos.lol", "wss://offchain.pub"}
	pCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, url := range pubRelays {
		r, err := nostr.RelayConnect(pCtx, url)
		if err != nil {
			log.Printf("[bot] publishProfile connect %s: %v", url, err)
			continue
		}
		if err := r.Publish(pCtx, ev); err != nil {
			log.Printf("[bot] publishProfile publish %s: %v", url, err)
		}
		r.Close()
	}
}

func (b *Bridge) stageLoop(ctx context.Context, relay *nostr.Relay) {
	b.mu.Lock()
	on := b.onStage
	b.mu.Unlock()
	if on {
		b.publishStage(ctx, relay)
	}
	b.publishPresence(ctx, relay)
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			b.mu.Lock()
			on := b.onStage
			b.mu.Unlock()
			if on {
				b.publishStage(ctx, relay)
			}
			b.publishPresence(ctx, relay)
		}
	}
}

func (b *Bridge) publishPresence(ctx context.Context, relay *nostr.Relay) {
	ev := nostr.Event{
		PubKey:    b.pk,
		Kind:      20100,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", b.cfg.ClubID},
		},
	}
	_ = ev.Sign(b.sk)
	if err := relay.Publish(ctx, ev); err != nil {
		log.Printf("[bot] presence beat err: %v", err)
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
		Content: "on",
	}
	_ = ev.Sign(b.sk)
	if err := relay.Publish(ctx, ev); err != nil {
		log.Printf("[bot] stage heartbeat err: %v", err)
	}
}

// leaveStage publishes a 30102 event with content="off" so clients remove the bot from the
// stage display immediately instead of waiting for the heartbeat to go stale (~1h).
// Pass the relay explicitly — b.relay may not be set yet during startup (loadQueue runs
// before setRelay).
func (b *Bridge) leaveStage(ctx context.Context, relay *nostr.Relay) {
	if relay == nil {
		relay = b.getRelay()
	}
	if relay == nil {
		return
	}
	ev := nostr.Event{
		PubKey:    b.pk,
		Kind:      30102,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", b.cfg.ClubID},
			{"d", b.cfg.ClubID},
			{"since", strconv.FormatInt(b.since, 10)},
		},
		Content: "off",
	}
	_ = ev.Sign(b.sk)
	if err := relay.Publish(ctx, ev); err != nil {
		log.Printf("[bot] leave stage err: %v", err)
	} else {
		log.Println("[bot] left stage (published off event)")
	}
}

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
		log.Printf("[bot] join club: msg: %v", err)
	} else {
		log.Printf("[bot] joined club %s", b.cfg.ClubID)
	}
}

func (b *Bridge) loadClubName(ctx context.Context, relay *nostr.Relay) {
	lCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sub, err := relay.Subscribe(lCtx, []nostr.Filter{{
		Kinds: []int{39000},
		Tags:  nostr.TagMap{"d": {b.cfg.ClubID}},
		Limit: 1,
	}})
	if err != nil {
		return
	}
	defer sub.Unsub()
	select {
	case ev := <-sub.Events:
		if t := ev.Tags.GetFirst([]string{"name"}); t != nil && len(*t) >= 2 {
			b.mu.Lock()
			b.clubName = (*t)[1]
			b.mu.Unlock()
			log.Printf("[bot] club name: %s", b.clubName)
		}
	case <-lCtx.Done():
		log.Println("[bot] club name not found")
	}
}

// ── Queue: load existing from relay on connect ────────────────────────────────

func parseTracks(ev *nostr.Event) []Track {
	var q []Track
	for _, tag := range ev.Tags {
		if len(tag) >= 4 && tag[0] == "track" && strings.HasPrefix(tag[1], "yt:") {
			dur, _ := strconv.Atoi(tag[3])
			if len(tag) < 5 || tag[4] != "off" {
				q = append(q, Track{
					VideoID:  strings.TrimPrefix(tag[1], "yt:"),
					Title:    tag[2],
					Duration: dur,
				})
			}
		}
	}
	return q
}

// fetchCurrentTracks returns the active tracks from the bot's 30103 queue event.
// Returns (tracks, true) when an event was found; (nil, false) when no event exists.
// An empty slice with true means the event was found but all tracks are off.
func (b *Bridge) fetchCurrentTracks(ctx context.Context, relay *nostr.Relay) ([]Track, bool) {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	sub, err := relay.Subscribe(qCtx, []nostr.Filter{
		{Kinds: []int{30103}, Authors: []string{b.pk}, Tags: nostr.TagMap{"h": {b.cfg.ClubID}, "d": {b.cfg.ClubID}}, Limit: 1},
		{Kinds: []int{30103}, Tags: nostr.TagMap{"h": {b.cfg.ClubID}, "d": {b.pk + ":" + b.cfg.ClubID}}, Limit: 1},
	})
	if err != nil {
		return nil, false
	}
	defer sub.Unsub()

	var best *nostr.Event
drain:
	for {
		select {
		case ev := <-sub.Events:
			if ev != nil && (best == nil || ev.CreatedAt > best.CreatedAt) {
				best = ev
			}
		case <-sub.EndOfStoredEvents:
			break drain
		case <-qCtx.Done():
			break drain
		}
	}

	if best == nil {
		log.Println("[bot] fetchCurrentTracks: no event found")
		return nil, false
	}
	tracks := parseTracks(best)
	log.Printf("[bot] fetchCurrentTracks: pk=%.8s d=%s active=%d ts=%d",
		best.PubKey, best.Tags.GetFirst([]string{"d"}), len(tracks), int64(best.CreatedAt))
	return tracks, true
}

func (b *Bridge) loadQueue(ctx context.Context, relay *nostr.Relay) {
	q, found := b.fetchCurrentTracks(ctx, relay)
	if found && len(q) == 0 {
		// Event exists but all tracks are off — ensure we're off stage.
		// Always publish the off event regardless of local onStage state: the first
		// leave attempt may have failed (Transaction Conflict) and left the relay
		// believing we're still on stage.
		b.mu.Lock()
		b.onStage = false
		b.mu.Unlock()
		log.Println("[bot] queue exhausted on load — leaving stage")
		b.leaveStage(ctx, relay)
	} else if found {
		b.mu.Lock()
		b.queue = q
		b.mu.Unlock()
		log.Printf("[bot] loaded queue: %d active tracks", len(q))
	} else {
		log.Println("[bot] no existing queue found")
	}

	sCtx, sCancel := context.WithTimeout(ctx, 5*time.Second)
	defer sCancel()
	sub2, err := relay.Subscribe(sCtx, []nostr.Filter{{
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
		if ev == nil {
			break
		}
		if sinceTag := ev.Tags.GetFirst([]string{"since"}); sinceTag != nil && len(*sinceTag) >= 2 {
			if s, err := strconv.ParseInt((*sinceTag)[1], 10, 64); err == nil {
				b.since = s
				log.Printf("[bot] restored since: %d", s)
			}
		}
	case <-sCtx.Done():
	}
}

// ── YouTube search ────────────────────────────────────────────────────────────

var ytIDRe   = regexp.MustCompile(`(?:v=|youtu\.be/|shorts/|embed/)([a-zA-Z0-9_-]{11})`)
var ytListRe = regexp.MustCompile(`[?&]list=([A-Za-z0-9_-]{10,64})`)

type searchResult struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Duration int    `json:"duration"`
}

func (b *Bridge) ytSearch(ctx context.Context, query string) ([]Track, error) {
	if m := ytIDRe.FindStringSubmatch(query); m != nil {
		vid := m[1]
		apiURL := b.cfg.RelayAPI + "/yt-search?ids=" + vid
		tracks, err := b.fetchSearch(ctx, apiURL)
		if err == nil && len(tracks) > 0 {
			return tracks, nil
		}
		return []Track{{VideoID: vid, Title: vid, Duration: 0}}, nil
	}
	apiURL := b.cfg.RelayAPI + "/yt-search?q=" + url.QueryEscape(query)
	return b.fetchSearch(ctx, apiURL)
}

func (b *Bridge) fetchSearch(ctx context.Context, apiURL string) ([]Track, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[bot] search HTTP error: %v url=%s", err, apiURL)
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		n := len(body)
		if n > 120 {
			n = 120
		}
		log.Printf("[bot] search HTTP %d: %s url=%s", resp.StatusCode, body[:n], apiURL)
		return nil, fmt.Errorf("search error %d: %s", resp.StatusCode, body[:n])
	}

	var results []searchResult
	if err := json.Unmarshal(body, &results); err == nil && len(results) > 0 {
		out := make([]Track, len(results))
		for i, r := range results {
			out[i] = Track{VideoID: r.ID, Title: r.Title, Duration: r.Duration}
		}
		return out, nil
	}
	var m map[string]string
	if err := json.Unmarshal(body, &m); err == nil {
		out := make([]Track, 0, len(m))
		for id, title := range m {
			if id != "error" {
				out = append(out, Track{VideoID: id, Title: title})
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	n := len(body)
	if n > 120 {
		n = 120
	}
	log.Printf("[bot] search unexpected response: %s url=%s", body[:n], apiURL)
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

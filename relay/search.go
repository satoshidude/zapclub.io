package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxYtdlpOutput = 1 << 20 // 1 MiB — generous for ≤100 lines of id/title/duration

// capBuffer collects up to `cap` bytes and silently drops the rest, always reporting a
// full write so the child (yt-dlp) never blocks on a filled pipe. Bounds memory from a
// subprocess that emits unexpectedly large output.
type capBuffer struct {
	buf bytes.Buffer
	cap int
}

func (c *capBuffer) Write(p []byte) (int, error) {
	if room := c.cap - c.buf.Len(); room > 0 {
		if room > len(p) {
			room = len(p)
		}
		c.buf.Write(p[:room])
	}
	return len(p), nil
}

// runCapped runs cmd, capturing at most maxYtdlpOutput bytes of stdout.
func runCapped(cmd *exec.Cmd) ([]byte, error) {
	cb := &capBuffer{cap: maxYtdlpOutput}
	cmd.Stdout = cb
	err := cmd.Run()
	return cb.buf.Bytes(), err
}

// Self-hosted YouTube-Suche via yt-dlp. Kein Google-API-Key.
// Sicherheit: exec.CommandContext mit Arg-Liste (kein Shell → keine Injection),
// Timeout, Query-Längenlimit, Concurrency-Limit, In-Memory-Cache.

type searchResult struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Duration int    `json:"duration"`
}

const (
	searchTimeout  = 20 * time.Second
	searchMaxQuery = 120
	searchResults  = 8
	cacheTTL       = 10 * time.Minute
	playlistMax    = 100 // max. importierte Tracks pro Playlist
)

// YouTube-Playlist-ID: alphanumerisch + - _ (kein Shell-Risiko, URL serverseitig gebaut).
var listIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{10,64}$`)

// Begrenzt gleichzeitige yt-dlp-Prozesse (Resource-Schutz).
var searchSem = make(chan struct{}, 3)

// Pro-IP-Rate-Limit für /yt-search + /yt-playlist: Burst 10, Auffüllung 1 alle 3 s
// (~20/min nachhaltig). yt-dlp ist teuer → schützt vor Ressourcen-Erschöpfung durch
// Flood mit immer neuen (cache-umgehenden) Queries.
var ytLimiter = newIPLimiter(10, 1.0/3.0)

// clientIP liefert die ECHTE Client-IP. Hinter Caddy ist RemoteAddr stets localhost;
// Caddy setzt die echte IP via header_up X-Real-IP {remote_host} (nicht client-spoofbar).
func clientIP(r *http.Request) string {
	// Trust ONLY X-Real-IP (set by Caddy, not client-spoofable). Do NOT fall back to a
	// client-supplied X-Forwarded-For — an attacker could send a fresh value per request
	// to mint a new bucket each time and bypass the yt-dlp rate limit. If X-Real-IP is ever
	// missing, RemoteAddr is loopback behind Caddy → one shared bucket (fail-closed).
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// sweepCache entfernt abgelaufene Cache-Einträge (sonst wächst die Map bei vielen
// unterschiedlichen Queries unbegrenzt → Memory-DoS). Periodisch aus main() gerufen.
func sweepCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	for k, e := range searchCache {
		if time.Since(e.at) >= cacheTTL {
			delete(searchCache, k)
		}
	}
}

type cacheEntry struct {
	results []searchResult
	at      time.Time
}

var (
	searchCache = map[string]cacheEntry{}
	cacheMu     sync.Mutex
)

// The print format: id, title, duration, artist, and channel. In --flat-playlist mode
// yt-dlp does NOT extract per-video music metadata, so %(artist)s is almost always "NA";
// %(channel)s, however, IS populated and is our reliable artist source (see
// artistFromChannel). The artist field is kept for the rare case yt-dlp does fill it.
const ytPrint = "%(id)s\t%(title)s\t%(duration)s\t%(artist)s\t%(channel)s"

// artistFromChannel derives the artist from a YouTube channel name, but ONLY when the
// channel carries an unambiguous music marker: an auto-generated "<Artist> - Topic"
// channel (YouTube Music), a "<Artist>VEVO" channel, or an "<Artist> Official" channel.
// For a plain uploader channel (no marker) it returns "" — better to show no artist than a
// random uploader's name on a cover/reaction/clip.
func artistFromChannel(channel string) string {
	c := strings.TrimSpace(channel)
	if c == "" || c == "NA" {
		return ""
	}
	lower := strings.ToLower(c)
	for _, m := range []string{" - topic", " official", " officiel", "vevo"} {
		if strings.HasSuffix(lower, m) {
			return strings.TrimSpace(c[:len(c)-len(m)])
		}
	}
	return ""
}

// buildTitle folds the artist into the title so every track shows "Artist – Title". It
// prefers yt-dlp's artist tag, falls back to the cleaned channel name, and skips both when
// the artist is already part of the title (avoids "Artist – Artist - Song" duplication).
func buildTitle(title, artist, channel string) string {
	a := strings.TrimSpace(artist)
	if a == "" || a == "NA" {
		a = artistFromChannel(channel)
	}
	if a == "" {
		return title
	}
	if strings.Contains(strings.ToLower(title), strings.ToLower(a)) {
		return title
	}
	return a + " – " + title
}

// parseYtLines parses tab-separated "id\ttitle\tduration\tartist\tchannel" lines from yt-dlp.
func parseYtLines(out []byte) []searchResult {
	var results []searchResult
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		dur := 0
		if len(parts) >= 3 {
			if f, e := strconv.ParseFloat(parts[2], 64); e == nil {
				dur = int(f)
			}
		}
		artist, channel := "", ""
		if len(parts) >= 4 {
			artist = parts[3]
		}
		if len(parts) >= 5 {
			channel = parts[4]
		}
		results = append(results, searchResult{ID: parts[0], Title: buildTitle(parts[1], artist, channel), Duration: dur})
	}
	return results
}

// videoIDRe: an exact 11-char YouTube video id.
var videoIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// parseVideoID returns the YouTube video id if `q` is a single-video link or a bare id
// (youtu.be/<id>, youtube.com/watch?v=<id>, /shorts/<id>, /embed/<id>), else "". Used so that
// pasting a single video into search resolves THAT video instead of a keyword search.
func parseVideoID(q string) string {
	q = strings.TrimSpace(q)
	if videoIDRe.MatchString(q) {
		return q
	}
	u, err := url.Parse(q)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	if host == "youtu.be" {
		if id := strings.TrimPrefix(u.Path, "/"); videoIDRe.MatchString(id) {
			return id
		}
		return ""
	}
	if host == "youtube.com" || host == "m.youtube.com" || host == "music.youtube.com" {
		if v := u.Query().Get("v"); videoIDRe.MatchString(v) {
			return v
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "embed") && videoIDRe.MatchString(parts[1]) {
			return parts[1]
		}
	}
	return ""
}

// ytVideo resolves a SINGLE video's metadata. We CAN'T fetch the watch page directly — from a
// server IP YouTube gates it with "sign in to confirm you're not a bot" — but the SEARCH
// endpoint isn't gated and returns the exact video first when queried by its id. So we search
// for the id and pick the matching result (title + duration). Empty if it isn't found.
func ytVideo(ctx context.Context, id string) ([]searchResult, error) {
	cmd := exec.CommandContext(ctx, "/usr/local/bin/yt-dlp",
		"--flat-playlist", "--no-cache-dir", "--no-warnings",
		"--print", ytPrint,
		"--",
		"ytsearch5:"+id,
	)
	out, err := runCapped(cmd)
	if err != nil {
		return nil, err
	}
	for _, r := range parseYtLines(out) {
		if r.ID == id {
			return []searchResult{r}, nil
		}
	}
	return nil, nil
}

// oEmbed gives a video's channel (author_name) + title WITHOUT extraction — so it is NOT
// bot-gated (unlike the watch page). Cheap HTTP; lets us backfill the artist into a playlist's
// titles the same way the search path does (buildTitle folds the channel artist in).
var oembedClient = &http.Client{Timeout: 6 * time.Second}

type oembedResp struct {
	Title  string `json:"title"`
	Author string `json:"author_name"`
}

// oembedEnrich resolves id→"Artist – Title" via YouTube oEmbed, in parallel. Failed/unknown ids
// (private, deleted, no music-channel) are simply omitted.
func oembedEnrich(ctx context.Context, ids []string) map[string]string {
	out := make(map[string]string, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // cap parallel HTTP
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			u := "https://www.youtube.com/oembed?format=json&url=https://www.youtube.com/watch?v=" + id
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				return
			}
			resp, err := oembedClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			var o oembedResp
			if json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&o) != nil {
				return
			}
			if title := buildTitle(o.Title, "", o.Author); title != "" {
				mu.Lock()
				out[id] = title
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	return out
}

func ytSearch(ctx context.Context, query string) ([]searchResult, error) {
	cmd := exec.CommandContext(ctx, "/usr/local/bin/yt-dlp",
		"--flat-playlist", "--no-cache-dir", "--no-warnings",
		"--print", ytPrint,
		"--", // Ende der Optionen → Query kann nie als yt-dlp-Flag interpretiert werden.
		"ytsearch"+strconv.Itoa(searchResults)+":"+query,
	)
	out, err := runCapped(cmd)
	if err != nil {
		return nil, err
	}
	return parseYtLines(out), nil
}

func ytPlaylist(ctx context.Context, listID string) ([]searchResult, error) {
	cmd := exec.CommandContext(ctx, "/usr/local/bin/yt-dlp",
		"--flat-playlist", "--no-cache-dir", "--no-warnings",
		"--playlist-end", strconv.Itoa(playlistMax),
		"--print", ytPrint,
		"--", // Ende der Optionen (URL ist durch https://-Präfix ohnehin kein Flag).
		"https://www.youtube.com/playlist?list="+listID,
	)
	out, err := runCapped(cmd)
	if err != nil {
		return nil, err
	}
	return parseYtLines(out), nil
}

// /yt-playlist?list=<id> — importiert eine YouTube-Playlist (Track-Liste).
func handlePlaylist(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !ytLimiter.allow(clientIP(r)) {
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
		return
	}
	list := strings.TrimSpace(r.URL.Query().Get("list"))
	if !listIDRe.MatchString(list) {
		http.Error(w, `{"error":"bad list id"}`, http.StatusBadRequest)
		return
	}
	key := "pl:" + list

	cacheMu.Lock()
	if e, ok := searchCache[key]; ok && time.Since(e.at) < cacheTTL {
		cacheMu.Unlock()
		_ = json.NewEncoder(w).Encode(e.results)
		return
	}
	cacheMu.Unlock()

	select {
	case searchSem <- struct{}{}:
		defer func() { <-searchSem }()
	case <-r.Context().Done():
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()
	results, err := ytPlaylist(ctx, list)
	if err != nil {
		http.Error(w, `{"error":"playlist failed"}`, http.StatusBadGateway)
		return
	}

	cacheMu.Lock()
	searchCache[key] = cacheEntry{results: results, at: time.Now()}
	cacheMu.Unlock()

	_ = json.NewEncoder(w).Encode(results)
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !ytLimiter.allow(clientIP(r)) {
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
		return
	}
	// Enrich mode: ?ids=a,b,c → {id:"Artist – Title"} via oEmbed (backfills playlist titles
	// with the channel artist; not bot-gated, no yt-dlp). Capped + validated.
	if idsParam := r.URL.Query().Get("ids"); idsParam != "" {
		var ids []string
		for _, s := range strings.Split(idsParam, ",") {
			if s = strings.TrimSpace(s); videoIDRe.MatchString(s) {
				ids = append(ids, s)
			}
			if len(ids) >= 40 {
				break
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		_ = json.NewEncoder(w).Encode(oembedEnrich(ctx, ids))
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || len(query) > searchMaxQuery {
		http.Error(w, `{"error":"bad query"}`, http.StatusBadRequest)
		return
	}

	cacheMu.Lock()
	if e, ok := searchCache[query]; ok && time.Since(e.at) < cacheTTL {
		cacheMu.Unlock()
		_ = json.NewEncoder(w).Encode(e.results)
		return
	}
	cacheMu.Unlock()

	// Concurrency-Limit (mit Abbruch, falls Client geht).
	select {
	case searchSem <- struct{}{}:
		defer func() { <-searchSem }()
	case <-r.Context().Done():
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()
	// A single video link / id resolves to THAT video; otherwise it's a keyword search.
	var results []searchResult
	var err error
	if vid := parseVideoID(query); vid != "" {
		results, err = ytVideo(ctx, vid)
	} else {
		results, err = ytSearch(ctx, query)
	}
	if err != nil {
		http.Error(w, `{"error":"search failed"}`, http.StatusBadGateway)
		return
	}

	cacheMu.Lock()
	searchCache[query] = cacheEntry{results: results, at: time.Now()}
	cacheMu.Unlock()

	_ = json.NewEncoder(w).Encode(results)
}

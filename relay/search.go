package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
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

// The print format: id, title, duration, and artist. We ask yt-dlp for the artist so
// YouTube-Music-style results (where %(title)s is just the song) still show "Artist – Title".
const ytPrint = "%(id)s\t%(title)s\t%(duration)s\t%(artist)s"

// buildTitle folds the artist into the title when yt-dlp gives a distinct artist and it
// isn't already part of the title (avoids "Artist – Artist - Song" duplication).
func buildTitle(title, artist string) string {
	artist = strings.TrimSpace(artist)
	if artist == "" || artist == "NA" {
		return title
	}
	if strings.Contains(strings.ToLower(title), strings.ToLower(artist)) {
		return title
	}
	return artist + " – " + title
}

// parseYtLines parses tab-separated "id\ttitle\tduration\tartist" lines from yt-dlp.
func parseYtLines(out []byte) []searchResult {
	var results []searchResult
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		dur := 0
		if len(parts) >= 3 {
			if f, e := strconv.ParseFloat(parts[2], 64); e == nil {
				dur = int(f)
			}
		}
		artist := ""
		if len(parts) >= 4 {
			artist = parts[3]
		}
		results = append(results, searchResult{ID: parts[0], Title: buildTitle(parts[1], artist), Duration: dur})
	}
	return results
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
	results, err := ytSearch(ctx, query)
	if err != nil {
		http.Error(w, `{"error":"search failed"}`, http.StatusBadGateway)
		return
	}

	cacheMu.Lock()
	searchCache[query] = cacheEntry{results: results, at: time.Now()}
	cacheMu.Unlock()

	_ = json.NewEncoder(w).Encode(results)
}

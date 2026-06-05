package main

import (
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
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	// RemoteAddr ist "IP:Port" — Port abschneiden, sonst wäre jede Verbindung eine
	// eigene „IP" (eigener Bucket) und das Limit liefe ins Leere.
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

func ytSearch(ctx context.Context, query string) ([]searchResult, error) {
	cmd := exec.CommandContext(ctx, "/usr/local/bin/yt-dlp",
		"--flat-playlist", "--no-cache-dir", "--no-warnings",
		"--print", "%(id)s\t%(title)s\t%(duration)s",
		"--", // Ende der Optionen → Query kann nie als yt-dlp-Flag interpretiert werden.
		"ytsearch"+strconv.Itoa(searchResults)+":"+query,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var results []searchResult
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		dur := 0
		if len(parts) == 3 {
			if f, e := strconv.ParseFloat(parts[2], 64); e == nil {
				dur = int(f)
			}
		}
		results = append(results, searchResult{ID: parts[0], Title: parts[1], Duration: dur})
	}
	return results, nil
}

func ytPlaylist(ctx context.Context, listID string) ([]searchResult, error) {
	cmd := exec.CommandContext(ctx, "/usr/local/bin/yt-dlp",
		"--flat-playlist", "--no-cache-dir", "--no-warnings",
		"--playlist-end", strconv.Itoa(playlistMax),
		"--print", "%(id)s\t%(title)s\t%(duration)s",
		"--", // Ende der Optionen (URL ist durch https://-Präfix ohnehin kein Flag).
		"https://www.youtube.com/playlist?list="+listID,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var results []searchResult
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		dur := 0
		if len(parts) == 3 {
			if f, e := strconv.ParseFloat(parts[2], 64); e == nil {
				dur = int(f)
			}
		}
		results = append(results, searchResult{ID: parts[0], Title: parts[1], Duration: dur})
	}
	return results, nil
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

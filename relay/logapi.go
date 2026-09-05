package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// logToken is the Bearer token for the read-only /admin/logs endpoint.
// Set via LOG_API_TOKEN env; if empty the endpoint is disabled.
var logToken = env("LOG_API_TOKEN", "")

type logResponse struct {
	Since       string   `json:"since"`
	GeneratedAt string   `json:"generated_at"`
	Lines       []string `json:"lines"`
}

// handleLogs serves GET /admin/logs?since=1h
// Auth: Authorization: Bearer <LOG_API_TOKEN>
// Returns recent journalctl output for zapclub-relay, filtered to conductor/
// chat/connection events (strips high-frequency heartbeat/presence noise).
func handleLogs(w http.ResponseWriter, r *http.Request) {
	if logToken == "" {
		http.Error(w, "log api disabled", http.StatusServiceUnavailable)
		return
	}
	auth := r.Header.Get("Authorization")
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token != logToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	since := r.URL.Query().Get("since")
	if since == "" {
		since = "1 hour ago"
	} else {
		// Accept "1h" → "1 hour ago", "2h" → "2 hours ago", etc.
		since = normalizeSince(since)
	}

	cmd := exec.CommandContext(r.Context(), "journalctl",
		"-u", "zapclub-relay",
		"--no-pager",
		"--since", since,
		"--output", "short-iso",
	)
	out, err := cmd.Output()
	if err != nil {
		http.Error(w, "journalctl error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter noise: drop high-frequency heartbeat and presence lines.
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if shouldKeepLine(line) {
			lines = append(lines, line)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logResponse{
		Since:       since,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Lines:       lines,
	})
}

// normalizeSince converts short forms like "1h", "2h" to journalctl-friendly "N hour(s) ago".
func normalizeSince(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "h") {
		n := strings.TrimSuffix(s, "h")
		if n == "1" {
			return "1 hour ago"
		}
		return n + " hours ago"
	}
	if strings.HasSuffix(s, "m") {
		n := strings.TrimSuffix(s, "m")
		if n == "1" {
			return "1 minute ago"
		}
		return n + " minutes ago"
	}
	return s // pass through as-is (e.g. "2026-06-14 16:00:00")
}

// ── /admin/access-logs — Caddy HTTP access log summary ───────────────────────

type accessSummary struct {
	Since       string              `json:"since"`
	GeneratedAt string              `json:"generated_at"`
	TotalReqs   int                 `json:"total_requests"`
	UniqueIPs   int                 `json:"unique_ips"`
	ByHost      map[string]hostStat `json:"by_host"`
	TopIPs      []ipStat            `json:"top_ips"`
	Bots        []uaStat            `json:"bots"`
	Errors      []errorEntry        `json:"errors"`
}

type hostStat struct {
	Count    int            `json:"count"`
	TopURIs  []uriCount     `json:"top_uris"`
	Statuses map[string]int `json:"statuses"`
}

type uriCount struct {
	URI   string `json:"uri"`
	Count int    `json:"count"`
}

type ipStat struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
}
type uaStat struct {
	UA    string `json:"ua"`
	Count int    `json:"count"`
}
type errorEntry struct {
	Status int    `json:"status"`
	Host   string `json:"host"`
	URI    string `json:"uri"`
	IP     string `json:"ip"`
}

// handleAccessLogs serves GET /admin/access-logs?since=1h
// Parses Caddy JSON HTTP access log and returns a structured summary.
func handleAccessLogs(w http.ResponseWriter, r *http.Request) {
	if logToken == "" {
		http.Error(w, "log api disabled", http.StatusServiceUnavailable)
		return
	}
	authHdr := r.Header.Get("Authorization")
	tok := strings.TrimPrefix(authHdr, "Bearer ")
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	if tok != logToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sinceParam := r.URL.Query().Get("since")
	if sinceParam == "" {
		sinceParam = "1h"
	}
	dur := parseDuration(sinceParam)
	cutoff := float64(time.Now().Add(-dur).UnixNano()) / 1e9

	logPath := env("CADDY_LOG_PATH", "/var/log/caddy/access.log")
	f, err := os.Open(logPath)
	if err != nil {
		http.Error(w, "cannot open access log: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	type entry struct {
		host, uri, ip, ua string
		status            int
	}
	var hits []entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		var d map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &d); err != nil {
			continue
		}
		if d["msg"] != "handled request" {
			continue
		}
		ts, _ := d["ts"].(float64)
		if ts < cutoff {
			continue
		}
		req, _ := d["request"].(map[string]any)
		if req == nil {
			continue
		}
		host, _ := req["host"].(string)
		uri, _ := req["uri"].(string)
		ip, _ := req["client_ip"].(string)
		if ip == "" {
			ip, _ = req["remote_ip"].(string)
		}
		ua := "-"
		if hdrs, ok := req["headers"].(map[string]any); ok {
			if uas, ok := hdrs["User-Agent"].([]any); ok && len(uas) > 0 {
				ua, _ = uas[0].(string)
			}
		}
		status := 0
		if sv, ok := d["status"].(float64); ok {
			status = int(sv)
		}
		if uri != "" {
			// strip query string for grouping
			if idx := strings.IndexByte(uri, '?'); idx >= 0 {
				uri = uri[:idx]
			}
		}
		hits = append(hits, entry{host, uri, ip, ua, status})
	}

	// Build summary
	ipCounts := map[string]int{}
	uaCounts := map[string]int{}
	hostMap := map[string]*struct {
		count    int
		uris     map[string]int
		statuses map[string]int
	}{}
	var errors []errorEntry

	for _, h := range hits {
		ipCounts[h.ip]++
		if h.ua != "-" && !strings.Contains(h.ua, "Mozilla") {
			uaCounts[h.ua]++
		}
		if hostMap[h.host] == nil {
			hostMap[h.host] = &struct {
				count    int
				uris     map[string]int
				statuses map[string]int
			}{uris: map[string]int{}, statuses: map[string]int{}}
		}
		hm := hostMap[h.host]
		hm.count++
		if h.uri != "" {
			hm.uris[h.uri]++
		}
		if h.status > 0 {
			hm.statuses[http.StatusText(h.status)]++
		}
		if h.status >= 400 {
			errors = append(errors, errorEntry{h.status, h.host, h.uri, h.ip})
		}
	}

	// Top IPs
	topIPs := make([]ipStat, 0, len(ipCounts))
	for ip, n := range ipCounts {
		topIPs = append(topIPs, ipStat{ip, n})
	}
	sort.Slice(topIPs, func(i, j int) bool { return topIPs[i].Count > topIPs[j].Count })
	if len(topIPs) > 10 {
		topIPs = topIPs[:10]
	}

	// Top bots
	topBots := make([]uaStat, 0, len(uaCounts))
	for ua, n := range uaCounts {
		s := ua
		if len(s) > 80 {
			s = s[:80]
		}
		topBots = append(topBots, uaStat{s, n})
	}
	sort.Slice(topBots, func(i, j int) bool { return topBots[i].Count > topBots[j].Count })
	if len(topBots) > 5 {
		topBots = topBots[:5]
	}

	// Per-host stats
	byHost := map[string]hostStat{}
	for host, hm := range hostMap {
		uris := make([]uriCount, 0, len(hm.uris))
		for u, n := range hm.uris {
			uris = append(uris, uriCount{u, n})
		}
		sort.Slice(uris, func(i, j int) bool { return uris[i].Count > uris[j].Count })
		if len(uris) > 8 {
			uris = uris[:8]
		}
		statuses := map[string]int{}
		for k, v := range hm.statuses {
			statuses[k] = v
		}
		byHost[host] = hostStat{hm.count, uris, statuses}
	}

	if len(errors) > 15 {
		errors = errors[:15]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(accessSummary{
		Since:       sinceParam,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		TotalReqs:   len(hits),
		UniqueIPs:   len(ipCounts),
		ByHost:      byHost,
		TopIPs:      topIPs,
		Bots:        topBots,
		Errors:      errors,
	})
}

// parseDuration converts "1h", "30m", "2h" etc. to time.Duration.
func parseDuration(s string) time.Duration {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "h") {
		if n := strings.TrimSuffix(s, "h"); n != "" {
			var v int
			if _, err := fmt.Sscanf(n, "%d", &v); err == nil {
				return time.Duration(v) * time.Hour
			}
		}
	}
	if strings.HasSuffix(s, "m") {
		if n := strings.TrimSuffix(s, "m"); n != "" {
			var v int
			if _, err := fmt.Sscanf(n, "%d", &v); err == nil {
				return time.Duration(v) * time.Minute
			}
		}
	}
	return time.Hour
}

// shouldKeepLine drops chatty lines that add volume without analytical value.
func shouldKeepLine(line string) bool {
	noisy := []string{
		"heartbeat", " hb ", "30102", "20100", "20105",
		"BrokenPipeError", "Exception ignored",
		"flushing sys.stdout",
	}
	for _, n := range noisy {
		if strings.Contains(line, n) {
			return false
		}
	}
	return true
}

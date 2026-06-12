package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// radioStation holds one ffmpeg process (MP3 to stdout) and broadcasts its output
// to all connected HTTP listeners. When all listeners disconnect, the station idles.
type radioStation struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newRadioStation() *radioStation {
	return &radioStation{clients: map[chan []byte]struct{}{}}
}

func (s *radioStation) subscribe() chan []byte {
	ch := make(chan []byte, 128) // ~512 kB buffer (128 × 4 kB chunks)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *radioStation) unsubscribe(ch chan []byte) {
	s.mu.Lock()
	delete(s.clients, ch)
	s.mu.Unlock()
}

func (s *radioStation) listenerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

func (s *radioStation) broadcast(data []byte) {
	cp := make([]byte, len(data)) // copy so each listener owns its slice
	copy(cp, data)
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- cp:
		default:
			// slow listener: drop chunk rather than block the broadcast goroutine
		}
	}
}

// radioProc tracks one ffmpeg process for a club.
type radioProc struct {
	cancel context.CancelFunc
	id     uint64
}

// radioManager runs per-club MP3 streams accessible via GET /radio/<clubid>.
// Only the club owner may start/stop via POST /radio/start|stop.
// The ffmpeg process is restarted on each track advance by the conductor.
type radioManager struct {
	mu       sync.Mutex
	cond     *conductor
	stations map[string]*radioStation // clubID → station (persistent across restarts)
	procs    map[string]*radioProc    // clubID → running proc
	nextID   uint64
}

func newRadioManager(cond *conductor) *radioManager {
	return &radioManager{
		cond:     cond,
		stations: map[string]*radioStation{},
		procs:    map[string]*radioProc{},
	}
}

// isActive reports whether the club currently has a running radio stream.
func (m *radioManager) isActive(clubID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.procs[clubID] != nil
}

// startStream begins broadcasting club audio. Called by HTTP /radio/start.
func (m *radioManager) startStream(clubID string) {
	videoID, seekSec := m.cond.currentTrack(clubID)
	if videoID == "" {
		return // nothing playing yet; restartForClub will catch next advance
	}
	m.restartProc(clubID, videoID, seekSec)
}

// stopStream halts the club radio. Called by HTTP /radio/stop.
func (m *radioManager) stopStream(clubID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.procs[clubID]; p != nil {
		p.cancel()
		delete(m.procs, clubID)
	}
}

// restartForClub is called by the conductor on every track advance.
// Only restarts if the club radio is already active (owner enabled it).
func (m *radioManager) restartForClub(clubID, videoID string) {
	m.mu.Lock()
	active := m.procs[clubID] != nil
	m.mu.Unlock()
	if active {
		m.restartProc(clubID, videoID, 0)
	}
}

func (m *radioManager) restartProc(clubID, videoID string, seekSec int) {
	ctx, cancel := context.WithCancel(context.Background())

	m.mu.Lock()
	if old := m.procs[clubID]; old != nil {
		old.cancel()
	}
	station, ok := m.stations[clubID]
	if !ok {
		station = newRadioStation()
		m.stations[clubID] = station
	}
	m.nextID++
	myID := m.nextID
	m.procs[clubID] = &radioProc{cancel: cancel, id: myID}
	m.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			m.mu.Lock()
			if p := m.procs[clubID]; p != nil && p.id == myID {
				delete(m.procs, clubID)
			}
			m.mu.Unlock()
			log.Printf("radio [%.8s] proc exited vid=%s", clubID, videoID)
		}()
		if err := m.runProc(ctx, clubID, videoID, seekSec, station); err != nil && ctx.Err() == nil {
			log.Printf("radio [%.8s] error: %v", clubID, err)
		}
	}()
}

func (m *radioManager) runProc(ctx context.Context, clubID, videoID string, seekSec int, station *radioStation) error {
	// Resolve CDN URL via yt-dlp (same helper as rtmpstream.go).
	resolveCtx, resolveCancel := context.WithTimeout(ctx, 40*time.Second)
	defer resolveCancel()
	log.Printf("radio [%.8s] yt-dlp resolving vid=%s", clubID, videoID)

	ytArgs := []string{"--get-url", "-f", "bestaudio", "--no-warnings"}
	if ytdlpCookies != "" {
		ytArgs = append(ytArgs, "--cookies", ytdlpCookies)
	}
	ytArgs = append(ytArgs, "--", "https://www.youtube.com/watch?v="+videoID)
	ytCmd := exec.CommandContext(resolveCtx, "yt-dlp", ytArgs...)
	out, err := ytCmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			log.Printf("radio [%.8s] yt-dlp stderr: %s", clubID, strings.TrimSpace(string(ee.Stderr)))
		}
		return err
	}
	audioURL := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if audioURL == "" {
		return nil
	}
	log.Printf("radio [%.8s] yt-dlp ok, starting ffmpeg vid=%s seek=%ds", clubID, videoID, seekSec)

	// ffmpeg → MP3 → stdout for broadcasting
	args := []string{
		"-loglevel", "warning",
		"-i", audioURL,
		"-c:a", "libmp3lame", "-b:a", "128k", "-ar", "44100",
		"-f", "mp3",
		"-",
	}
	if seekSec > 0 {
		args = append([]string{"-ss", fmt.Sprintf("%d", seekSec)}, args...)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}

	if stderr != nil {
		go func() {
			sc := bufio.NewScanner(stderr)
			for sc.Scan() {
				log.Printf("radio [%.8s] ffmpeg: %s", clubID, sc.Text())
			}
		}()
	}

	buf := make([]byte, 4096)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			station.broadcast(buf[:n])
		}
		if err != nil {
			break
		}
	}
	_ = cmd.Wait()
	return nil
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

type radioHandler struct {
	mgr  *radioManager
	cond *conductor
}

// ServeHTTP dispatches /radio/<clubid> (GET=listen), /radio/start, /radio/stop.
func (h *radioHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/radio/start" || path == "/radio/stop":
		h.handleControl(w, r)
	case strings.HasPrefix(path, "/radio/") && len(path) > len("/radio/"):
		clubID := strings.TrimPrefix(path, "/radio/")
		h.handleListen(w, r, clubID)
	default:
		http.NotFound(w, r)
	}
}

func (h *radioHandler) handleControl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pubkey, ok := verifyNIP98Pubkey(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	club := r.URL.Query().Get("club")
	if club == "" {
		http.Error(w, "missing club param", http.StatusBadRequest)
		return
	}

	owner := h.cond.clubOwner(r.Context(), club)
	if owner == "" || pubkey != owner {
		http.Error(w, "forbidden: only club owner may control radio", http.StatusForbidden)
		return
	}

	if r.URL.Path == "/radio/start" {
		h.mgr.startStream(club)
		log.Printf("radio start [%.8s] by %.8s", club, pubkey)
		w.WriteHeader(http.StatusNoContent)
	} else {
		h.mgr.stopStream(club)
		log.Printf("radio stop [%.8s] by %.8s", club, pubkey)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *radioHandler) handleListen(w http.ResponseWriter, r *http.Request, clubID string) {
	// CORS: radio streams are public (anyone can listen)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.mgr.isActive(clubID) {
		http.Error(w, "radio not streaming", http.StatusServiceUnavailable)
		return
	}

	h.mgr.mu.Lock()
	station := h.mgr.stations[clubID]
	h.mgr.mu.Unlock()
	if station == nil {
		http.Error(w, "radio not streaming", http.StatusServiceUnavailable)
		return
	}

	ch := station.subscribe()
	defer station.unsubscribe(ch)
	log.Printf("radio [%.8s] listener connected (total=%d)", clubID, station.listenerCount())

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	flusher, canFlush := w.(http.Flusher)

	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(data); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		case <-r.Context().Done():
			log.Printf("radio [%.8s] listener disconnected (remaining=%d)", clubID, station.listenerCount()-1)
			return
		}
	}
}

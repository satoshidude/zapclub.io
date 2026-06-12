package main

import (
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

// radioStation fans out WebM/Opus audio chunks (pushed via WebSocket from the owner's
// browser) to all connected HTTP listeners. No server-side yt-dlp or ffmpeg needed —
// the browser MediaRecorder captures the YouTube tab audio and streams it here.
type radioStation struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newRadioStation() *radioStation {
	return &radioStation{clients: map[chan []byte]struct{}{}}
}

func (s *radioStation) subscribe() chan []byte {
	ch := make(chan []byte, 128) // ~512 kB buffer
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
	cp := make([]byte, len(data))
	copy(cp, data)
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- cp:
		default: // slow listener: drop rather than block
		}
	}
}

// radioManager: per-club WebSocket-push fan-out.
type radioManager struct {
	mu       sync.Mutex
	stations map[string]*radioStation
	pushers  map[string]int // active browser-pusher count per club
}

func newRadioManager(_ *conductor) *radioManager {
	return &radioManager{
		stations: map[string]*radioStation{},
		pushers:  map[string]int{},
	}
}

func (m *radioManager) getOrCreate(clubID string) *radioStation {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.stations[clubID]; ok {
		return s
	}
	s := newRadioStation()
	m.stations[clubID] = s
	return s
}

func (m *radioManager) pusherAdd(clubID string) {
	m.mu.Lock()
	m.pushers[clubID]++
	m.mu.Unlock()
}

func (m *radioManager) pusherRemove(clubID string) {
	m.mu.Lock()
	if m.pushers[clubID] > 0 {
		m.pushers[clubID]--
	}
	m.mu.Unlock()
}

func (m *radioManager) isActive(clubID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pushers[clubID] > 0
}

// ── HTTP / WebSocket handlers ─────────────────────────────────────────────────

type radioHandler struct {
	mgr  *radioManager
	cond *conductor
}

func (h *radioHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/radio/push/"):
		clubID := strings.TrimPrefix(path, "/radio/push/")
		h.handlePush(w, r, clubID)
	case strings.HasPrefix(path, "/radio/") && len(path) > len("/radio/"):
		clubID := strings.TrimPrefix(path, "/radio/")
		h.handleListen(w, r, clubID)
	default:
		http.NotFound(w, r)
	}
}

// handlePush accepts a WebSocket connection from the club owner's browser.
// The browser pushes WebM/Opus audio chunks; the relay fans them to HTTP listeners.
// NIP-98 auth is passed as ?auth=<base64 event> because browsers cannot set custom
// headers on WebSocket upgrades.
func (h *radioHandler) handlePush(w http.ResponseWriter, r *http.Request, clubID string) {
	if clubID == "" {
		http.Error(w, "missing club id", http.StatusBadRequest)
		return
	}

	pubkey, ok := verifyNIP98QueryParam(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	owner := h.cond.clubOwner(r.Context(), clubID)
	if owner == "" || pubkey != owner {
		http.Error(w, "forbidden: only club owner may push radio", http.StatusForbidden)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"zapclub.io", "www.zapclub.io"},
	})
	if err != nil {
		log.Printf("radio [%.8s] ws accept: %v", clubID, err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	station := h.mgr.getOrCreate(clubID)
	h.mgr.pusherAdd(clubID)
	defer h.mgr.pusherRemove(clubID)
	log.Printf("radio [%.8s] pusher connected by %.8s", clubID, pubkey)

	ctx := r.Context()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if len(data) > 0 {
			station.broadcast(data)
		}
	}
	log.Printf("radio [%.8s] pusher disconnected", clubID)
}

// handleListen serves the audio stream to HTTP clients (browser <audio>, VLC, etc.).
// Content-Type is audio/webm matching the browser MediaRecorder output.
func (h *radioHandler) handleListen(w http.ResponseWriter, r *http.Request, clubID string) {
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

	station := h.mgr.getOrCreate(clubID)
	ch := station.subscribe()
	defer station.unsubscribe(ch)
	log.Printf("radio [%.8s] listener connected (total=%d)", clubID, station.listenerCount())

	w.Header().Set("Content-Type", "audio/webm;codecs=opus")
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

package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	icyMetaint  = 8192                              // ICY metadata interval in bytes
	icyArtwork  = "https://zapclub.io/icon-192.png" // album art sent to ICY-aware players
	icyStationURL = "https://zapclub.io/"
)

// Shared turntable SVG and brand CSS used by both radio page templates.
// The vinyl group animates at 2.4 s per rotation, matching the Svelte Turntable component.
const radioBrandSVG = `<svg class="turntable" viewBox="0 0 36 36" width="120" height="120" role="img" aria-label="zapclub.io">
  <g class="vinyl">
    <circle cx="16" cy="20" r="13" fill="#1b0b33" stroke="#8e30eb" stroke-width="1.6"/>
    <circle cx="16" cy="20" r="9.5" fill="none" stroke="#a855f7" stroke-width="0.5" opacity="0.4"/>
    <circle cx="16" cy="20" r="6.5" fill="none" stroke="#a855f7" stroke-width="0.5" opacity="0.3"/>
    <circle cx="16" cy="20" r="3.6" fill="#22c55e"/>
    <circle cx="16" cy="11.5" r="1.1" fill="#d8b4fe"/>
    <circle cx="16" cy="20" r="1" fill="#1b0b33"/>
  </g>
  <line x1="29" y1="7" x2="20.5" y2="15.5" stroke="#c084fc" stroke-width="1.7" stroke-linecap="round"/>
  <circle cx="29" cy="7" r="1.9" fill="#c084fc"/>
</svg>`

const radioBrandCSS = `
.brand{display:flex;flex-direction:column;align-items:center;gap:.9rem}
.turntable{display:block;filter:drop-shadow(0 0 10px rgba(142,48,235,.6))}
.vinyl{transform-origin:16px 20px;animation:spin 2.4s linear infinite}
@keyframes spin{to{transform:rotate(360deg)}}
@media(prefers-reduced-motion:reduce){.vinyl{animation:none}}
.brand-name{font-size:2rem;font-weight:800;letter-spacing:-.02em}
.brand-name .word{color:#fff}
.brand-name .tld{color:#8e30eb;font-weight:700}`

// radioPlayerPage args: streamPath, streamPath (m3u), clubID, clubName
const radioPlayerPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>📻 zapclub.io Webradio</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#0d0d0f;color:#e2e8f0;font-family:system-ui,sans-serif;
     display:flex;flex-direction:column;align-items:center;justify-content:center;
     min-height:100vh;gap:2rem;padding:2rem}
` + radioBrandCSS + `
audio{width:min(320px,85vw);opacity:.7}
.offline{color:#ef4444;font-size:.85rem;display:none}
.url-row{display:flex;align-items:center;gap:.5rem;width:min(340px,88vw)}
.url-row input{flex:1;background:#1e1e2e;border:1px solid #334155;border-radius:.4rem;
               color:#64748b;font-size:.73rem;padding:.4rem .6rem;outline:none;min-width:0}
.url-row button{background:#1e1e2e;border:1px solid #334155;border-radius:.4rem;color:#94a3b8;
                font-size:.73rem;padding:.4rem .7rem;cursor:pointer;white-space:nowrap}
.url-row button:hover{border-color:#475569;color:#e2e8f0}
.url-row a{color:#64748b;font-size:.73rem;text-decoration:none;white-space:nowrap}
.url-row a:hover{color:#94a3b8}
.enter{display:inline-block;background:#8e30eb;color:#fff;
       font-weight:600;font-size:.9rem;padding:.6rem 1.4rem;border-radius:.5rem;
       text-decoration:none;letter-spacing:.01em}
.enter:hover{background:#a855f7}
</style>
</head>
<body>
<div class="brand">
` + radioBrandSVG + `
  <span class="brand-name"><span class="word">zapclub</span><span class="tld">.io</span></span>
</div>
<audio id="player" src="%s" autoplay controls
  onerror="document.getElementById('offline').style.display='block'"></audio>
<p id="offline" class="offline">⚠ Stream offline</p>
<div class="url-row">
  <input id="streamurl" type="text" readonly>
  <button onclick="copyURL()">Copy</button>
  <a href="%s.m3u" download>M3U</a>
</div>
<a class="enter" href="https://zapclub.io/?club=%s">Enter the Club: %s</a>
<script>
var u=location.href.replace(/\?.*$/,'');
document.getElementById('streamurl').value=u;
document.getElementById('player').src=u;
function copyURL(){navigator.clipboard.writeText(u).then(function(){
  var b=document.querySelector('.url-row button');
  b.textContent='Copied!';setTimeout(function(){b.textContent='Copy'},1500);
});}
</script>
</body>
</html>`

// radioOfflinePage args: clubID, clubName
const radioOfflinePage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>📻 zapclub.io Webradio</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#0d0d0f;font-family:system-ui,sans-serif;
     display:flex;flex-direction:column;align-items:center;justify-content:center;
     min-height:100vh;gap:1.5rem;padding:2rem}
` + radioBrandCSS + `
.turntable{opacity:.4}
.vinyl{animation:none!important}
.status{color:#475569;font-size:.85rem}
.enter{display:inline-block;background:#8e30eb;color:#fff;font-weight:600;
       font-size:.9rem;padding:.6rem 1.4rem;border-radius:.5rem;
       text-decoration:none;letter-spacing:.01em}
.enter:hover{background:#a855f7}
</style>
</head>
<body>
<div class="brand">
` + radioBrandSVG + `
  <span class="brand-name"><span class="word">zapclub</span><span class="tld">.io</span></span>
</div>
<p class="status">📻 Stream offline</p>
<a class="enter" href="https://zapclub.io/?club=%s">Enter the Club: %s</a>
</body>
</html>`

// ytdlpProxy is the SOCKS5/HTTP proxy used for yt-dlp to bypass YouTube's
// datacenter IP blocks. Set via YTDLP_PROXY env (e.g. socks5://127.0.0.1:40000).
// Cloudflare WARP in proxy mode listens on socks5://127.0.0.1:40000 by default.
var ytdlpProxy = os.Getenv("YTDLP_PROXY")

// radioStation fans out audio chunks from the server-side yt-dlp→ffmpeg pipeline
// to all connected HTTP listeners.
type radioStation struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	titleMu sync.RWMutex
	title   string // current track title for ICY metadata injection
}

func newRadioStation() *radioStation {
	return &radioStation{clients: map[chan []byte]struct{}{}}
}

func (s *radioStation) setTitle(t string) {
	s.titleMu.Lock()
	s.title = t
	s.titleMu.Unlock()
}

func (s *radioStation) getTitle() string {
	s.titleMu.RLock()
	defer s.titleMu.RUnlock()
	return s.title
}

// icyBlock builds an ICY metadata block for StreamTitle injection.
// Format: 1-byte length (in 16-byte units) + metadata padded to length*16 bytes.
func icyBlock(title string) []byte {
	// Strip/replace characters that break the ICY format.
	safe := strings.NewReplacer("'", "’", "\n", " ", "\r", "").Replace(title)
	meta := "StreamTitle='" + safe + "';StreamUrl='" + icyStationURL + "';"
	metaBytes := []byte(meta)
	blocks := (len(metaBytes) + 15) / 16
	if blocks > 255 {
		blocks = 255
	}
	buf := make([]byte, 1+blocks*16)
	buf[0] = byte(blocks)
	copy(buf[1:], metaBytes)
	return buf
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

// radioClub holds per-club streaming state.
type radioClub struct {
	station *radioStation
	cancel  context.CancelFunc
	videoID string
	title   string
}

// radioManager manages server-side audio streaming per club.
// Streams start and stop automatically whenever the conductor advances or stops a club.
// No manual activation required — every club with active playback gets its own stream.
type radioManager struct {
	mu    sync.Mutex
	clubs map[string]*radioClub
}

func newRadioManager() *radioManager {
	return &radioManager{clubs: map[string]*radioClub{}}
}

func (m *radioManager) getOrCreate(clubID string) *radioClub {
	if rc, ok := m.clubs[clubID]; ok {
		return rc
	}
	rc := &radioClub{station: newRadioStation()}
	m.clubs[clubID] = rc
	return rc
}

// onTrackChange is called by the conductor on every track advance or stop.
// videoID="" means playback stopped (lobby) — stream pauses until next advance.
func (m *radioManager) onTrackChange(clubID, videoID, title string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rc := m.getOrCreate(clubID)
	rc.title = title
	rc.station.setTitle(title)
	m.startStream(rc, clubID, videoID)
}

// isActive returns true when a track is currently streaming for the club.
func (m *radioManager) isActive(clubID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	rc := m.clubs[clubID]
	return rc != nil && rc.videoID != ""
}

// startStream starts a yt-dlp→ffmpeg pipeline for videoID. Caller must hold m.mu.
func (m *radioManager) startStream(rc *radioClub, clubID, videoID string) {
	if rc.cancel != nil {
		rc.cancel()
		rc.cancel = nil
	}
	rc.videoID = videoID
	if videoID == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	rc.cancel = cancel
	go streamVideo(ctx, rc.station, clubID, videoID)
}

// streamVideo pipes yt-dlp → ffmpeg for a YouTube video ID.
// yt-dlp downloads through the WARP SOCKS5 proxy (YTDLP_PROXY env) and pipes
// raw video/audio data directly to ffmpeg's stdin — no CDN URL hop, no IP lock.
// ffmpeg extracts the audio and re-encodes as MP3 for browser <audio>.
// Retries up to 3 times with backoff if yt-dlp fails quickly (e.g. 429).
func streamVideo(ctx context.Context, station *radioStation, clubID, videoID string) {
	log.Printf("radio [%.8s] stream start vid=%s", clubID, videoID)

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*15) * time.Second
			log.Printf("radio [%.8s] retry %d/%d in %s vid=%s", clubID, attempt, 3, delay, videoID)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
		if ok := streamOnce(ctx, station, clubID, videoID); ok {
			break
		}
	}
	log.Printf("radio [%.8s] stream end vid=%s", clubID, videoID)
}

// streamOnce runs one yt-dlp→ffmpeg pass. Returns true if the stream ran long
// enough to be considered successful (≥30 s), false on quick failure (retry).
func streamOnce(ctx context.Context, station *radioStation, clubID, videoID string) bool {
	start := time.Now()

	// Format priority: combined mp4 (no PO token required) → HLS fallback.
	// Audio-only formats (webm/m4a) require a YouTube PO token which needs a real browser.
	ytArgs := []string{
		"-f", "18/93/94/91/best[ext=mp4]/best",
		"-o", "-",
		"--quiet",
	}
	if ytdlpProxy != "" {
		ytArgs = append(ytArgs, "--proxy", ytdlpProxy)
	}
	ytArgs = append(ytArgs, "--", videoID)

	ytCmd := exec.CommandContext(ctx, "yt-dlp", ytArgs...)
	ytCmd.Stderr = log.Writer() // log yt-dlp errors (429, unavailable, etc.)
	ytPipe, err := ytCmd.StdoutPipe()
	if err != nil {
		log.Printf("radio [%.8s] yt-dlp pipe: %v", clubID, err)
		return false
	}
	if err := ytCmd.Start(); err != nil {
		log.Printf("radio [%.8s] yt-dlp start: %v", clubID, err)
		return false
	}
	defer ytCmd.Wait()

	ffCmd := exec.CommandContext(ctx, "ffmpeg",
		"-loglevel", "warning",
		"-re",
		"-i", "pipe:0",
		"-vn",
		"-f", "mp3", "-c:a", "libmp3lame", "-b:a", "192k",
		"pipe:1",
	)
	ffCmd.Stdin = ytPipe
	stdout, err := ffCmd.StdoutPipe()
	if err != nil {
		log.Printf("radio [%.8s] ffmpeg pipe: %v", clubID, err)
		ytCmd.Process.Kill() //nolint:errcheck
		return false
	}
	if err := ffCmd.Start(); err != nil {
		log.Printf("radio [%.8s] ffmpeg start: %v", clubID, err)
		ytCmd.Process.Kill() //nolint:errcheck
		return false
	}
	defer ffCmd.Wait()

	buf := make([]byte, 4096)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			station.broadcast(chunk)
		}
		if err != nil {
			break
		}
	}
	return time.Since(start) >= 30*time.Second
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

type radioHandler struct {
	mgr  *radioManager
	cond *conductor
}

func (h *radioHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, ".m3u") && strings.HasPrefix(path, "/radio/"):
		clubID := strings.TrimSuffix(strings.TrimPrefix(path, "/radio/"), ".m3u")
		h.handleM3U(w, r, clubID)
	case strings.HasPrefix(path, "/radio/") && len(path) > len("/radio/"):
		clubID := strings.TrimPrefix(path, "/radio/")
		h.handleListen(w, r, clubID)
	default:
		http.NotFound(w, r)
	}
}

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

	// Browser navigation: serve an HTML player page instead of raw audio.
	// Media players and <audio> elements send Accept: */*, not text/html.
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		if !h.mgr.isActive(clubID) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, radioOfflinePage, clubID, h.clubName(clubID)) //nolint:errcheck
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		streamPath := "/radio/" + clubID
		clubName := h.clubName(clubID)
		fmt.Fprintf(w, radioPlayerPage, streamPath, streamPath, clubID, clubName)
		return
	}

	if !h.mgr.isActive(clubID) {
		http.Error(w, "radio not streaming", http.StatusServiceUnavailable)
		return
	}

	h.mgr.mu.Lock()
	rc := h.mgr.clubs[clubID]
	var station *radioStation
	if rc != nil {
		station = rc.station
	}
	h.mgr.mu.Unlock()

	if station == nil {
		http.Error(w, "radio not streaming", http.StatusServiceUnavailable)
		return
	}

	ch := station.subscribe()
	defer station.unsubscribe(ch)
	log.Printf("radio [%.8s] listener connected (total=%d)", clubID, station.listenerCount())

	wantICY := r.Header.Get("Icy-MetaData") == "1"
	clubName := h.clubName(clubID)

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("icy-name", clubName+" — zapclub.io")
	w.Header().Set("icy-description", "Live collaborative radio on zapclub.io")
	w.Header().Set("icy-artwork", icyArtwork)
	w.Header().Set("icy-url", icyStationURL)
	w.Header().Set("icy-br", "192")
	if wantICY {
		w.Header().Set("icy-metaint", fmt.Sprintf("%d", icyMetaint))
	}

	flusher, canFlush := w.(http.Flusher)
	sent := 0 // bytes written since last ICY metadata block

	writeWithICY := func(data []byte) error {
		if !wantICY {
			_, err := w.Write(data)
			return err
		}
		buf := &bytes.Buffer{}
		offset := 0
		for offset < len(data) {
			// How many audio bytes until the next metadata slot?
			room := icyMetaint - sent
			end := offset + room
			if end > len(data) {
				end = len(data)
			}
			buf.Write(data[offset:end])
			sent += end - offset
			offset = end
			if sent == icyMetaint {
				buf.Write(icyBlock(station.getTitle()))
				sent = 0
			}
		}
		_, err := w.Write(buf.Bytes())
		return err
	}

	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
			}
			if err := writeWithICY(data); err != nil {
				log.Printf("radio [%.8s] listener write error: %v", clubID, err)
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

func (h *radioHandler) clubName(clubID string) string {
	if g, ok := h.cond.state.Groups.Load(clubID); ok {
		if name := g.Group.Name; name != "" {
			return name
		}
	}
	return clubID
}

func (h *radioHandler) handleM3U(w http.ResponseWriter, r *http.Request, clubID string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	streamURL := fmt.Sprintf("%s://%s/radio/%s", scheme, r.Host, clubID)
	name := h.clubName(clubID)
	m3u := "#EXTM3U\n#EXTINF:-1," + name + " — zapclub.io Webradio\n" + streamURL + "\n"
	w.Header().Set("Content-Type", "audio/x-mpegurl")
	w.Header().Set("Content-Disposition", `attachment; filename="zapclub-radio.m3u"`)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	fmt.Fprint(w, m3u)
}


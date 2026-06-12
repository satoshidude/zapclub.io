package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

// rtmpStream holds one active ffmpeg push for a (club, dj) pair.
type rtmpStream struct {
	cancel   context.CancelFunc
	rtmpURL  string
	clubName string
}

// rtmpManager drives ffmpeg processes that push the current club audio + a QR overlay
// to external RTMP endpoints. Multiple DJs in one club may stream to different platforms
// simultaneously — one ffmpeg process per (club, dj). The conductor calls restartForClub
// on every track advance; HTTP handlers /rtmp/start and /rtmp/stop control sessions.
type rtmpManager struct {
	mu      sync.Mutex
	cond    *conductor
	streams map[string]map[string]*rtmpStream // club → dj → stream

	qrMu    sync.Mutex
	qrPaths map[string]string // clubID → overlay PNG path (generated once, reused)
}

func newRtmpManager(cond *conductor) *rtmpManager {
	return &rtmpManager{
		cond:    cond,
		streams: map[string]map[string]*rtmpStream{},
		qrPaths: map[string]string{},
	}
}

// start begins pushing videoID to rtmpURL for (club, dj). Cancels any prior process.
func (m *rtmpManager) start(club, clubName, dj, rtmpURL, videoID string, seekSec int) {
	ctx, cancel := context.WithCancel(context.Background())

	m.mu.Lock()
	if m.streams[club] == nil {
		m.streams[club] = map[string]*rtmpStream{}
	}
	if old := m.streams[club][dj]; old != nil {
		old.cancel()
	}
	s := &rtmpStream{cancel: cancel, rtmpURL: rtmpURL, clubName: clubName}
	m.streams[club][dj] = s
	m.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			m.mu.Lock()
			if cur := m.streams[club][dj]; cur == s {
				delete(m.streams[club], dj)
				if len(m.streams[club]) == 0 {
					delete(m.streams, club)
				}
			}
			m.mu.Unlock()
		}()
		if err := m.runStream(ctx, club, clubName, videoID, seekSec, rtmpURL); err != nil && ctx.Err() == nil {
			log.Printf("rtmp [%.8s/%.8s] error: %v", club, dj, err)
		}
	}()
}

// stop cancels the RTMP stream for (club, dj).
func (m *rtmpManager) stop(club, dj string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.streams[club][dj]; s != nil {
		s.cancel()
		delete(m.streams[club], dj)
		if len(m.streams[club]) == 0 {
			delete(m.streams, club)
		}
	}
}

// restartForClub is called by the conductor on every track advance. All active streams
// for this club get a fresh process for the new track (seek=0, fresh track).
func (m *rtmpManager) restartForClub(club, videoID string) {
	type entry struct{ dj, rtmpURL, clubName string }
	m.mu.Lock()
	var active []entry
	for dj, s := range m.streams[club] {
		s.cancel()
		active = append(active, entry{dj: dj, rtmpURL: s.rtmpURL, clubName: s.clubName})
	}
	delete(m.streams, club)
	m.mu.Unlock()

	for _, e := range active {
		m.start(club, e.clubName, e.dj, e.rtmpURL, videoID, 0)
	}
}

// overlayPath returns the overlay PNG for the club, generating it if needed.
// The image contains a QR code linking to the club join URL.
func (m *rtmpManager) overlayPath(clubID string) (string, error) {
	m.qrMu.Lock()
	defer m.qrMu.Unlock()

	if p, ok := m.qrPaths[clubID]; ok {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	joinURL := "https://zapclub.io/club/" + clubID
	// Use first 16 chars of clubID to keep filename short but unique.
	slug := clubID
	if len(slug) > 16 {
		slug = slug[:16]
	}
	path := filepath.Join(os.TempDir(), "zc-qr-"+slug+".png")

	q, err := qrcode.New(joinURL, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("qrcode: %w", err)
	}
	// Light QR (dark modules on white bg) — overlaid on dark frame it looks clean.
	if err := q.WriteFile(400, path); err != nil {
		return "", fmt.Errorf("qrcode write: %w", err)
	}

	m.qrPaths[clubID] = path
	return path, nil
}

// runStream resolves the YouTube CDN URL, generates the overlay, then runs ffmpeg.
func (m *rtmpManager) runStream(ctx context.Context, clubID, clubName, videoID string, seekSec int, rtmpURL string) error {
	// Resolve CDN URL via yt-dlp (best audio, single URL line).
	resolveCtx, resolveCancel := context.WithTimeout(ctx, 40*time.Second)
	defer resolveCancel()
	log.Printf("rtmp [%.8s] yt-dlp resolving vid=%s", clubID, videoID)
	ytCmd := exec.CommandContext(resolveCtx, "yt-dlp",
		"--get-url",
		"-f", "bestaudio",
		"--no-warnings",
		"--", "https://www.youtube.com/watch?v="+videoID,
	)
	out, err := ytCmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			log.Printf("rtmp [%.8s] yt-dlp stderr: %s", clubID, strings.TrimSpace(string(ee.Stderr)))
		}
		return fmt.Errorf("yt-dlp resolve: %w", err)
	}
	log.Printf("rtmp [%.8s] yt-dlp resolved ok", clubID)
	// yt-dlp may return multiple lines for adaptive streams; take the first.
	audioURL := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if audioURL == "" {
		return fmt.Errorf("yt-dlp: no URL for %s", videoID)
	}

	qrPath, err := m.overlayPath(clubID)
	if err != nil {
		log.Printf("rtmp [%.8s] overlay gen failed (continuing without): %v", clubID, err)
		qrPath = ""
	}

	args := m.buildFFmpegArgs(clubID, clubName, audioURL, qrPath, seekSec, rtmpURL)
	log.Printf("rtmp [%.8s] ffmpeg start: seek=%ds vid=%s dst=%s", clubID, seekSec, videoID, rtmpURL)
	log.Printf("rtmp [%.8s] ffmpeg args: %s", clubID, strings.Join(append([]string{"ffmpeg"}, args...), " "))

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	// Pipe ffmpeg stderr line-by-line into the relay log for debugging.
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}
	if stderr != nil {
		go func() {
			sc := bufio.NewScanner(stderr)
			for sc.Scan() {
				log.Printf("rtmp [%.8s] ffmpeg: %s", clubID, sc.Text())
			}
		}()
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("ffmpeg exit: %w", err)
	}
	log.Printf("rtmp [%.8s] ffmpeg finished vid=%s", clubID, videoID)
	return nil
}

// rtmpFontPath is used by ffmpeg drawtext. Overridable via RTMP_FONT_PATH.
var rtmpFontPath = func() string {
	if p := os.Getenv("RTMP_FONT_PATH"); p != "" {
		return p
	}
	// Ubuntu/Debian default — always available on a standard install.
	return "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
}()

// buildFFmpegArgs composes the ffmpeg argument list. With a QR overlay:
//
//	input 0: dark background (lavfi color source, infinite)
//	input 1: QR code PNG (loop 1 → infinite)
//	input 2: YouTube CDN audio URL
//	filter: overlay QR centred + drawtext for club name and join URL
//
// Without overlay (qrPath=""): skips inputs 0+1 and just transcodes the audio.
func (m *rtmpManager) buildFFmpegArgs(clubID, clubName, audioURL, qrPath string, seekSec int, rtmpURL string) []string {
	var args []string

	if qrPath != "" {
		filter := m.buildFilterComplex(clubID, clubName)
		args = []string{
			"-loglevel", "warning",
			"-f", "lavfi", "-i", "color=c=0x0d1117:size=1280x720:rate=1",
			"-loop", "1", "-i", qrPath,
			"-i", audioURL,
			"-filter_complex", filter,
			"-map", "[vout]",
			"-map", "2:a",
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "stillimage",
			"-b:v", "800k", "-g", "2", "-r", "1",
			"-c:a", "aac", "-b:a", "128k", "-ar", "44100",
			"-shortest",
			"-f", "flv", rtmpURL,
		}
		if seekSec > 0 {
			// Seek on the audio input (index 2).
			args = insertBefore(args, "-i", audioURL, "-ss", fmt.Sprintf("%d", seekSec))
		}
	} else {
		// Fallback: audio-only (no video — some platforms require video, but this at least doesn't crash).
		args = []string{
			"-loglevel", "warning",
			"-i", audioURL,
			"-vn",
			"-c:a", "aac", "-b:a", "128k", "-ar", "44100",
			"-f", "flv", rtmpURL,
		}
		if seekSec > 0 {
			args = append([]string{"-ss", fmt.Sprintf("%d", seekSec)}, args...)
		}
	}

	return args
}

// buildFilterComplex builds the ffmpeg filter_complex string.
// The QR is centred slightly above the middle; drawtext renders club name (top) and join URL (bottom).
func (m *rtmpManager) buildFilterComplex(clubID, clubName string) string {
	name := sanitizeDrawtext(clubName)
	joinURL := sanitizeDrawtext("zapclub.io/club/" + clubID)

	fontParam := ""
	if rtmpFontPath != "" {
		fontParam = "fontfile=" + rtmpFontPath + ":"
	}

	return fmt.Sprintf(
		"[0:v][1:v]overlay=(W-w)/2:(H-h)/2-30,"+
			"drawtext=%stext='%s':x=(w-text_w)/2:y=30:fontcolor=white:fontsize=36:box=1:boxcolor=0x0d1117@0.85:boxborderw=15,"+
			"drawtext=%stext='%s':x=(w-text_w)/2:y=H-70:fontcolor=#aaaaaa:fontsize=26:box=1:boxcolor=0x0d1117@0.85:boxborderw=12"+
			"[vout]",
		fontParam, name,
		fontParam, joinURL,
	)
}

// sanitizeDrawtext removes ffmpeg drawtext special characters from user-supplied strings.
func sanitizeDrawtext(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\'', '\\', ':', '\n', '\r', '[', ']', '=', ';':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// insertBefore inserts extra args immediately before the first occurrence of target in args.
func insertBefore(args []string, target, value string, extra ...string) []string {
	for i, a := range args {
		if a == target && i+1 < len(args) && args[i+1] == value {
			out := make([]string, 0, len(args)+len(extra))
			out = append(out, args[:i]...)
			out = append(out, extra...)
			out = append(out, args[i:]...)
			return out
		}
	}
	return args
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

type rtmpHandler struct {
	mgr  *rtmpManager
	cond *conductor
}

func (h *rtmpHandler) handle(w http.ResponseWriter, r *http.Request) {
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

	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	switch r.URL.Path {
	case "/rtmp/start":
		h.handleStart(w, pubkey, body)
	case "/rtmp/stop":
		h.handleStop(w, pubkey, body)
	default:
		http.NotFound(w, r)
	}
}

func (h *rtmpHandler) handleStart(w http.ResponseWriter, pubkey string, body []byte) {
	var req struct {
		Club     string `json:"club"`
		ClubName string `json:"clubName"`
		Server   string `json:"server"` // e.g. rtmp://in.core.zap.stream:1935/good
		Key      string `json:"key"`    // stream key
	}
	if err := json.Unmarshal(body, &req); err != nil ||
		req.Club == "" || req.Server == "" || req.Key == "" {
		http.Error(w, "bad request: need club, server, key", http.StatusBadRequest)
		return
	}

	if !h.cond.isOnStage(req.Club, pubkey) {
		http.Error(w, "forbidden: not on stage", http.StatusForbidden)
		return
	}

	videoID, seekSec := h.cond.currentTrack(req.Club)
	if videoID == "" {
		http.Error(w, "no track playing", http.StatusConflict)
		return
	}

	rtmpURL := strings.TrimRight(req.Server, "/") + "/" + req.Key
	clubName := req.ClubName
	if clubName == "" {
		clubName = req.Club[:min(len(req.Club), 8)]
	}

	h.mgr.start(req.Club, clubName, pubkey, rtmpURL, videoID, seekSec)
	log.Printf("rtmp start [%.8s/%.8s] → %s seek=%ds", req.Club, pubkey, req.Server, seekSec)
	w.WriteHeader(http.StatusNoContent)
}

func (h *rtmpHandler) handleStop(w http.ResponseWriter, pubkey string, body []byte) {
	var req struct {
		Club string `json:"club"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Club == "" {
		http.Error(w, "bad request: need club", http.StatusBadRequest)
		return
	}
	h.mgr.stop(req.Club, pubkey)
	log.Printf("rtmp stop [%.8s/%.8s]", req.Club, pubkey)
	w.WriteHeader(http.StatusNoContent)
}


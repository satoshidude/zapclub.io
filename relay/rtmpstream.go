package main

import (
	"bufio"
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

	"github.com/coder/websocket"
	"github.com/skip2/go-qrcode"
)

// rtmpStream holds one active ffmpeg process that receives WebM/Opus audio from a
// browser WebSocket and pushes it as FLV/AAC to an RTMP endpoint (e.g. Twitch).
type rtmpStream struct {
	stdinW   io.WriteCloser // connected to ffmpeg's stdin
	clubName string
}

// rtmpManager manages per-(club, pusher) ffmpeg RTMP sessions.
type rtmpManager struct {
	mu      sync.Mutex
	cond    *conductor
	streams map[string]map[string]*rtmpStream // club → pusher_pubkey → stream

	qrMu    sync.Mutex
	qrPaths map[string]string // clubID → overlay PNG path
}

func newRtmpManager(cond *conductor) *rtmpManager {
	return &rtmpManager{
		cond:    cond,
		streams: map[string]map[string]*rtmpStream{},
		qrPaths: map[string]string{},
	}
}

// ── HTTP / WebSocket handlers ─────────────────────────────────────────────────

type rtmpHandler struct {
	mgr  *rtmpManager
	cond *conductor
}

func (h *rtmpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/rtmp/push/") {
		clubID := strings.TrimPrefix(r.URL.Path, "/rtmp/push/")
		h.handlePush(w, r, clubID)
		return
	}
	// Legacy CORS pre-flight from old endpoints — just return OK.
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.NotFound(w, r)
}

// handlePush upgrades the request to a WebSocket. The browser sends:
//   - first message (text/JSON): {"server":"rtmp://...","key":"...","clubName":"..."}
//   - subsequent messages (binary): WebM/Opus audio chunks from MediaRecorder
//
// The relay starts ffmpeg with stdin connected to the audio pipe and pushes RTMP.
// Auth: NIP-98 via ?auth=<base64 event> (browsers cannot set headers on WS upgrades).
func (h *rtmpHandler) handlePush(w http.ResponseWriter, r *http.Request, clubID string) {
	if clubID == "" {
		http.Error(w, "missing club id", http.StatusBadRequest)
		return
	}

	pubkey, ok := verifyNIP98QueryParam(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"zapclub.io", "www.zapclub.io"},
	})
	if err != nil {
		log.Printf("rtmp [%.8s] ws accept: %v", clubID, err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := r.Context()

	// First message: JSON config.
	_, configData, err := conn.Read(ctx)
	if err != nil {
		return
	}
	var cfg struct {
		Server   string `json:"server"`
		Key      string `json:"key"`
		ClubName string `json:"clubName"`
	}
	if err := json.Unmarshal(configData, &cfg); err != nil || cfg.Server == "" || cfg.Key == "" {
		conn.Close(websocket.StatusUnsupportedData, "bad config: need server and key")
		return
	}

	rtmpURL := strings.TrimRight(cfg.Server, "/") + "/" + cfg.Key
	clubName := cfg.ClubName
	if clubName == "" {
		if len(clubID) > 8 {
			clubName = clubID[:8]
		} else {
			clubName = clubID
		}
	}
	log.Printf("rtmp [%.8s/%.8s] start → %s", clubID, pubkey, cfg.Server)

	// Build ffmpeg command.
	qrPath, err := h.mgr.overlayPath(clubID)
	if err != nil {
		log.Printf("rtmp [%.8s] overlay gen failed (continuing without): %v", clubID, err)
		qrPath = ""
	}
	args := h.mgr.buildFFmpegArgs(clubID, clubName, qrPath, rtmpURL)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdinW, err := cmd.StdinPipe()
	if err != nil {
		conn.Close(websocket.StatusInternalError, "stdin pipe failed")
		return
	}
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		conn.Close(websocket.StatusInternalError, "ffmpeg start failed")
		return
	}
	if stderr != nil {
		go func() {
			sc := bufio.NewScanner(stderr)
			for sc.Scan() {
				log.Printf("rtmp [%.8s] ffmpeg: %s", clubID, sc.Text())
			}
		}()
	}

	// Register in active streams map.
	h.mgr.mu.Lock()
	if h.mgr.streams[clubID] == nil {
		h.mgr.streams[clubID] = map[string]*rtmpStream{}
	}
	if old := h.mgr.streams[clubID][pubkey]; old != nil {
		old.stdinW.Close() // kills the old ffmpeg by closing its stdin
	}
	s := &rtmpStream{stdinW: stdinW, clubName: clubName}
	h.mgr.streams[clubID][pubkey] = s
	h.mgr.mu.Unlock()

	defer func() {
		stdinW.Close()
		_ = cmd.Wait()
		h.mgr.mu.Lock()
		if cur := h.mgr.streams[clubID][pubkey]; cur == s {
			delete(h.mgr.streams[clubID], pubkey)
			if len(h.mgr.streams[clubID]) == 0 {
				delete(h.mgr.streams, clubID)
			}
		}
		h.mgr.mu.Unlock()
		log.Printf("rtmp [%.8s/%.8s] stopped", clubID, pubkey)
	}()

	// Pump binary WebSocket messages → ffmpeg stdin.
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if msgType == websocket.MessageBinary && len(data) > 0 {
			if _, err := stdinW.Write(data); err != nil {
				break // ffmpeg exited
			}
		}
	}
}

// overlayPath returns the QR overlay PNG for the club, generating it if needed.
func (m *rtmpManager) overlayPath(clubID string) (string, error) {
	m.qrMu.Lock()
	defer m.qrMu.Unlock()

	if p, ok := m.qrPaths[clubID]; ok {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	joinURL := "https://zapclub.io/club/" + clubID
	slug := clubID
	if len(slug) > 16 {
		slug = slug[:16]
	}
	path := filepath.Join(os.TempDir(), "zc-qr-"+slug+".png")

	q, err := qrcode.New(joinURL, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("qrcode: %w", err)
	}
	if err := q.WriteFile(400, path); err != nil {
		return "", fmt.Errorf("qrcode write: %w", err)
	}
	m.qrPaths[clubID] = path
	return path, nil
}

// buildFFmpegArgs builds the ffmpeg argument list for browser-sourced WebM/Opus input.
// With a QR overlay: background + QR image + drawtext → libx264 video + AAC audio → FLV/RTMP.
// Without overlay: audio-only FLV (some platforms require video, but this at least doesn't crash).
func (m *rtmpManager) buildFFmpegArgs(clubID, clubName, qrPath, rtmpURL string) []string {
	if qrPath != "" {
		filter := m.buildFilterComplex(clubID, clubName)
		return []string{
			"-loglevel", "warning",
			"-f", "webm", "-i", "pipe:0",
			"-f", "lavfi", "-i", "color=c=0x0d1117:size=1280x720:rate=1",
			"-loop", "1", "-i", qrPath,
			"-filter_complex", filter,
			"-map", "[vout]",
			"-map", "0:a",
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "stillimage",
			"-b:v", "800k", "-g", "2", "-r", "1",
			"-c:a", "aac", "-b:a", "128k", "-ar", "44100",
			"-f", "flv", rtmpURL,
		}
	}
	return []string{
		"-loglevel", "warning",
		"-f", "webm", "-i", "pipe:0",
		"-vn",
		"-c:a", "aac", "-b:a", "128k", "-ar", "44100",
		"-f", "flv", rtmpURL,
	}
}

func (m *rtmpManager) buildFilterComplex(clubID, clubName string) string {
	name := sanitizeDrawtext(clubName)
	joinURL := sanitizeDrawtext("zapclub.io/club/" + clubID)
	fontParam := ""
	if rtmpFontPath != "" {
		fontParam = "fontfile=" + rtmpFontPath + ":"
	}
	return fmt.Sprintf(
		"[1:v][2:v]overlay=(W-w)/2:(H-h)/2-30,"+
			"drawtext=%stext='%s':x=(w-text_w)/2:y=30:fontcolor=white:fontsize=36:box=1:boxcolor=0x0d1117@0.85:boxborderw=15,"+
			"drawtext=%stext='%s':x=(w-text_w)/2:y=H-70:fontcolor=#aaaaaa:fontsize=26:box=1:boxcolor=0x0d1117@0.85:boxborderw=12"+
			"[vout]",
		fontParam, name,
		fontParam, joinURL,
	)
}

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

// rtmpFontPath is used by ffmpeg drawtext. Overridable via RTMP_FONT_PATH.
var rtmpFontPath = func() string {
	if p := os.Getenv("RTMP_FONT_PATH"); p != "" {
		return p
	}
	return "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
}()

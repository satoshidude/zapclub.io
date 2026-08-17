package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// cacheState tracks a trackEntry's download lifecycle.
type cacheState int

const (
	cacheDownloading cacheState = iota
	cacheReady
	cacheFailed
)

// trackEntry represents one cached video file.
// Fields are written only before close(done); safe to read after done is closed.
// refs and lastUsed are guarded by trackCache.mu.
type trackEntry struct {
	videoID  string
	path     string     // <dir>/<videoID>.mp4
	state    cacheState // mutated only before close(done)
	err      error      // set on cacheFailed
	done     chan struct{}
	refs     int
	lastUsed time.Time
	created  time.Time
	priority bool // live stream acquire — download bypasses the prefetch semaphore
}

// trackCache is a shared on-disk cache for YouTube downloads.
// Downloads are serialised through dlSem so only N yt-dlp processes run
// concurrently through the WARP proxy, preventing bandwidth saturation.
// Two callers requesting the same videoID share one download (singleflight
// via the done channel).
type trackCache struct {
	mu      sync.Mutex
	dir     string
	entries map[string]*trackEntry
	failed  map[string]failedDownload // negative cache: videoID → last failure
	dlSem   chan struct{}             // cap = RADIO_DOWNLOAD_SLOTS

	bytesMu sync.Mutex
	bytes   int64 // total bytes of cacheReady files on disk
}

// failedDownload records why/when a download failed so the radio doesn't hammer
// yt-dlp (and kill the lobby stream) for a video that cannot be fetched anyway.
type failedDownload struct {
	at        time.Time
	permanent bool // geo-block / removed / private — retry much later
}

// errVidUnavailable marks a video that yt-dlp reported as unavailable (geo-block
// via the WARP exit, removed, private). Callers should skip it immediately
// instead of retrying with backoff.
var errVidUnavailable = errors.New("video unavailable")

const (
	// Transient failures ("This video is not available" via WARP is flaky) back off
	// briefly — the conductor heartbeat restarts the stream, so a mid-track recovery
	// still happens 2-3 times over a typical track length.
	failedRetryTransient = 90 * time.Second
	failedRetryPermanent = 24 * time.Hour
)

// failedRecently reports whether videoID is in the negative cache (and not yet
// expired). Caller must hold tc.mu.
func (tc *trackCache) failedRecently(videoID string) (failedDownload, bool) {
	f, ok := tc.failed[videoID]
	if !ok {
		return failedDownload{}, false
	}
	ttl := failedRetryTransient
	if f.permanent {
		ttl = failedRetryPermanent
	}
	if time.Since(f.at) > ttl {
		delete(tc.failed, videoID)
		return failedDownload{}, false
	}
	return f, true
}

var (
	radioCacheDir        = os.Getenv("RADIO_CACHE_DIR")
	radioDownloadSlots   = envInt("RADIO_DOWNLOAD_SLOTS", 1)
	radioCacheMaxAgeSec  = envInt("RADIO_CACHE_MAX_AGE_SEC", 6*3600)
	radioCacheMaxBytesMB = int64(envInt("RADIO_CACHE_MAX_BYTES_MB", 4096))
	radioCacheMaxEntries = envInt("RADIO_CACHE_MAX_ENTRIES", 256)
)

// newTrackCache creates a trackCache backed by dir. Returns nil if dir cannot
// be created.
func newTrackCache(dir string) *trackCache {
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("trackcache: mkdir %s: %v — cache disabled", dir, err)
		return nil
	}
	slots := radioDownloadSlots
	if slots < 1 {
		slots = 1
	}
	tc := &trackCache{
		dir:     dir,
		entries: map[string]*trackEntry{},
		failed:  map[string]failedDownload{},
		dlSem:   make(chan struct{}, slots),
	}
	log.Printf("trackcache: dir=%s slots=%d maxAge=%dh maxMB=%d maxEntries=%d",
		dir, slots, radioCacheMaxAgeSec/3600, radioCacheMaxBytesMB, radioCacheMaxEntries)
	tc.sweepOrphans()
	go tc.evictLoop()
	return tc
}

// getOrCreate returns an existing entry (refs++) or creates one (refs=1, fresh=true).
// When fresh the caller must call go tc.download(e).
// If the file already exists on disk from a prior run it is returned as cacheReady.
func (tc *trackCache) getOrCreate(videoID string) (*trackEntry, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if e, ok := tc.entries[videoID]; ok {
		e.refs++
		e.lastUsed = time.Now()
		return e, false
	}
	path := filepath.Join(tc.dir, videoID+".mp4")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		ch := make(chan struct{})
		close(ch)
		e := &trackEntry{
			videoID:  videoID,
			path:     path,
			state:    cacheReady,
			done:     ch,
			refs:     1,
			lastUsed: time.Now(),
			created:  time.Now(),
		}
		tc.bytes += fi.Size()
		tc.entries[videoID] = e
		return e, false
	}
	e := &trackEntry{
		videoID:  videoID,
		path:     path,
		state:    cacheDownloading,
		done:     make(chan struct{}),
		refs:     1,
		lastUsed: time.Now(),
		created:  time.Now(),
	}
	tc.entries[videoID] = e
	return e, true
}

// release decrements the ref count for videoID.
func (tc *trackCache) release(videoID string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if e, ok := tc.entries[videoID]; ok {
		if e.refs > 0 {
			e.refs--
		}
		e.lastUsed = time.Now()
	}
}

// acquireStreaming returns the entry once ≥256 KB is on disk (or the download
// is complete). The caller must call release(videoID) when done.
func (tc *trackCache) acquireStreaming(ctx context.Context, videoID string) (*trackEntry, error) {
	tc.mu.Lock()
	if _, bad := tc.failedRecently(videoID); bad {
		tc.mu.Unlock()
		// Known-bad (permanent or within the transient backoff): tell the caller to
		// fall to the lobby immediately — retry loops on a cached failure only hold
		// the station silent. The TTL alone decides when a retry is allowed.
		return nil, errVidUnavailable
	}
	tc.mu.Unlock()
	e, fresh := tc.getOrCreate(videoID)
	if fresh {
		// The station is silent until this file has data — never queue the live
		// track behind prefetch downloads (after a restart dozens of prefetches
		// flood the semaphore and the on-air track starves for minutes).
		e.priority = true
		go tc.download(e)
	}
	const minBuf = 256 << 10
	for {
		select {
		case <-e.done:
			if e.state == cacheFailed {
				tc.release(videoID)
				return nil, e.err
			}
			return e, nil
		default:
		}
		if fi, err := os.Stat(e.path); err == nil && fi.Size() >= minBuf {
			return e, nil
		}
		select {
		case <-ctx.Done():
			tc.release(videoID)
			return nil, ctx.Err()
		case <-e.done:
			if e.state == cacheFailed {
				tc.release(videoID)
				return nil, e.err
			}
			return e, nil
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// prefetch starts caching videoID in the background. No-op if already in cache
// or queued. Does not hold a lasting ref.
func (tc *trackCache) prefetch(videoID string) {
	if videoID == "" {
		return
	}
	tc.mu.Lock()
	if _, bad := tc.failedRecently(videoID); bad {
		tc.mu.Unlock()
		return // known-bad — don't churn yt-dlp on every conductor tick
	}
	tc.mu.Unlock()
	e, fresh := tc.getOrCreate(videoID)
	if fresh {
		go func() {
			tc.download(e)
			tc.release(videoID)
		}()
	} else {
		tc.release(videoID)
	}
}

// download downloads videoID via yt-dlp, gated by dlSem. Writes directly to
// e.path with --no-part so streaming readers can tail the file. On failure the
// partial file is removed and the entry deleted from the index so the next
// acquireStreaming retries.
func (tc *trackCache) download(e *trackEntry) {
	defer close(e.done)

	// Prefetches are serialised through dlSem (WARP bandwidth); live acquires
	// (e.priority) start immediately — at most one per streaming club.
	if !e.priority {
		tc.dlSem <- struct{}{}
		defer func() { <-tc.dlSem }()
	}

	log.Printf("trackcache: download start vid=%s", e.videoID)

	ytArgs := []string{
		"-f", "18/91/93/94/best[ext=mp4]/best",
		"-o", e.path,
		"--no-part",
		"--quiet",
	}
	if ytdlpProxy != "" {
		ytArgs = append(ytArgs, "--proxy", ytdlpProxy)
	}
	ytArgs = append(ytArgs, "--", e.videoID)

	ytCmd := exec.Command("yt-dlp", ytArgs...)
	var stderr bytes.Buffer
	ytCmd.Stderr = io.MultiWriter(log.Writer(), &stderr)
	if err := ytCmd.Run(); err != nil {
		permanent := isVidUnavailable(stderr.String())
		log.Printf("trackcache: download failed vid=%s: %v (permanent=%v)", e.videoID, err, permanent)
		os.Remove(e.path)
		tc.mu.Lock()
		if tc.entries[e.videoID] == e {
			delete(tc.entries, e.videoID)
		}
		// Negative-cache the failure so acquire/prefetch skip this video instead of
		// re-running yt-dlp on every conductor tick (which stalls the stream for
		// minutes on geo-blocked tracks).
		tc.failed[e.videoID] = failedDownload{at: time.Now(), permanent: permanent}
		tc.mu.Unlock()
		if permanent {
			err = errVidUnavailable
		}
		e.err = err
		e.state = cacheFailed
		return
	}

	fi, _ := os.Stat(e.path)
	if fi != nil {
		tc.bytesMu.Lock()
		tc.bytes += fi.Size()
		tc.bytesMu.Unlock()
		log.Printf("trackcache: download done vid=%s size=%dMB", e.videoID, fi.Size()>>20)
	} else {
		log.Printf("trackcache: download done vid=%s", e.videoID)
	}
	e.state = cacheReady
}

// isVidUnavailable classifies yt-dlp stderr output: true only for failures that
// will not resolve by retrying (removed, private, explicit country block, age
// gate). The generic "This video is not available" is deliberately NOT here:
// through the WARP exit that error is flaky — the same video often downloads
// fine minutes later — so it gets the short transient backoff instead of 24 h.
func isVidUnavailable(stderr string) bool {
	s := strings.ToLower(stderr)
	for _, marker := range []string{
		"not available in your country",
		"who has blocked it",
		"private video",
		"has been removed",
		"age-restricted",
		"sign in to confirm your age",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// sweepOrphans removes stale temp files from a previous run.
func (tc *trackCache) sweepOrphans() {
	entries, err := os.ReadDir(tc.dir)
	if err != nil {
		return
	}
	n := 0
	for _, de := range entries {
		if !de.IsDir() && filepath.Ext(de.Name()) == ".tmp" {
			os.Remove(filepath.Join(tc.dir, de.Name()))
			n++
		}
	}
	if n > 0 {
		log.Printf("trackcache: swept %d orphan temp file(s)", n)
	}
}

// evictLoop runs periodic eviction to enforce age, size, and count limits.
func (tc *trackCache) evictLoop() {
	for range time.Tick(60 * time.Second) {
		tc.evict()
	}
}

func (tc *trackCache) evict() {
	maxAge := time.Duration(radioCacheMaxAgeSec) * time.Second
	maxBytes := radioCacheMaxBytesMB << 20
	now := time.Now()

	tc.mu.Lock()
	type cand struct {
		vid      string
		e        *trackEntry
		lastUsed time.Time
		size     int64
	}
	var cands []cand
	totalEntries := 0
	for vid, e := range tc.entries {
		if e.state == cacheReady {
			totalEntries++
		}
		if e.state != cacheReady || e.refs > 0 {
			continue
		}
		fi, err := os.Stat(e.path)
		if err != nil {
			delete(tc.entries, vid)
			continue
		}
		cands = append(cands, cand{vid, e, e.lastUsed, fi.Size()})
	}
	tc.mu.Unlock()

	tc.bytesMu.Lock()
	totalBytes := tc.bytes
	tc.bytesMu.Unlock()

	// Sort oldest-first (insertion sort; N is small).
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && cands[j].lastUsed.Before(cands[j-1].lastUsed); j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}

	for _, c := range cands {
		tooOld := now.Sub(c.lastUsed) > maxAge
		overCount := totalEntries > radioCacheMaxEntries
		overSize := totalBytes > maxBytes
		if !tooOld && !overCount && !overSize {
			break
		}
		tc.mu.Lock()
		e, ok := tc.entries[c.vid]
		if ok && e == c.e && e.refs == 0 && e.state == cacheReady {
			delete(tc.entries, c.vid)
			tc.mu.Unlock()
			os.Remove(c.e.path)
			tc.bytesMu.Lock()
			tc.bytes -= c.size
			if tc.bytes < 0 {
				tc.bytes = 0
			}
			tc.bytesMu.Unlock()
			totalBytes -= c.size
			totalEntries--
			log.Printf("trackcache: evicted vid=%s age=%s size=%dMB",
				c.vid, now.Sub(c.lastUsed).Round(time.Minute), c.size>>20)
		} else {
			tc.mu.Unlock()
		}
	}
}

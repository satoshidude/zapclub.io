package main

import (
	"encoding/json"
	"math/big"
	"net/http"
	"sort"
)

const (
	djLeaderboardTopN    = 100
	trackLeaderboardTopN = 10
	djScorePrior         = 10 // ten settled tracks reach 50% confidence
	djTrackPointMax      = 6  // one play point plus at most five accepted bangers
)

// trackLeaderboardEntry is a ranked, settled performance. Club and DJ stay on
// the row so the public API never implies that a YouTube title belongs to one
// DJ globally. Only aggregate Banger totals leave the relay.
type trackLeaderboardEntry struct {
	Rank      int    `json:"rank"`
	Club      string `json:"club"`
	VideoID   string `json:"videoId"`
	Title     string `json:"title"`
	DJ        string `json:"dj"`
	Bangers   int    `json:"bangers"`
	Skipped   bool   `json:"skipped"`
	StartedAt int64  `json:"startedAt"`
}

// djLeaderboardEntry is the public, relay-derived view of one DJ's settled
// performance. Score is stored as integer tenths so the JSON contract avoids
// floating-point ordering differences (333 renders as 33.3).
type djLeaderboardEntry struct {
	Pubkey    string `json:"pubkey"`
	Rank      int    `json:"rank"`
	Score     int    `json:"score"`
	Tracks    int    `json:"tracks"`
	Bangers   int    `json:"bangers"`
	Skipped   int    `json:"skipped"`
	VibeScore int    `json:"vibeScore"`

	earned      *big.Int
	denominator *big.Int
}

// djEarnedPoints converts the canonical credibility aggregate into track
// points. A normally settled track earns one play point plus 0..5 Vibemeter
// bangers; a community-skipped track earns zero because its canonical score is
// -1. The clamp protects the public calculation from malformed legacy data.
func djEarnedPoints(tracks, vibeScore int) *big.Int {
	if tracks <= 0 {
		return new(big.Int)
	}
	trackCount := big.NewInt(int64(tracks))
	earned := new(big.Int).Add(new(big.Int).Set(trackCount), big.NewInt(int64(vibeScore)))
	maximum := new(big.Int).Mul(new(big.Int).Set(trackCount), big.NewInt(djTrackPointMax))
	if earned.Sign() <= 0 {
		return new(big.Int)
	}
	if earned.Cmp(maximum) > 0 {
		return maximum
	}
	return earned
}

// djScoreTenths combines quality and experience:
//
//	score = 100 * earned / (6 * tracks) * tracks / (tracks + 10)
//	      = 100 * earned / (6 * (tracks + 10))
//
// The ten-track prior shrinks tiny samples, while DJs who merely accumulate
// unvoted tracks asymptotically top out at 16.7 instead of outranking strong
// community feedback through volume alone.
func djScoreTenths(tracks, vibeScore int) int {
	if tracks <= 0 {
		return 0
	}
	earned := djEarnedPoints(tracks, vibeScore)
	denominator := new(big.Int).Add(big.NewInt(int64(tracks)), big.NewInt(djScorePrior))
	denominator.Mul(denominator, big.NewInt(djTrackPointMax))
	numerator := new(big.Int).Mul(new(big.Int).Set(earned), big.NewInt(1000))
	numerator.Add(numerator, new(big.Int).Quo(new(big.Int).Set(denominator), big.NewInt(2)))
	return int(numerator.Quo(numerator, denominator).Int64())
}

func newDJLeaderboardEntry(entry *credibilityEntry) djLeaderboardEntry {
	earned := djEarnedPoints(entry.Tracks, entry.Score)
	return djLeaderboardEntry{
		Pubkey:      entry.Pubkey,
		Score:       djScoreTenths(entry.Tracks, entry.Score),
		Tracks:      entry.Tracks,
		Bangers:     entry.Bangers,
		Skipped:     entry.Skipped,
		VibeScore:   entry.Score,
		earned:      earned,
		denominator: new(big.Int).Add(big.NewInt(int64(entry.Tracks)), big.NewInt(djScorePrior)),
	}
}

// ranked returns one deterministic ordering shared by the top-list and
// per-profile rank endpoint. It compares the unrounded score fractions, then
// prefers the larger settled sample and finally the lexical pubkey.
func (b *credibilityBoard) ranked() []djLeaderboardEntry {
	b.mu.Lock()
	all := make([]djLeaderboardEntry, 0, len(b.By))
	for _, entry := range b.By {
		if entry == nil || entry.Pubkey == "" || entry.Tracks <= 0 {
			continue
		}
		all = append(all, newDJLeaderboardEntry(entry))
	}
	b.mu.Unlock()

	sort.Slice(all, func(i, j int) bool {
		a, z := all[i], all[j]
		aWeighted := new(big.Int).Mul(a.earned, z.denominator)
		zWeighted := new(big.Int).Mul(z.earned, a.denominator)
		if comparison := aWeighted.Cmp(zWeighted); comparison != 0 {
			return comparison > 0
		}
		if a.Tracks != z.Tracks {
			return a.Tracks > z.Tracks
		}
		return a.Pubkey < z.Pubkey
	})
	for i := range all {
		all[i].Rank = i + 1
	}
	return all
}

func (b *credibilityBoard) djRankOf(pubkey string) (entry djLeaderboardEntry, total int, ok bool) {
	all := b.ranked()
	for _, candidate := range all {
		if candidate.Pubkey == pubkey {
			return candidate, len(all), true
		}
	}
	return djLeaderboardEntry{}, len(all), false
}

func (b *credibilityBoard) djTop(n int) (entries []djLeaderboardEntry, total int) {
	all := b.ranked()
	total = len(all)
	if n < 0 {
		n = 0
	}
	if len(all) > n {
		all = all[:n]
	}
	return all, total
}

func (b *credibilityBoard) trackTop(n int) []trackLeaderboardEntry {
	b.mu.Lock()
	performances := append([]credibilityTrack(nil), b.TrackPerformances...)
	b.mu.Unlock()

	sort.Slice(performances, func(i, j int) bool {
		a, z := performances[i], performances[j]
		if a.Bangers != z.Bangers {
			return a.Bangers > z.Bangers
		}
		if a.StartedAt != z.StartedAt {
			return a.StartedAt > z.StartedAt
		}
		if a.Club != z.Club {
			return a.Club < z.Club
		}
		if a.DJ != z.DJ {
			return a.DJ < z.DJ
		}
		return a.VideoID < z.VideoID
	})
	if n < 0 {
		n = 0
	}
	top := make([]trackLeaderboardEntry, 0, n)
	for _, track := range performances {
		if len(top) >= n {
			break
		}
		if track.Bangers <= 0 || track.Club == "" || track.DJ == "" || track.StartedAt <= 0 {
			continue
		}
		bangers := track.Bangers
		if bangers > moodBangerMax {
			bangers = moodBangerMax
		}
		top = append(top, trackLeaderboardEntry{
			Rank: len(top) + 1, Club: track.Club, VideoID: track.VideoID,
			Title: track.Title, DJ: track.DJ, Bangers: bangers,
			Skipped: track.Skipped, StartedAt: track.StartedAt,
		})
	}
	return top
}

// handleLeaderboard serves the public DJ-performance ranking. Zaps never enter
// this calculation; they remain available only through the separate zap APIs.
func (b *credibilityBoard) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Vary", "Origin")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=30")
	enc := json.NewEncoder(w)
	if pubkey := r.URL.Query().Get("pubkey"); pubkey != "" {
		entry, total, ok := b.djRankOf(pubkey)
		if !ok {
			_ = enc.Encode(map[string]any{"ranked": false, "total": total})
			return
		}
		_ = enc.Encode(struct {
			Ranked bool `json:"ranked"`
			Total  int  `json:"total"`
			djLeaderboardEntry
		}{Ranked: true, Total: total, djLeaderboardEntry: entry})
		return
	}
	entries, total := b.djTop(djLeaderboardTopN)
	_ = enc.Encode(map[string]any{
		"total":     total,
		"top":       entries,
		"topTracks": b.trackTop(trackLeaderboardTopN),
	})
}

package main

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"sync"
)

// kindCredibility uses NIP-78's addressable application-data kind. The relay signs one
// replaceable snapshot per DJ (d=zapclub:credibility:<pubkey>), so the score can be read as a
// normal Nostr event while the local JSON file remains the authoritative durable aggregate.
const kindCredibility = 30078
const credibilityNamespace = "zapclub-credibility"
const credibilityTrackHistoryMax = 100

type credibilityEntry struct {
	Pubkey  string `json:"pubkey"`
	Score   int    `json:"score"`
	Tracks  int    `json:"tracks"`
	Bangers int    `json:"bangers"`
	Skipped int    `json:"skipped"`
}

// credibilityTrack is one settled public performance. It deliberately stores
// only the aggregate Vibemeter result, never the voters behind it. Keeping the
// club and controlling DJ on the performance (instead of aggregating by video)
// makes every public leaderboard row attributable without inventing a single
// owner for a track that may have been played in several clubs.
type credibilityTrack struct {
	Club      string `json:"club"`
	VideoID   string `json:"video_id"`
	Title     string `json:"title"`
	DJ        string `json:"dj"`
	Bangers   int    `json:"bangers"`
	Skipped   bool   `json:"skipped,omitempty"`
	AutoDJ    bool   `json:"auto_dj,omitempty"`
	StartedAt int64  `json:"started_at"`
}

type trackSettlement uint8

const (
	trackPlayed trackSettlement = iota
	trackCommunitySkipped
	trackDiscarded
)

type credibilityBoard struct {
	mu                sync.Mutex
	path              string
	By                map[string]*credibilityEntry `json:"by"`
	LastTrack         map[string]string            `json:"last_track"`
	TrackPerformances []credibilityTrack           `json:"track_performances,omitempty"`
}

func newCredibilityBoard(path string) *credibilityBoard {
	b := &credibilityBoard{
		path:      path,
		By:        map[string]*credibilityEntry{},
		LastTrack: map[string]string{},
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, b)
	}
	if b.By == nil {
		b.By = map[string]*credibilityEntry{}
	}
	if b.LastTrack == nil {
		b.LastTrack = map[string]string{}
	}
	b.path = path
	return b
}

// record settles exactly one outcome for a track. A naturally played track receives its accepted
// bangers, a community-skipped track is always -1, and manual/broken transitions are only marked
// as handled so they cannot farm play volume. LastTrack makes a repeated conductor transition or
// restart idempotent; the separate performance history remains explicitly bounded.
func (b *credibilityBoard) record(club string, pos int, startedAt int64, videoID, title, dj string, bangers int, settlement trackSettlement, publishTrack bool) (credibilityEntry, bool) {
	return b.recordSettlement(club, pos, startedAt, videoID, title, dj, bangers, settlement, publishTrack, false, true)
}

// recordAutoTrack stores the attributable public track result while leaving the
// owner's personal DJ credibility untouched. The owner remains the controller
// attached to the performance, and AutoDJ lets clients label that distinction.
func (b *credibilityBoard) recordAutoTrack(club string, pos int, startedAt int64, videoID, title, owner string, bangers int, settlement trackSettlement, publishTrack bool) bool {
	_, changed := b.recordSettlement(club, pos, startedAt, videoID, title, owner, bangers, settlement, publishTrack, true, false)
	return changed
}

func (b *credibilityBoard) recordSettlement(club string, pos int, startedAt int64, videoID, title, dj string, bangers int, settlement trackSettlement, publishTrack, autoDJ, affectCredibility bool) (credibilityEntry, bool) {
	if club == "" || dj == "" || pos < 0 || startedAt <= 0 {
		return credibilityEntry{}, false
	}
	trackKey := strconv.Itoa(pos) + ":" + strconv.FormatInt(startedAt, 10)

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.LastTrack[club] == trackKey {
		return credibilityEntry{}, false
	}
	b.LastTrack[club] = trackKey
	if settlement == trackDiscarded {
		// Persist the dedup marker so a subsequent stop or restart cannot reclassify this
		// discarded transition as a naturally played track.
		b.saveLocked()
		return credibilityEntry{}, false
	}

	if bangers < 0 {
		bangers = 0
	}
	if bangers > moodBangerMax {
		bangers = moodBangerMax
	}
	var settled credibilityEntry
	if affectCredibility {
		entry := b.By[dj]
		if entry == nil {
			entry = &credibilityEntry{Pubkey: dj}
			b.By[dj] = entry
		}
		entry.Tracks++
		entry.Bangers += bangers
		if settlement == trackCommunitySkipped {
			entry.Score--
			entry.Skipped++
		} else {
			entry.Score += bangers
		}
		settled = *entry
	}
	if publishTrack {
		b.TrackPerformances = append(b.TrackPerformances, credibilityTrack{
			Club: club, VideoID: videoID, Title: title, DJ: dj, Bangers: bangers,
			Skipped: settlement == trackCommunitySkipped, AutoDJ: autoDJ, StartedAt: startedAt,
		})
		b.trimTrackPerformancesLocked()
	}

	// A track finishes at most every few seconds; persisting here keeps an unclean relay restart
	// from losing settled credibility. The rename is atomic on the production filesystem.
	b.saveLocked()
	return settled, true
}

// Keep persistence bounded while retaining the performances that can still
// matter to the public Top 10. Among equal vote counts, newer plays win.
func (b *credibilityBoard) trimTrackPerformancesLocked() {
	sort.SliceStable(b.TrackPerformances, func(i, j int) bool {
		a, z := b.TrackPerformances[i], b.TrackPerformances[j]
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
	if len(b.TrackPerformances) > credibilityTrackHistoryMax {
		b.TrackPerformances = b.TrackPerformances[:credibilityTrackHistoryMax]
	}
}

func (b *credibilityBoard) saveLocked() {
	data, err := json.Marshal(b)
	if err != nil {
		return
	}
	tmp := b.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) != nil {
		return
	}
	_ = os.Rename(tmp, b.path)
}

func (b *credibilityBoard) save() {
	b.mu.Lock()
	b.saveLocked()
	b.mu.Unlock()
}

package main

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
)

// kindCredibility uses NIP-78's addressable application-data kind. The relay signs one
// replaceable snapshot per DJ (d=zapclub:credibility:<pubkey>), so the score can be read as a
// normal Nostr event while the local JSON file remains the authoritative durable aggregate.
const kindCredibility = 30078
const credibilityNamespace = "zapclub-credibility"

type credibilityEntry struct {
	Pubkey  string `json:"pubkey"`
	Score   int    `json:"score"`
	Tracks  int    `json:"tracks"`
	Bangers int    `json:"bangers"`
	Skipped int    `json:"skipped"`
}

type credibilityBoard struct {
	mu        sync.Mutex
	path      string
	By        map[string]*credibilityEntry `json:"by"`
	LastTrack map[string]string            `json:"last_track"`
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

// record settles exactly one score for a played track. A community-skipped track is always -1;
// otherwise each accepted banger contributes +1, capped at +5. LastTrack makes a repeated
// conductor transition or restart idempotent without retaining an unbounded event history.
func (b *credibilityBoard) record(club string, pos int, startedAt int64, dj string, bangers int, skipped bool) (credibilityEntry, bool) {
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

	entry := b.By[dj]
	if entry == nil {
		entry = &credibilityEntry{Pubkey: dj}
		b.By[dj] = entry
	}
	if bangers < 0 {
		bangers = 0
	}
	if bangers > moodBangerMax {
		bangers = moodBangerMax
	}
	entry.Tracks++
	entry.Bangers += bangers
	if skipped {
		entry.Score--
		entry.Skipped++
	} else {
		entry.Score += bangers
	}

	// A track finishes at most every few seconds; persisting here keeps an unclean relay restart
	// from losing settled credibility. The rename is atomic on the production filesystem.
	b.saveLocked()
	return *entry, true
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

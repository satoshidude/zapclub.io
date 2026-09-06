package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestDJScoreTenthsBalancesVibeAndExperience(t *testing.T) {
	tests := []struct {
		name      string
		tracks    int
		vibeScore int
		want      int
	}{
		{"unranked", 0, 0, 0},
		{"one community skip", 1, -1, 0},
		{"one unvoted play", 1, 0, 15},
		{"one perfect play", 1, 5, 91},
		{"five unvoted plays", 5, 0, 56},
		{"five perfect plays", 5, 25, 333},
		{"ten unvoted plays", 10, 0, 83},
		{"ten mixed plays", 10, 14, 200},
		{"ten good plays", 10, 20, 250},
		{"ten perfect plays", 10, 50, 500},
		{"twenty good plays", 20, 40, 333},
		{"volume without votes", 100, 0, 152},
		{"only community skips", 100, -100, 0},
		{"malformed high score is capped", 10, 500, 500},
		{"malformed low score is floored", 10, -500, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := djScoreTenths(test.tracks, test.vibeScore); got != test.want {
				t.Fatalf("djScoreTenths(%d, %d) = %d, want %d", test.tracks, test.vibeScore, got, test.want)
			}
		})
	}
}

func TestDJScoreTenthsHandlesExtremeMalformedAggregates(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got := djScoreTenths(maxInt/5, maxInt); got < 0 || got > 1000 {
		t.Fatalf("high malformed aggregate produced out-of-range score %d", got)
	}
	if got := djScoreTenths(maxInt, -maxInt); got != 0 {
		t.Fatalf("low malformed aggregate produced score %d, want 0", got)
	}
}

func TestDJLeaderboardRanksByVotesThenTracks(t *testing.T) {
	b := newCredibilityBoard(filepath.Join(t.TempDir(), "credibility.json"))
	b.By = map[string]*credibilityEntry{
		"most-votes": {Pubkey: "most-votes", Tracks: 100, Score: 60, Bangers: 60},
		"best-score": {Pubkey: "best-score", Tracks: 10, Score: 50, Bangers: 50},
		"veteran":    {Pubkey: "veteran", Tracks: 20, Score: 20, Bangers: 25},
		"rookie":     {Pubkey: "rookie", Tracks: 5, Score: 25, Bangers: 25},
		"alpha":      {Pubkey: "alpha", Tracks: 10, Score: 20, Bangers: 20},
		"beta":       {Pubkey: "beta", Tracks: 10, Score: 20, Bangers: 20},
		"volume":     {Pubkey: "volume", Tracks: 100, Score: 0},
		"skipped":    {Pubkey: "skipped", Tracks: 1, Score: -1, Skipped: 1},
		"empty":      {Pubkey: "empty", Tracks: 0, Score: 99, Bangers: 99},
	}

	entries, total := b.djTop(100)
	if total != 6 || len(entries) != 6 {
		t.Fatalf("ranked total=%d len=%d, want 6", total, len(entries))
	}
	want := []string{"most-votes", "best-score", "veteran", "rookie", "alpha", "beta"}
	for i, pubkey := range want {
		if entries[i].Pubkey != pubkey || entries[i].Rank != i+1 {
			t.Fatalf("entry %d = %+v, want %s at rank %d", i, entries[i], pubkey, i+1)
		}
	}
	if entries[0].Score >= entries[1].Score {
		t.Fatal("fixture must prove votes outrank a higher legacy score")
	}
	if rank, rankTotal, ok := b.djRankOf("rookie"); !ok || rankTotal != total || rank.Rank != 4 {
		t.Fatalf("rookie rank = %+v, total=%d, ok=%v; top and profile rank must agree", rank, rankTotal, ok)
	}
	if _, _, ok := b.djRankOf("volume"); ok {
		t.Fatal("a DJ without accepted votes must remain unranked")
	}
}

func TestDJLeaderboardHTTPLimitsDJsToTen(t *testing.T) {
	b := newCredibilityBoard(filepath.Join(t.TempDir(), "credibility.json"))
	for i := 1; i <= 12; i++ {
		pubkey := fmt.Sprintf("dj-%02d", i)
		b.By[pubkey] = &credibilityEntry{Pubkey: pubkey, Tracks: i, Bangers: i, Score: i}
	}
	recorder := httptest.NewRecorder()
	b.handleLeaderboard(recorder, httptest.NewRequest(http.MethodGet, "/leaderboard", nil))
	var response struct {
		Total int                  `json:"total"`
		Top   []djLeaderboardEntry `json:"top"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 12 || len(response.Top) != 10 || response.Top[9].Pubkey != "dj-03" {
		t.Fatalf("expected ten ranked DJs and full total: %+v", response)
	}
	if rank, total, ok := b.djRankOf("dj-01"); !ok || total != 12 || rank.Rank != 12 {
		t.Fatalf("profile rank beyond top ten = %+v, total=%d, ok=%v", rank, total, ok)
	}
}

func TestDJLeaderboardHTTPUsesCredibilityNotZaps(t *testing.T) {
	b := newCredibilityBoard(filepath.Join(t.TempDir(), "credibility.json"))
	b.By = map[string]*credibilityEntry{
		"alice": {Pubkey: "alice", Tracks: 10, Score: 20, Bangers: 20},
		"bob":   {Pubkey: "bob", Tracks: 1, Score: -1, Bangers: 2, Skipped: 1},
	}
	b.TrackPerformances = []credibilityTrack{
		{Club: "club-b", VideoID: "video-2", Title: "Newest tie", DJ: "bob", Bangers: 5, AutoDJ: true, StartedAt: 3000},
		{Club: "club-a", VideoID: "video-1", Title: "Older tie", DJ: "alice", Bangers: 5, StartedAt: 2000},
		{Club: "club-a", VideoID: "video-3", Title: "Skipped but liked", DJ: "alice", Bangers: 3, Skipped: true, StartedAt: 4000},
		{Club: "club-c", VideoID: "video-4", Title: "No likes", DJ: "carol", Bangers: 0, StartedAt: 5000},
	}

	topRecorder := httptest.NewRecorder()
	b.handleLeaderboard(topRecorder, httptest.NewRequest(http.MethodGet, "/leaderboard", nil))
	if topRecorder.Code != http.StatusOK {
		t.Fatalf("GET /leaderboard = %d: %s", topRecorder.Code, topRecorder.Body.String())
	}
	var top struct {
		Total     int                     `json:"total"`
		Top       []djLeaderboardEntry    `json:"top"`
		TopTracks []trackLeaderboardEntry `json:"topTracks"`
	}
	if err := json.Unmarshal(topRecorder.Body.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	if top.Total != 2 || len(top.Top) != 2 || top.Top[0].Pubkey != "alice" ||
		top.Top[0].Score != 250 || top.Top[0].Tracks != 10 || top.Top[0].VibeScore != 20 {
		t.Fatalf("leaderboard response = %+v", top)
	}
	if len(top.TopTracks) != 3 || top.TopTracks[0].Title != "Newest tie" ||
		top.TopTracks[0].Rank != 1 || !top.TopTracks[0].AutoDJ || top.TopTracks[1].Title != "Older tie" ||
		!top.TopTracks[2].Skipped || top.TopTracks[2].Bangers != 3 {
		t.Fatalf("track leaderboard response = %+v", top.TopTracks)
	}
	if string(topRecorder.Body.Bytes()) == "" || json.Valid(topRecorder.Body.Bytes()) == false {
		t.Fatal("leaderboard response must be valid JSON")
	}

	rankRecorder := httptest.NewRecorder()
	b.handleLeaderboard(rankRecorder, httptest.NewRequest(http.MethodGet, "/leaderboard?pubkey=bob", nil))
	var rank struct {
		Ranked bool `json:"ranked"`
		Total  int  `json:"total"`
		djLeaderboardEntry
	}
	if err := json.Unmarshal(rankRecorder.Body.Bytes(), &rank); err != nil {
		t.Fatal(err)
	}
	if !rank.Ranked || rank.Total != 2 || rank.Pubkey != "bob" || rank.Rank != 2 || rank.Score != 0 {
		t.Fatalf("profile rank response = %+v", rank)
	}

	unrankedRecorder := httptest.NewRecorder()
	b.handleLeaderboard(unrankedRecorder, httptest.NewRequest(http.MethodGet, "/leaderboard?pubkey=carol", nil))
	var unranked map[string]any
	if err := json.Unmarshal(unrankedRecorder.Body.Bytes(), &unranked); err != nil {
		t.Fatal(err)
	}
	if unranked["ranked"] != false || unranked["total"] != float64(2) {
		t.Fatalf("unranked response = %+v", unranked)
	}

	postRecorder := httptest.NewRecorder()
	b.handleLeaderboard(postRecorder, httptest.NewRequest(http.MethodPost, "/leaderboard", nil))
	if postRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /leaderboard = %d, want 405", postRecorder.Code)
	}
}

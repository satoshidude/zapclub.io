package main

import "testing"

// The relay is the sole conductor: now_playing (30100) and the play-log (1313) may be written
// ONLY by the relay key. Clients write stage (30102), queue (30103), chat (9), skip (30107).
func TestIsForeignConductorWrite(t *testing.T) {
	const relay = "b095f434relaykey"
	const client = "deadbeefclientkey"

	cases := []struct {
		name    string
		kind    int
		pubkey  string
		foreign bool
	}{
		{"relay now_playing ok", kindNowPlaying, relay, false},
		{"relay play-log ok", kindPlay, relay, false},
		{"client now_playing blocked", kindNowPlaying, client, true},
		{"client play-log blocked", kindPlay, client, true},
		{"client stage allowed", kindStage, client, false},
		{"client queue allowed", kindQueue, client, false},
		{"client skip allowed", kindSkip, client, false},
		{"client chat allowed", 9, client, false},
	}
	for _, c := range cases {
		if got := isForeignConductorWrite(c.kind, c.pubkey, relay); got != c.foreign {
			t.Errorf("%s: isForeignConductorWrite(kind=%d) = %v, want %v", c.name, c.kind, got, c.foreign)
		}
	}
}

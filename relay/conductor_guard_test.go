package main

import "testing"

// The relay is the sole author of playback, credibility and aggregate listener/member state. Clients
// write stage (30102), queue (30103), chat (9), skip (30107), mood (20104) and listener beats
// (20105).
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
		{"relay credibility ok", kindCredibility, relay, false},
		{"client credibility blocked", kindCredibility, client, true},
		{"relay listener count ok", kindListenerCount, relay, false},
		{"client listener count blocked", kindListenerCount, client, true},
		{"relay member count ok", kindMemberCount, relay, false},
		{"client member count blocked", kindMemberCount, client, true},
		{"client listener beat allowed", kindListenerBeat, client, false},
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

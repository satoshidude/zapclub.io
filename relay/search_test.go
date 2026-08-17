package main

import "testing"

func TestBuildTitle(t *testing.T) {
	cases := []struct{ title, artist, channel, want string }{
		{"Bohemian Rhapsody", "Queen", "", "Queen – Bohemian Rhapsody"},         // yt-dlp artist present
		{"Queen - Bohemian Rhapsody", "Queen", "", "Queen - Bohemian Rhapsody"}, // already present → unchanged
		{"Some Song", "", "", "Some Song"},                                      // no artist, no channel
		{"Some Song", "NA", "NA", "Some Song"},                                  // yt-dlp's null markers
		{"queen live", "Queen", "", "queen live"},                              // case-insensitive dedup
		{"Song", "  Artist  ", "", "Artist – Song"},                            // trims whitespace
		// channel fallback (flat-playlist: artist is NA, channel carries the artist):
		{"Rosenrot", "NA", "Rammstein Official", "Rammstein – Rosenrot"},        // " Official" marker stripped
		{"Blinding Lights", "NA", "The Weeknd - Topic", "The Weeknd – Blinding Lights"}, // YT Music "- Topic"
		{"Diamonds", "NA", "RihannaVEVO", "Rihanna – Diamonds"},                 // "VEVO" stripped
		{"Rammstein - Rosenrot (4K)", "NA", "Rammstein Official", "Rammstein - Rosenrot (4K)"}, // artist already in title
		{"Rammstein Cover Live", "NA", "Super chuvak", "Rammstein Cover Live"},  // plain uploader → no pollution
	}
	for _, c := range cases {
		if got := buildTitle(c.title, c.artist, c.channel); got != c.want {
			t.Errorf("buildTitle(%q, %q, %q) = %q, want %q", c.title, c.artist, c.channel, got, c.want)
		}
	}
}

func TestParseYtLines(t *testing.T) {
	out := []byte("vid1\tBohemian Rhapsody\t355\tQueen\tQueenVEVO\nvid2\tArtist - Song\t200\tArtist\tArtist\nvid3\tRosenrot\t233\tNA\tRammstein - Topic\nbad\n\tno-id\t1\tx\ty")
	got := parseYtLines(out)
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3 (malformed/empty-id lines dropped)", len(got))
	}
	if got[0].Title != "Queen – Bohemian Rhapsody" || got[0].Duration != 355 || got[0].ID != "vid1" {
		t.Errorf("result[0] = %+v", got[0])
	}
	if got[1].Title != "Artist - Song" { // artist already in title → not doubled
		t.Errorf("result[1].Title = %q", got[1].Title)
	}
	if got[2].Title != "Rammstein – Rosenrot" { // channel "- Topic" fallback fills the bare title
		t.Errorf("result[2].Title = %q", got[2].Title)
	}
}

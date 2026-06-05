package main

import "testing"

func TestBuildTitle(t *testing.T) {
	cases := []struct{ title, artist, want string }{
		{"Bohemian Rhapsody", "Queen", "Queen – Bohemian Rhapsody"},     // YT Music: artist missing from title
		{"Queen - Bohemian Rhapsody", "Queen", "Queen - Bohemian Rhapsody"}, // already present → unchanged
		{"Some Song", "", "Some Song"},                                  // no artist
		{"Some Song", "NA", "Some Song"},                                // yt-dlp's null marker
		{"queen live", "Queen", "queen live"},                           // case-insensitive dedup
		{"Song", "  Artist  ", "Artist – Song"},                         // trims whitespace
	}
	for _, c := range cases {
		if got := buildTitle(c.title, c.artist); got != c.want {
			t.Errorf("buildTitle(%q, %q) = %q, want %q", c.title, c.artist, got, c.want)
		}
	}
}

func TestParseYtLines(t *testing.T) {
	out := []byte("vid1\tBohemian Rhapsody\t355\tQueen\nvid2\tArtist - Song\t200\tArtist\nbad\n\tno-id\t1\tx")
	got := parseYtLines(out)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2 (malformed/empty-id lines dropped)", len(got))
	}
	if got[0].Title != "Queen – Bohemian Rhapsody" || got[0].Duration != 355 || got[0].ID != "vid1" {
		t.Errorf("result[0] = %+v", got[0])
	}
	if got[1].Title != "Artist - Song" { // artist already in title → not doubled
		t.Errorf("result[1].Title = %q", got[1].Title)
	}
}

package main

import (
	"reflect"
	"testing"
)

// allActive builds a playable matrix with every track active.
func allActive(counts ...int) [][]bool {
	m := make([][]bool, len(counts))
	for i, n := range counts {
		row := make([]bool, n)
		for j := range row {
			row[j] = true
		}
		m[i] = row
	}
	return m
}

func TestPosToSlot(t *testing.T) {
	cases := []struct{ pos, n, di, ti int }{
		{0, 2, 0, 0}, {1, 2, 1, 0}, {2, 2, 0, 1}, {3, 2, 1, 1},
		{18, 2, 0, 9}, // the diskbuster repro pos
		{7, 1, 0, 7},  // single DJ: pos == trackIndex
	}
	for _, c := range cases {
		di, ti := posToSlot(c.pos, c.n)
		if di != c.di || ti != c.ti {
			t.Errorf("posToSlot(%d,%d)=(%d,%d) want (%d,%d)", c.pos, c.n, di, ti, c.di, c.ti)
		}
	}
}

func TestNextPlayablePos(t *testing.T) {
	p := allActive(3, 3)
	if got := nextPlayablePos(-1, 2, p); got != 0 {
		t.Errorf("interleave start: got %d want 0", got)
	}
	if got := nextPlayablePos(0, 2, p); got != 1 {
		t.Errorf("interleave: got %d want 1", got)
	}
	if got := nextPlayablePos(1, 2, p); got != 2 {
		t.Errorf("interleave: got %d want 2", got)
	}

	// dj0 has 3 tracks, dj1 has 1 → shorter queue skipped.
	short := allActive(3, 1)
	if got := nextPlayablePos(1, 2, short); got != 2 {
		t.Errorf("short queue: got %d want 2", got)
	}
	if got := nextPlayablePos(2, 2, short); got != 4 {
		t.Errorf("short queue: got %d want 4", got)
	}
	if got := nextPlayablePos(4, 2, short); got != -1 {
		t.Errorf("short queue end: got %d want -1", got)
	}

	// played (off) track skipped without shifting neighbours: [t0, OFF, t2]
	off := [][]bool{{true, false, true}}
	if got := nextPlayablePos(0, 1, off); got != 2 {
		t.Errorf("skip off: got %d want 2", got)
	}

	if got := nextPlayablePos(-1, 2, [][]bool{{false}, {false}}); got != -1 {
		t.Errorf("nothing playable: got %d want -1", got)
	}
	if got := nextPlayablePos(-1, 0, nil); got != -1 {
		t.Errorf("no djs: got %d want -1", got)
	}
}

func TestFirstPlayablePos(t *testing.T) {
	if got := firstPlayablePos(2, allActive(2, 2)); got != 0 {
		t.Errorf("got %d want 0", got)
	}
	// dj0.t0 off, dj1.t0 active → first playable is pos 1
	if got := firstPlayablePos(2, [][]bool{{false, true}, {true}}); got != 1 {
		t.Errorf("got %d want 1", got)
	}
	if got := firstPlayablePos(0, nil); got != -1 {
		t.Errorf("empty stage: got %d want -1", got)
	}
	// played(off) at the top skipped — next is first active below.
	p := [][]bool{{false, false, true}, {false, true}}
	if got := firstPlayablePos(2, p); got != 3 {
		t.Errorf("scan-from-top: got %d want 3", got)
	}
	// running track masked → topmost OTHER active track.
	masked := [][]bool{{false, true}, {true, true}}
	if got := firstPlayablePos(2, masked); got != 1 {
		t.Errorf("masked running: got %d want 1", got)
	}
}

func TestReanchoredPos(t *testing.T) {
	q := func(ids []string) func(string) []string {
		return func(dj string) []string {
			if dj == "A" {
				return ids
			}
			return nil
		}
	}
	if got := reanchoredPos([]string{"A"}, "A", 2, "c", q([]string{"a", "b", "c", "d"})); got != 2 {
		t.Errorf("still aligned: got %d want 2", got)
	}
	if got := reanchoredPos([]string{"A"}, "A", 2, "c", q([]string{"c", "a", "b", "d"})); got != 0 {
		t.Errorf("reordered to top: got %d want 0", got)
	}
	// two DJs: pos = djIndex(0) + newIdx(3)*n(2) = 6
	qids := func(dj string) []string {
		if dj == "A" {
			return []string{"a", "b", "c", "x"}
		}
		return []string{"p", "q"}
	}
	if got := reanchoredPos([]string{"A", "B"}, "A", 0, "x", qids); got != 6 {
		t.Errorf("two-dj interleave: got %d want 6", got)
	}
	if got := reanchoredPos([]string{"A"}, "A", 2, "gone", q([]string{"a", "b", "c"})); got != 2 {
		t.Errorf("track removed: got %d want 2", got)
	}
	if got := reanchoredPos(nil, "A", 5, "x", q([]string{"x"})); got != 5 {
		t.Errorf("empty stage: got %d want 5", got)
	}
	if got := reanchoredPos([]string{"B"}, "A", 5, "x", q([]string{"x"})); got != 5 {
		t.Errorf("off-stage dj: got %d want 5", got)
	}
}

func TestFullWalk(t *testing.T) {
	p := allActive(2, 2)
	var visited [][2]int
	pos := firstPlayablePos(2, p)
	for pos >= 0 && len(visited) < 4 {
		di, ti := posToSlot(pos, 2)
		visited = append(visited, [2]int{di, ti})
		pos = nextPlayablePos(pos, 2, p)
	}
	want := [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}}
	if !reflect.DeepEqual(visited, want) {
		t.Errorf("walk: got %v want %v", visited, want)
	}
}

package main

import "testing"

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

// walk drives fairNext like the conductor does: pick a slot, mark it `off` (played), repeat —
// collecting the order of (djIndex, trackIndex) slots until nothing is playable.
func walk(djs []string, playable [][]bool, max int) [][2]int {
	var out [][2]int
	last := ""
	for len(out) < max {
		di, ti := fairNext(djs, last, playable)
		if di == -1 {
			break
		}
		out = append(out, [2]int{di, ti})
		playable[di][ti] = false // played → off, drops out (visible queue is the only truth)
		last = djs[di]
	}
	return out
}

func TestFairNextSingleDJ(t *testing.T) {
	djs := []string{"A"}
	// played(off) at the top skipped — next is first active below: [off, t1, t2]
	p := [][]bool{{false, true, true}}
	di, ti := fairNext(djs, "", p)
	if di != 0 || ti != 1 {
		t.Errorf("single-dj skip-off: got (%d,%d) want (0,1)", di, ti)
	}
	// full top-down walk for one DJ.
	got := walk(djs, allActive(3), 9)
	want := [][2]int{{0, 0}, {0, 1}, {0, 2}}
	if !equalSlots(got, want) {
		t.Errorf("single-dj walk: got %v want %v", got, want)
	}
}

func TestFairNextInterleave(t *testing.T) {
	djs := []string{"A", "B"}
	got := walk(djs, allActive(2, 2), 9)
	want := [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}}
	if !equalSlots(got, want) {
		t.Errorf("interleave: got %v want %v", got, want)
	}
}

// The fairness bug: DJ0's off tracks sit at the TOP, DJ1's at the bottom. The rotation must still
// alternate by each DJ's k-th PLAYABLE track, not by absolute queue index.
func TestFairNextSkewedOffPositions(t *testing.T) {
	djs := []string{"A", "B"}
	// A active at indices 5,6,7 (top 5 off); B active at indices 0,1,2 (bottom off).
	p := [][]bool{
		{false, false, false, false, false, true, true, true},
		{true, true, true, false, false, false, false, false},
	}
	got := walk(djs, p, 9)
	// Fair: A.idx5, B.idx0, A.idx6, B.idx1, A.idx7, B.idx2 — strict alternation.
	want := [][2]int{{0, 5}, {1, 0}, {0, 6}, {1, 1}, {0, 7}, {1, 2}}
	if !equalSlots(got, want) {
		t.Errorf("skewed off positions: got %v want %v", got, want)
	}
}

func TestFairNextRotatesAfterLastDJ(t *testing.T) {
	djs := []string{"A", "B", "C"}
	p := allActive(2, 2, 2)
	// Just played A → next is B (the one after A in the rotation).
	if di, _ := fairNext(djs, "A", p); di != 1 {
		t.Errorf("after A: got dj %d want 1 (B)", di)
	}
	// Just played C (last) → wraps to A.
	if di, _ := fairNext(djs, "C", p); di != 0 {
		t.Errorf("after C: got dj %d want 0 (A)", di)
	}
	// lastDJ not on stage → start at dj0.
	if di, _ := fairNext(djs, "Z", p); di != 0 {
		t.Errorf("unknown lastDJ: got dj %d want 0", di)
	}
}

func TestFairNextSkipsDryDJs(t *testing.T) {
	djs := []string{"A", "B", "C"}
	// B has nothing playable → rotation skips it: A, C, A, C, …
	p := [][]bool{{true, true}, {false}, {true, true}}
	got := walk(djs, p, 9)
	want := [][2]int{{0, 0}, {2, 0}, {0, 1}, {2, 1}}
	if !equalSlots(got, want) {
		t.Errorf("skip dry dj: got %v want %v", got, want)
	}
}

func TestFairNextNothingPlayable(t *testing.T) {
	if di, ti := fairNext([]string{"A", "B"}, "", [][]bool{{false}, {false}}); di != -1 || ti != -1 {
		t.Errorf("nothing playable: got (%d,%d) want (-1,-1)", di, ti)
	}
	if di, _ := fairNext(nil, "", nil); di != -1 {
		t.Errorf("no djs: got %d want -1", di)
	}
}

func equalSlots(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

package main

// Fair round-robin scheduling — picks the next track to play across the stage DJs.
//
// Each advance, the DJs take turns (round-robin) starting right AFTER the DJ who just played.
// Every DJ contributes its TOP playable track — the first ACTIVE (not `off`) track in its queue
// that isn't the currently-playing one. So the interleave alternates fairly across DJs by the
// k-th PLAYABLE track of each — never by absolute queue index — regardless of where each DJ's
// `off` (played/disabled) tracks happen to sit. The visible queue is the single source of truth:
// a played track becomes `off` and drops out (the DJ's "top" moves down), a reorder changes the
// top immediately, a re-activated track returns to the rotation. No hidden played-set. Mirrors
// frontend/src/lib/nostr/roundrobin.ts (fairSequence) — the relay is the authority, the TS copy
// drives the client's "Up next" preview only.

// fairNext picks the next (djIndex, trackIndex) to play: round-robin across djPks starting after
// lastDJ, each DJ offering its first playable track. playable[i][j] = true when DJ i's track j is
// ACTIVE and not the currently-playing one. Returns (-1, -1) when nothing is playable.
func fairNext(djPks []string, lastDJ string, playable [][]bool) (djIndex, trackIndex int) {
	n := len(djPks)
	if n == 0 {
		return -1, -1
	}
	start := indexOf(djPks, lastDJ) + 1 // -1 (not found / empty lastDJ) → start at dj0
	for off := 0; off < n; off++ {
		di := (start + off) % n
		if di >= len(playable) {
			continue
		}
		for ti, ok := range playable[di] {
			if ok {
				return di, ti
			}
		}
	}
	return -1, -1
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

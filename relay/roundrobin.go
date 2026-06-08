package main

// Pure round-robin position math — a faithful Go port of
// frontend/src/lib/nostr/roundrobin.ts (kept behaviourally identical; the TS copy stays for
// the client-side "Up next" preview only, the relay is the authority).
//
// Stage DJs in stable order [dj0..dj(n-1)], each with a queue. A running position `pos`:
//   djIndex    = pos mod n
//   trackIndex = pos div n
// → dj0.t0, dj1.t0, …, dj(n-1).t0, dj0.t1, …  (round-robin). Positions whose DJ has no
// (playable) track at `trackIndex` are skipped.

// posToSlot maps a running position to (djIndex, trackIndex).
func posToSlot(pos, djCount int) (djIndex, trackIndex int) {
	return pos % djCount, pos / djCount
}

// nextPlayablePos returns the next playable position after fromPos (exclusive), or -1.
// playable[i][j] = true when DJ i has an ACTIVE (not yet played) track at index j. Skips
// empty/exhausted queues AND played tracks (index unshifted → neighbours aren't shifted).
func nextPlayablePos(fromPos, djCount int, playable [][]bool) int {
	if djCount == 0 {
		return -1
	}
	maxLen := 0
	for _, a := range playable {
		if len(a) > maxLen {
			maxLen = len(a)
		}
	}
	maxPos := maxLen * djCount // upper bound: all tracks once through
	for pos := fromPos + 1; pos < maxPos; pos++ {
		di, ti := posToSlot(pos, djCount)
		if di < len(playable) && ti < len(playable[di]) && playable[di][ti] {
			return pos
		}
	}
	return -1
}

// firstPlayablePos returns the first playable position (start), or -1.
func firstPlayablePos(djCount int, playable [][]bool) int {
	return nextPlayablePos(-1, djCount, playable)
}

// reanchoredPos re-anchors pos to the index the currently-playing track now sits at in its
// DJ's queue (so a reorder takes effect on the NEXT track instead of being skipped). Returns
// the corrected pos, or the original if already aligned / stage empty / DJ off-stage / the
// track is gone from the queue. queueVideoIDs(dj) returns that DJ's track ids in order.
func reanchoredPos(djs []string, dj string, pos int, videoID string, queueVideoIDs func(string) []string) int {
	n := len(djs)
	djIndex := indexOf(djs, dj)
	if n == 0 || djIndex < 0 {
		return pos
	}
	_, trackIndex := posToSlot(pos, n)
	ids := queueVideoIDs(dj)
	if trackIndex < len(ids) && ids[trackIndex] == videoID {
		return pos // still aligned — nothing to do
	}
	newIdx := indexOf(ids, videoID)
	if newIdx < 0 {
		return pos // playing track gone from queue → let advance() handle it
	}
	return djIndex + newIdx*n
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// Pure round-robin logic (no Nostr/state) — easy to test.
//
// Stage DJs in stable order [dj0..dj(n-1)], each with a queue. The global flow is a
// running position `pos`:
//   djIndex    = pos mod n
//   trackIndex = floor(pos / n)
// → dj0.t0, dj1.t0, …, dj(n-1).t0, dj0.t1, …  (round-robin).
// Positions whose DJ has no track at `trackIndex` are skipped.

export interface Slot {
  djIndex: number
  trackIndex: number
}

export function posToSlot(pos: number, djCount: number): Slot {
  return { djIndex: pos % djCount, trackIndex: Math.floor(pos / djCount) }
}

/**
 * Next playable position after `fromPos` (exclusive), or -1 if none.
 * `playable[i][j]` = true when DJ i has an ACTIVE (not yet played) track at index j.
 * Skips empty/exhausted queues AND played tracks (index unshifted → no skipping of
 * neighbouring tracks).
 */
export function nextPlayablePos(
  fromPos: number,
  djCount: number,
  playable: boolean[][],
): number {
  if (djCount === 0) return -1
  const maxLen = playable.length ? Math.max(0, ...playable.map((a) => a.length)) : 0
  const maxPos = maxLen * djCount // upper bound: all tracks once through
  for (let pos = fromPos + 1; pos < maxPos; pos++) {
    const { djIndex, trackIndex } = posToSlot(pos, djCount)
    if (playable[djIndex]?.[trackIndex]) return pos
  }
  return -1
}

/** First playable position (start), or -1. */
export function firstPlayablePos(djCount: number, playable: boolean[][]): number {
  return nextPlayablePos(-1, djCount, playable)
}

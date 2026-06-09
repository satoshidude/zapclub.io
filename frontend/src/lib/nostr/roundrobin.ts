// Pure fair round-robin scheduling (no Nostr/state) — a mirror of relay/roundrobin.go (fairNext),
// used only for the client's read-only "Up next" preview. The relay is the authority.
//
// DJs take turns starting AFTER the currently-playing DJ; each contributes its TOP playable track
// (first ACTIVE, not the currently-playing one). So the interleave alternates fairly by the k-th
// PLAYABLE track of each DJ — never by absolute queue index — regardless of where each DJ's `off`
// tracks sit. The visible queue is the single source of truth: a played track is `off` and drops
// out, a reorder changes the top, a re-activated track returns. No hidden played-set.

export interface Slot {
  djIndex: number
  trackIndex: number
}

/**
 * The next `count` tracks the round-robin will play, as (djIndex, trackIndex) slots.
 * `playable[i][j]` = true when DJ i's track j is ACTIVE (not `off`) and not the currently-playing
 * one. `lastDjIndex` = index of the currently-playing DJ (or -1 to start from dj0). Looks ahead by
 * simulating consumption (a picked track won't be picked again), so each DJ contributes its next
 * active track each round — exactly the order the relay's repeated `advance` produces. Stops early
 * when nothing is left.
 */
export function fairSequence(
  djCount: number,
  playable: boolean[][],
  lastDjIndex: number,
  count: number,
): Slot[] {
  const out: Slot[] = []
  if (djCount === 0) return out
  const consumed = playable.map((row) => row.map(() => false))
  let last = lastDjIndex
  for (let k = 0; k < count; k++) {
    let picked = -1
    let pickedTi = -1
    const start = last + 1 // -1 (none playing) → start at dj0
    for (let o = 0; o < djCount; o++) {
      const di = (((start + o) % djCount) + djCount) % djCount
      const row = playable[di] ?? []
      for (let ti = 0; ti < row.length; ti++) {
        if (row[ti] && !consumed[di][ti]) {
          picked = di
          pickedTi = ti
          break
        }
      }
      if (picked >= 0) break
    }
    if (picked < 0) break
    consumed[picked][pickedTi] = true
    out.push({ djIndex: picked, trackIndex: pickedTi })
    last = picked
  }
  return out
}

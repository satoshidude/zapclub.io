import { describe, it, expect } from 'vitest'
import { posToSlot, nextPlayablePos, firstPlayablePos, reanchoredPos } from './roundrobin'

// playable[dj][track] = true when that DJ has an ACTIVE track at that index.
// Helper: all tracks active.
const allActive = (counts: number[]): boolean[][] => counts.map((n) => Array(n).fill(true))

describe('posToSlot', () => {
  it('maps pos to (djIndex, trackIndex) round-robin', () => {
    // 2 DJs: pos 0..5 → dj0.t0, dj1.t0, dj0.t1, dj1.t1, dj0.t2, dj1.t2
    expect(posToSlot(0, 2)).toEqual({ djIndex: 0, trackIndex: 0 })
    expect(posToSlot(1, 2)).toEqual({ djIndex: 1, trackIndex: 0 })
    expect(posToSlot(2, 2)).toEqual({ djIndex: 0, trackIndex: 1 })
    expect(posToSlot(3, 2)).toEqual({ djIndex: 1, trackIndex: 1 })
    expect(posToSlot(18, 2)).toEqual({ djIndex: 0, trackIndex: 9 }) // the diskbuster repro pos
  })

  it('single DJ: pos == trackIndex', () => {
    expect(posToSlot(7, 1)).toEqual({ djIndex: 0, trackIndex: 7 })
  })
})

describe('nextPlayablePos', () => {
  it('interleaves two DJs in order', () => {
    const p = allActive([3, 3])
    expect(nextPlayablePos(-1, 2, p)).toBe(0) // dj0.t0
    expect(nextPlayablePos(0, 2, p)).toBe(1) // dj1.t0
    expect(nextPlayablePos(1, 2, p)).toBe(2) // dj0.t1
  })

  it('skips a DJ whose queue is shorter (no track at that index)', () => {
    // dj0 has 3 tracks, dj1 has 1. After pos 1 (dj1.t0), dj1 has no t1 → skip to dj0.t1.
    const p = allActive([3, 1])
    expect(nextPlayablePos(1, 2, p)).toBe(2) // dj0.t1 (pos 3 = dj1.t1 doesn't exist)
    expect(nextPlayablePos(2, 2, p)).toBe(4) // dj0.t2 (pos 3 skipped)
    expect(nextPlayablePos(4, 2, p)).toBe(-1) // nothing left
  })

  it('skips played (inactive) tracks without shifting neighbours', () => {
    // single DJ, t1 played (off): [t0, OFF, t2]
    const p = [[true, false, true]]
    expect(nextPlayablePos(0, 1, p)).toBe(2) // skip the off t1
  })

  it('returns -1 when nothing is playable', () => {
    expect(nextPlayablePos(-1, 2, [[false], [false]])).toBe(-1)
    expect(nextPlayablePos(-1, 0, [])).toBe(-1)
  })
})

describe('firstPlayablePos', () => {
  it('finds the first active slot', () => {
    expect(firstPlayablePos(2, allActive([2, 2]))).toBe(0)
  })

  it('skips leading inactive slots', () => {
    // dj0.t0 off, dj1.t0 active → first playable is pos 1
    expect(firstPlayablePos(2, [[false, true], [true]])).toBe(1)
  })

  it('is -1 for an empty stage', () => {
    expect(firstPlayablePos(0, [])).toBe(-1)
  })
})

describe('reanchoredPos (reorder takes effect — the "refresh push" bug)', () => {
  // queueVideoIds for a single DJ "A".
  const q = (ids: string[]) => (dj: string) => (dj === 'A' ? ids : [])

  it('returns the same pos when the playing track is still at that index', () => {
    // single DJ, playing track "c" at index 2 (pos 2)
    expect(reanchoredPos(['A'], 'A', 2, 'c', q(['a', 'b', 'c', 'd']))).toBe(2)
  })

  it('re-anchors when the playing track moved to a new index', () => {
    // "c" was at index 2; reordered to index 0 → pos must become 0 so the NEXT advance
    // continues from the new order instead of skipping ahead.
    expect(reanchoredPos(['A'], 'A', 2, 'c', q(['c', 'a', 'b', 'd']))).toBe(0)
  })

  it('two DJs: pos accounts for the interleave (djIndex + newIdx*n)', () => {
    // DJs [A, B], playing A's "x". A reordered so "x" is now at index 3.
    // pos = djIndex(0) + newIdx(3)*n(2) = 6
    const qids = (dj: string) => (dj === 'A' ? ['a', 'b', 'c', 'x'] : ['p', 'q'])
    expect(reanchoredPos(['A', 'B'], 'A', 0, 'x', qids)).toBe(6)
  })

  it('leaves pos untouched when the playing track was removed from the queue', () => {
    expect(reanchoredPos(['A'], 'A', 2, 'gone', q(['a', 'b', 'c']))).toBe(2)
  })

  it('leaves pos untouched for an empty stage or an off-stage DJ', () => {
    expect(reanchoredPos([], 'A', 5, 'x', q(['x']))).toBe(5)
    expect(reanchoredPos(['B'], 'A', 5, 'x', q(['x']))).toBe(5)
  })
})

describe('scan from the top (regression: active tracks stranded above a drifted play head)', () => {
  // The live repro: the play head drifted forward (pos high) while ACTIVE tracks remained at
  // the top of each playlist. advance() now scans from the TOP (firstPlayablePos), not forward
  // from the drifted pos (nextPlayablePos) — so the topmost unplayed track is always next.

  it('the first unplayed (active) track from the top is next', () => {
    // dj0: t0 active. dj1: t0 active. → dj0.t0 (pos 0) is next.
    expect(firstPlayablePos(2, [[true, true], [true, true]])).toBe(0)
  })

  it('played (off) tracks at the top are skipped — next is the first active below', () => {
    // dj0: t0,t1 played(off), t2 active. dj1: t0 played(off), t1 active.
    // Scanning pos 0,1,2,…: dj0.t0(off), dj1.t0(off), dj0.t1(off), dj1.t1(active) → pos 3.
    const p = [
      [false, false, true],
      [false, true],
    ]
    expect(firstPlayablePos(2, p)).toBe(3) // dj1.t1 — first active scanning from the top
    expect(posToSlot(3, 2)).toEqual({ djIndex: 1, trackIndex: 1 })
  })

  it('a drifted forward scan would MISS the active top tracks (why we switched)', () => {
    // 2 DJs, 8 tracks each, all active. From a drifted pos 11 the forward scan returns a deep
    // slot — never the top tracks. firstPlayablePos returns the very top instead.
    const p = allActive([8, 8])
    const fwd = nextPlayablePos(11, 2, p)
    expect(posToSlot(fwd, 2).trackIndex).toBeGreaterThan(0) // forward → deep in the lists
    expect(firstPlayablePos(2, p)).toBe(0) // scan-from-top → dj0.t0
  })

  it('with the running track excluded, the next pick is the topmost OTHER active track', () => {
    // advance() masks the running track. Running = dj0.t0 → next is dj1.t0 (pos 1).
    const masked = [[false /* running, masked */, true], [true, true]]
    expect(firstPlayablePos(2, masked)).toBe(1)
  })
})

describe('round-robin full walk (regression: every DJ gets played)', () => {
  it('a 2-DJ rotation visits both DJs alternately', () => {
    const p = allActive([2, 2])
    const visited = []
    let pos = firstPlayablePos(2, p)
    while (pos >= 0 && visited.length < 4) {
      visited.push(posToSlot(pos, 2))
      pos = nextPlayablePos(pos, 2, p)
    }
    expect(visited).toEqual([
      { djIndex: 0, trackIndex: 0 },
      { djIndex: 1, trackIndex: 0 },
      { djIndex: 0, trackIndex: 1 },
      { djIndex: 1, trackIndex: 1 },
    ])
  })
})

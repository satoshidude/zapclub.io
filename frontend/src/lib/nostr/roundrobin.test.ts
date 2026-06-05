import { describe, it, expect } from 'vitest'
import { posToSlot, nextPlayablePos, firstPlayablePos } from './roundrobin'

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

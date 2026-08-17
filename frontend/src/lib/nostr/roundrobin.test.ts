import { describe, it, expect } from 'vitest'
import { fairSequence, type Slot } from './roundrobin'

// allActive builds a playable matrix with every track active.
function allActive(...counts: number[]): boolean[][] {
  return counts.map((n) => Array.from({ length: n }, () => true))
}

const flat = (s: Slot[]) => s.map((x) => [x.djIndex, x.trackIndex] as [number, number])

describe('fairSequence', () => {
  it('single DJ plays top-down, skipping off tracks', () => {
    // [off, t1, t2] → t1, t2
    expect(flat(fairSequence(1, [[false, true, true]], -1, 9))).toEqual([
      [0, 1],
      [0, 2],
    ])
  })

  it('two DJs interleave round-robin', () => {
    expect(flat(fairSequence(2, allActive(2, 2), -1, 9))).toEqual([
      [0, 0],
      [1, 0],
      [0, 1],
      [1, 1],
    ])
  })

  // The fairness bug: DJ0's off tracks sit at the TOP, DJ1's at the bottom. The preview must still
  // alternate by each DJ's k-th PLAYABLE track, not by absolute queue index.
  it('alternates by k-th playable track regardless of where off tracks sit', () => {
    const p = [
      [false, false, false, false, false, true, true, true], // A active at 5,6,7
      [true, true, true, false, false, false, false, false], // B active at 0,1,2
    ]
    expect(flat(fairSequence(2, p, -1, 9))).toEqual([
      [0, 5],
      [1, 0],
      [0, 6],
      [1, 1],
      [0, 7],
      [1, 2],
    ])
  })

  it('starts the rotation AFTER the currently-playing DJ', () => {
    // 3 DJs, A is live (index 0) → next is B, then C, then back to A.
    const slots = flat(fairSequence(3, allActive(1, 1, 1), 0, 9))
    expect(slots[0][0]).toBe(1) // B
    expect(slots[1][0]).toBe(2) // C
    expect(slots[2][0]).toBe(0) // A wraps
  })

  it('skips DJs with nothing playable', () => {
    // B (index 1) is dry → A, C, A, C
    const p = [[true, true], [false], [true, true]]
    expect(flat(fairSequence(3, p, -1, 9))).toEqual([
      [0, 0],
      [2, 0],
      [0, 1],
      [2, 1],
    ])
  })

  it('stops when nothing is playable', () => {
    expect(fairSequence(2, [[false], [false]], -1, 9)).toEqual([])
    expect(fairSequence(0, [], -1, 9)).toEqual([])
  })

  it('honours the count cap', () => {
    expect(fairSequence(2, allActive(5, 5), -1, 3)).toHaveLength(3)
  })
})

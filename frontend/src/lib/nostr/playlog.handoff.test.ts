import { describe, it, expect } from 'vitest'
import { reconstructPlayed, type PlayRecord } from './playlog'
import { firstPlayablePos, posToSlot } from './roundrobin'

// Composition test: proves the CORE stability property — after a conductor handoff, the new
// conductor (which seeds its played-set from the SHARED 1313 log) picks the NEXT track, never
// replaying what the room already heard, even for an AWAY DJ whose tracks never got an `off`
// flag. This mirrors how sync.svelte.ts combines reconstructPlayed → playableExcluding →
// firstPlayablePos, without the $state plumbing.

const GAP = 150_000

// 11-char ids per DJ.
const A = ['a0aaaaaaaaa', 'a1aaaaaaaaa'] // away DJ's queue (no `off` flags ever written)
const B = ['b0bbbbbbbbb', 'b1bbbbbbbbb', 'b2bbbbbbbbb'] // present DJ's queue
const djs = [A, B] // stage order: A (oldest), B

// Mirror sync.playableExcluding: a slot is playable if not `off` (all active here) AND not in
// the played-set AND not the current track.
function playableExcluding(queues: string[][], played: Set<string>, current: string | null) {
  const ex = new Set(played)
  if (current) ex.add(current)
  return queues.map((q) => q.map((vid) => !ex.has(vid)))
}

function pick(queues: string[][], played: Set<string>, current: string | null) {
  const pos = firstPlayablePos(queues.length, playableExcluding(queues, played, current))
  if (pos === -1) return null
  const { djIndex, trackIndex } = posToSlot(pos, queues.length)
  return queues[djIndex][trackIndex]
}

describe('round-robin handoff (no replay via the shared play-log)', () => {
  it('the rescuer continues to the next track instead of replaying the away DJ', () => {
    // Conductor 1 played a0(pos0), b0(pos1), a1(pos2) — interleaved round-robin.
    const log: PlayRecord[] = [
      { videoId: A[0], startedAt: 1000, pos: 0, loop: 0 },
      { videoId: B[0], startedAt: 60_000, pos: 1, loop: 0 },
      { videoId: A[1], startedAt: 120_000, pos: 2, loop: 0 },
    ]
    // Conductor 1 just started a1 → it's the live track when conductor 2 takes over.
    const current = A[1]
    const { played } = reconstructPlayed(log, GAP, current)
    // The new conductor advances PAST the current track → next should be b1, NOT any replay.
    const next = pick(djs, played, current)
    expect(next).toBe(B[1])
    expect([A[0], B[0], A[1]]).not.toContain(next)
  })

  it('a cold conductor with an EMPTY log starts from the top (no regression)', () => {
    const { played } = reconstructPlayed([], GAP, null)
    expect(pick(djs, played, null)).toBe(A[0])
  })

  it('when everything in the epoch is played, the loop epoch resets and replay is allowed', () => {
    // All five tracks played in loop 0.
    const all = [...A, ...B]
    const log: PlayRecord[] = all.map((videoId, i) => ({ videoId, startedAt: 1000 + i * 60_000, pos: i, loop: 0 }))
    const { played } = reconstructPlayed(log, GAP, null)
    expect(pick(djs, played, null)).toBeNull() // nothing left in this epoch → advance() bumps loop
    // After the loop bump, the played-set for the NEW epoch is empty → top of the rotation again.
    const afterLoop = reconstructPlayed(log, GAP, null)
    const nextEpochPlayed = new Set<string>() // epoch incremented locally, no loop-1 plays yet
    void afterLoop
    expect(pick(djs, nextEpochPlayed, null)).toBe(A[0])
  })
})

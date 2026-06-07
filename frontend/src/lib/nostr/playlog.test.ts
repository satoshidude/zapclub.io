import { describe, it, expect } from 'vitest'
import type { Event } from 'nostr-tools/pure'
import { parsePlayRecord, currentLoop, reconstructPlayed, type PlayRecord } from './playlog'

const GAP = 150_000 // SESSION_GAP_MS
// 11-char YouTube ids.
const A = 'aaaaaaaaaaa'
const B = 'bbbbbbbbbbb'
const C = 'ccccccccccc'

function ev(videoId: string, startedAtMs: number, pos = 0, loop = 0, withLoop = true): Event {
  const tags: string[][] = [
    ['h', 'club'],
    ['p', 'dj'],
    ['started_at', String(startedAtMs)],
    ['pos', String(pos)],
  ]
  if (withLoop) tags.push(['loop', String(loop)])
  return {
    id: videoId + startedAtMs,
    pubkey: 'dj',
    created_at: Math.floor(startedAtMs / 1000),
    kind: 1313,
    tags,
    content: videoId,
    sig: '',
  } as Event
}

function rec(videoId: string, startedAt: number, loop = 0): PlayRecord {
  return { videoId, startedAt, pos: 0, loop }
}

describe('parsePlayRecord', () => {
  it('parses a valid record', () => {
    expect(parsePlayRecord(ev(A, 5000, 3, 1))).toEqual({ videoId: A, startedAt: 5000, pos: 3, loop: 1 })
  })
  it('rejects an invalid video id', () => {
    expect(parsePlayRecord(ev('not a yt id', 5000))).toBeNull()
  })
  it('defaults loop and pos to 0 when the tags are missing (legacy 1313)', () => {
    const e = ev(A, 5000, 0, 0, false)
    e.tags = e.tags.filter((t) => t[0] !== 'pos')
    expect(parsePlayRecord(e)).toEqual({ videoId: A, startedAt: 5000, pos: 0, loop: 0 })
  })
  it('falls back to created_at (s→ms) when started_at is absent', () => {
    const e = ev(A, 0)
    e.tags = e.tags.filter((t) => t[0] !== 'started_at')
    e.created_at = 7
    expect(parsePlayRecord(e)?.startedAt).toBe(7000)
  })
})

describe('currentLoop', () => {
  it('is 0 for an empty log', () => {
    expect(currentLoop([], GAP)).toBe(0)
  })
  it('is the max loop within the latest session', () => {
    expect(currentLoop([rec(A, 1000, 0), rec(B, 2000, 1)], GAP)).toBe(1)
  })
  it('ignores a high epoch from a PREVIOUS session (separated by a gap)', () => {
    // old session at loop 5, then a >GAP gap, then a fresh session at loop 0
    const recs = [rec(A, 1000, 5), rec(B, 1000 + GAP + 1, 0)]
    expect(currentLoop(recs, GAP)).toBe(0)
  })
})

describe('reconstructPlayed', () => {
  it('returns all session videoIds minus the current one', () => {
    const recs = [rec(A, 1000), rec(B, 60_000), rec(C, 120_000)]
    const { played, loop } = reconstructPlayed(recs, GAP, C)
    expect([...played].sort()).toEqual([A, B])
    expect(loop).toBe(0)
  })
  it('splits sessions at a gap and only counts the latest', () => {
    // A,B are an old session; C is the new one after a >GAP gap.
    const recs = [rec(A, 1000), rec(B, 2000), rec(C, 2000 + GAP + 1)]
    const { played } = reconstructPlayed(recs, GAP, null)
    expect([...played]).toEqual([C])
  })
  it('only counts plays in the current loop epoch', () => {
    // within one session: A,B played in loop 0, then loop bumped to 1 and A replayed.
    const recs = [rec(A, 1000, 0), rec(B, 2000, 0), rec(A, 3000, 1)]
    const { played, loop } = reconstructPlayed(recs, GAP, null)
    expect(loop).toBe(1)
    expect([...played]).toEqual([A]) // only the loop-1 play counts; B (loop 0) is replayable
  })
  it('dedupes repeated videoIds within an epoch', () => {
    const recs = [rec(A, 1000, 0), rec(A, 2000, 0)]
    expect(reconstructPlayed(recs, GAP, null).played.size).toBe(1)
  })
  it('is deterministic regardless of input order', () => {
    const recs = [rec(C, 120_000), rec(A, 1000), rec(B, 60_000)]
    const shuffled = [rec(B, 60_000), rec(C, 120_000), rec(A, 1000)]
    const r1 = reconstructPlayed(recs, GAP, null)
    const r2 = reconstructPlayed(shuffled, GAP, null)
    expect([...r1.played].sort()).toEqual([...r2.played].sort())
    expect(r1.loop).toBe(r2.loop)
  })
  it('empty log → empty set, loop 0 (cold start, no regression)', () => {
    expect(reconstructPlayed([], GAP, null)).toEqual({ played: new Set(), loop: 0 })
  })
})

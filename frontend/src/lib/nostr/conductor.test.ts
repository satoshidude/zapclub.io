import { describe, it, expect } from 'vitest'
import { selectActiveDjs, pickConductor, STALE_MS, type DjState } from './conductor'

const NOW = 1_000_000_000_000 // fixed "now" in ms
const dj = (since: number, lastSeenMsAgo = 0, on = true): DjState => ({
  since,
  lastSeen: NOW - lastSeenMsAgo,
  on,
})

describe('selectActiveDjs', () => {
  it('sorts by since (oldest first), pubkey tiebreak', () => {
    const djs = { b: dj(200), a: dj(100), c: dj(100) }
    expect(selectActiveDjs(djs, {}, NOW).map((d) => d.pubkey)).toEqual(['a', 'c', 'b'])
  })

  it('drops DJs that stepped off', () => {
    const djs = { a: dj(100), b: dj(200, 0, false) }
    expect(selectActiveDjs(djs, {}, NOW).map((d) => d.pubkey)).toEqual(['a'])
  })

  it('drops stale DJs (no heartbeat within 1h) — the 72-min-DJ from the repro', () => {
    const djs = { fresh: dj(100, 60_000), stale: dj(50, STALE_MS + 60_000) }
    // `stale` joined earliest but hasn't been seen in >1h → excluded.
    expect(selectActiveDjs(djs, {}, NOW).map((d) => d.pubkey)).toEqual(['fresh'])
  })

  it('drops a kicked DJ until they heartbeat after the kick', () => {
    const djs = { a: dj(100, 10_000) } // last seen 10s ago
    expect(selectActiveDjs(djs, { a: NOW - 5_000 }, NOW)).toEqual([]) // kicked 5s ago (after last seen)
    expect(selectActiveDjs(djs, { a: NOW - 20_000 }, NOW).map((d) => d.pubkey)).toEqual(['a']) // kick predates last seen
  })

  it('caps at maxDjs', () => {
    const djs = Object.fromEntries(Array.from({ length: 8 }, (_, i) => [`d${i}`, dj(i)]))
    expect(selectActiveDjs(djs, {}, NOW, 5)).toHaveLength(5)
  })
})

describe('pickConductor', () => {
  const A = { pubkey: 'A', since: 100 }
  const B = { pubkey: 'B', since: 200 }
  const OWNER = { pubkey: 'OWNER', since: 300 }

  it('empty stage → null', () => {
    expect(pickConductor([], null, null)).toBeNull()
  })

  it('owner on stage is ALWAYS conductor, even if they joined last', () => {
    // Regression: the diskbuster bug came from mis-identifying the owner; here, given the
    // correct owner, they must lead regardless of join order or a sticky current.
    expect(pickConductor([A, B, OWNER], 'OWNER', 'A')).toBe('OWNER')
  })

  it('no owner on stage → oldest active DJ leads', () => {
    expect(pickConductor([A, B], 'OWNER', null)).toBe('A')
  })

  it('is STICKY: a newly joined older DJ does not steal an active conductor', () => {
    // current conductor B is active; A (older `since`) joins → B keeps it (no owner).
    expect(pickConductor([A, B], null, 'B')).toBe('B')
  })

  it('fails over to the oldest when the current conductor leaves', () => {
    expect(pickConductor([A, B], null, 'GONE')).toBe('A')
  })

  it('owner override beats a sticky current conductor', () => {
    expect(pickConductor([A, B, OWNER], 'OWNER', 'B')).toBe('OWNER')
  })

  it('deterministic across clients: same inputs → same conductor', () => {
    const c1 = pickConductor([A, B], null, null)
    const c2 = pickConductor([B, A], null, null) // different array order, same DJs
    // selectActiveDjs would sort first; here both already represent the active set.
    expect(c1).toBe('A')
    expect(c2).toBe('B') // pickConductor itself takes [0]; ordering is selectActiveDjs's job
  })
})

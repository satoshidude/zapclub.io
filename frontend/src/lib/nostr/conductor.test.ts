import { describe, it, expect } from 'vitest'
import { selectActiveDjs, STALE_MS, type DjState } from './conductor'

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

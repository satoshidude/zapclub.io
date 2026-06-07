import { describe, it, expect } from 'vitest'
import { selectActiveDjs, pickConductor, shouldConduct, STALE_MS, type DjState } from './conductor'

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

describe('shouldConduct (phantom-conductor rescue)', () => {
  const RESCUE = 90_000
  // DJs oldest-since first; owner is the elected conductor (owner-override).
  const djs = ['owner', 'bob', 'carol']

  it('the elected conductor always acts (present → reclaim / owner-override)', () => {
    // even while a rescuer is currently writing, the elected conductor reclaims when present
    expect(shouldConduct('owner', 'owner', djs, 'bob', 0, RESCUE)).toBe(true)
    expect(shouldConduct('owner', 'owner', djs, 'owner', 200_000, RESCUE)).toBe(true)
  })

  it('a non-elected DJ defers while now_playing is fresh', () => {
    expect(shouldConduct('bob', 'owner', djs, 'owner', 5_000, RESCUE)).toBe(false)
    expect(shouldConduct('carol', 'owner', djs, 'owner', 5_000, RESCUE)).toBe(false)
  })

  it('a non-elected DJ defers when there is no track yet', () => {
    expect(shouldConduct('bob', 'owner', djs, null, Infinity, RESCUE)).toBe(false)
  })

  it('bootstraps when there is no track AND the elected conductor is offline', () => {
    // owner is elected but offline (dead/closed client, or empty queue + navigated away while
    // sticky on stage) and there is NO now_playing. Everyone deferring to the absent owner
    // would leave the room silent even though bob/carol have full queues → the oldest ONLINE
    // DJ (bob) must bootstrap playback instead.
    const online = (pk: string) => pk !== 'owner'
    expect(shouldConduct('bob', 'owner', djs, null, Infinity, RESCUE, online)).toBe(true)
    expect(shouldConduct('carol', 'owner', djs, null, Infinity, RESCUE, online)).toBe(false)
  })

  it('still defers bootstrap to the elected conductor while it is online', () => {
    const online = () => true
    expect(shouldConduct('bob', 'owner', djs, null, Infinity, RESCUE, online)).toBe(false)
    expect(shouldConduct('owner', 'owner', djs, null, Infinity, RESCUE, online)).toBe(true)
  })

  it('bootstrap cascades past multiple offline DJs to the first online one', () => {
    const online = (pk: string) => pk === 'carol' // owner + bob offline, only carol here
    expect(shouldConduct('carol', 'owner', djs, null, Infinity, RESCUE, online)).toBe(true)
    expect(shouldConduct('bob', 'owner', djs, null, Infinity, RESCUE, online)).toBe(false)
  })

  it('the oldest non-writer DJ rescues a silent conductor', () => {
    // owner (elected) went silent → bob (oldest active DJ that is not the silent writer) takes over
    expect(shouldConduct('bob', 'owner', djs, 'owner', 120_000, RESCUE)).toBe(true)
    expect(shouldConduct('carol', 'owner', djs, 'owner', 120_000, RESCUE)).toBe(false)
  })

  it('only one DJ rescues — deterministic across clients', () => {
    // The elected `owner` is the silent/absent one being rescued (it would only return true
    // if present, which it isn't). Among the OTHER DJs exactly one rescues.
    const rescuers = djs
      .filter((me) => me !== 'owner')
      .filter((me) => shouldConduct(me, 'owner', djs, 'owner', 120_000, RESCUE))
    expect(rescuers).toEqual(['bob'])
  })

  it('the rescuer sticks (no flapping) while it keeps the heartbeat fresh', () => {
    // bob took over; now bob is the fresh writer → bob keeps going, others defer
    expect(shouldConduct('bob', 'owner', djs, 'bob', 5_000, RESCUE)).toBe(true)
    expect(shouldConduct('carol', 'owner', djs, 'bob', 5_000, RESCUE)).toBe(false)
    // owner is elected but absent → it doesn't run this at all; if it returns, it reclaims
    expect(shouldConduct('owner', 'owner', djs, 'bob', 5_000, RESCUE)).toBe(true)
  })

  it('hands off to the next DJ if the rescuer itself goes silent', () => {
    // bob rescued, then bob also went silent → carol (next non-writer) rescues
    expect(shouldConduct('carol', 'owner', djs, 'bob', 120_000, RESCUE)).toBe(true)
    expect(shouldConduct('owner', 'owner', djs, 'bob', 120_000, RESCUE)).toBe(true) // owner still reclaims if present
  })

  it('no one acts without a local pubkey', () => {
    expect(shouldConduct(null, 'owner', djs, 'owner', 120_000, RESCUE)).toBe(false)
  })

  it('rescue cascades to whoever is ONLINE (skips phantom designated rescuer)', () => {
    // owner (elected+writer) and bob are both phantoms (on stage but away); only carol is here.
    // Without presence the rescuer would be bob (oldest non-writer) — and stay stuck. With
    // presence, carol (the only online non-writer) takes over.
    const online = (pk: string) => pk === 'carol'
    expect(shouldConduct('carol', 'owner', djs, 'owner', 120_000, RESCUE, online)).toBe(true)
    expect(shouldConduct('bob', 'owner', djs, 'owner', 120_000, RESCUE, online)).toBe(false)
  })

  it('online rescuer is oldest-since first among present DJs', () => {
    const online = () => true // everyone present → oldest non-writer (bob) rescues
    expect(shouldConduct('bob', 'owner', djs, 'owner', 120_000, RESCUE, online)).toBe(true)
    expect(shouldConduct('carol', 'owner', djs, 'owner', 120_000, RESCUE, online)).toBe(false)
  })
})

// @vitest-environment happy-dom
// (queue.svelte.ts → groups.ts → nostrLogin.ts → router reads location.pathname at import)
import { describe, it, expect } from 'vitest'
import { ingestQueue, queues } from './queue.svelte'
import type { Event } from 'nostr-tools/pure'

// A valid 11-char YouTube id (parseTracks drops anything else).
const VID = (n: number) => `dQw4w9WgX0${n}`

function queueEvent(pubkey: string, createdAt: number, ids: number[]): Event {
  return {
    kind: 30103,
    pubkey,
    created_at: createdAt,
    tags: ids.map((n) => ['track', `yt:${VID(n)}`, `Track ${n}`, '200']),
    content: '',
    id: 'x',
    sig: 'x',
  } as Event
}

// The periodic re-sync (refreshQueues) re-ingests whatever the relay currently holds. That is
// only safe if ingestQueue never regresses a DJ's state to an older/equal snapshot — these
// pin that invariant so the round-robin can't be corrupted by a stale poll result.
describe('ingestQueue (re-sync safety: newest created_at wins)', () => {
  it('ingests a fresh queue', () => {
    ingestQueue(queueEvent('alice', 100, [1, 2]))
    expect(queues.get('alice')?.tracks.map((t) => t.videoId)).toEqual([VID(1), VID(2)])
    expect(queues.get('alice')?.updatedAt).toBe(100)
  })

  it('ignores an OLDER event (a stale poll result must not regress state)', () => {
    ingestQueue(queueEvent('bob', 200, [1, 2, 3]))
    ingestQueue(queueEvent('bob', 150, [9])) // older → dropped
    expect(queues.get('bob')?.tracks.map((t) => t.videoId)).toEqual([VID(1), VID(2), VID(3)])
    expect(queues.get('bob')?.updatedAt).toBe(200)
  })

  it('re-ingesting the SAME created_at is idempotent (poll returns the live event again)', () => {
    ingestQueue(queueEvent('carol', 300, [1, 2]))
    ingestQueue(queueEvent('carol', 300, [1, 2]))
    expect(queues.get('carol')?.tracks.map((t) => t.videoId)).toEqual([VID(1), VID(2)])
    expect(queues.get('carol')?.updatedAt).toBe(300)
  })

  it('applies a NEWER event (a real edit picked up by the poll)', () => {
    ingestQueue(queueEvent('dave', 400, [1, 2, 3]))
    ingestQueue(queueEvent('dave', 500, [2])) // newer → replaces
    expect(queues.get('dave')?.tracks.map((t) => t.videoId)).toEqual([VID(2)])
    expect(queues.get('dave')?.updatedAt).toBe(500)
  })
})

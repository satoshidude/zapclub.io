// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Event, EventTemplate } from 'nostr-tools/pure'
import {
  recordLogicalSignRequest,
  recordPhysicalSignEventCall,
  resetSigningDiagnostics,
  setSigningDiagnosticsEnabled,
  signingDiagnosticsSnapshot,
} from './signingDiagnostics'

const ME = 'a'.repeat(64)
const mocked = vi.hoisted(() => ({
  auth: { pubkey: 'a'.repeat(64) },
  publishClub: vi.fn<(template: EventTemplate) => Promise<Event>>(),
}))

vi.mock('./auth.svelte', () => ({ auth: mocked.auth }))
vi.mock('./pool', () => ({ CLUB_RELAY_PUBKEY: 'b'.repeat(64) }))
vi.mock('./groups', () => ({
  KIND_QUEUE: 30103,
  fetchClubQueues: vi.fn().mockResolvedValue([]),
  publishClub: mocked.publishClub,
}))

import {
  addTrack,
  enrichMyTrackDuration,
  enrichMyTrackTitle,
  ingestQueue,
  resetQueues,
} from './queue.svelte'

function queueEvent(): Event {
  return {
    id: 'event-id',
    sig: 'event-signature',
    pubkey: ME,
    kind: 30103,
    created_at: 1,
    tags: [
      ['h', 'private-club-id'],
      ['track', 'yt:ABCDEFGHIJK', 'Bare title', '0'],
    ],
    content: '',
  }
}

beforeEach(() => {
  resetQueues()
  resetSigningDiagnostics()
  setSigningDiagnosticsEnabled(true)
  mocked.publishClub.mockReset()
  mocked.publishClub.mockImplementation(async (template) => {
    // publishClub eventually reaches these two central nostrLogin hooks. Calling them here keeps
    // this test focused on whether the queue attached the correct non-sensitive trigger context.
    recordLogicalSignRequest(template, 'nostrLogin')
    recordPhysicalSignEventCall(template, 'nostrLogin')
    return { ...queueEvent(), ...template }
  })
})

afterEach(() => {
  setSigningDiagnosticsEnabled(false)
  resetSigningDiagnostics()
})

describe('queue signing diagnostics', () => {
  it('labels durable queue writes as user actions while player metadata stays local', async () => {
    ingestQueue(queueEvent())

    await enrichMyTrackTitle('private-club-id', 'ABCDEFGHIJK', 'Artist - Bare title')
    await enrichMyTrackDuration('private-club-id', 'ABCDEFGHIJK', 180)
    expect(mocked.publishClub).not.toHaveBeenCalled()

    await addTrack('private-club-id', {
      videoId: 'LMNOPQRSTUV',
      title: 'Explicit addition',
      duration: 200,
    })

    expect(mocked.publishClub).toHaveBeenCalledTimes(1)
    expect(signingDiagnosticsSnapshot().logicalSignRequests).toEqual([
      { kind: 30103, trigger: 'queue-user-action', count: 1 },
    ])
    expect(signingDiagnosticsSnapshot().physicalSignEventCalls).toEqual([
      { kind: 30103, trigger: 'queue-user-action', count: 1 },
    ])
  })
})

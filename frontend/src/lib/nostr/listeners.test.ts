// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { generateSecretKey, getPublicKey, verifyEvent, type Event } from 'nostr-tools/pure'
import { CLUB_RELAY_PUBKEY } from './pool'
import { KIND_LISTENER_COUNT } from './groups'
import { createListenerBeat, parseListenerCount } from './listeners.svelte'

function countEvent(overrides: Partial<Event> = {}): Event {
  return {
    id: 'count',
    sig: 'sig',
    kind: KIND_LISTENER_COUNT,
    pubkey: CLUB_RELAY_PUBKEY,
    created_at: 1,
    content: '',
    tags: [['h', 'club'], ['count', '3'], ['sent_at', '1700000000000']],
    ...overrides,
  } as Event
}

describe('listener count', () => {
  it('creates a valid anonymous heartbeat without a login identity', () => {
    const key = generateSecretKey()
    const event = createListenerBeat('club', 'on', key)
    expect(verifyEvent(event)).toBe(true)
    expect(event.pubkey).toBe(getPublicKey(key))
    expect(event.tags).toEqual([['h', 'club'], ['state', 'on']])
    expect(event.content).toBe('')
  })

  it('accepts a relay-signed aggregate for the requested club', () => {
    expect(parseListenerCount(countEvent(), 'club')).toEqual({ count: 3, sentAt: 1_700_000_000_000 })
  })

  it('rejects forged, cross-club and malformed counts', () => {
    expect(parseListenerCount(countEvent({ pubkey: 'f'.repeat(64) }), 'club')).toBeNull()
    expect(parseListenerCount(countEvent(), 'elsewhere')).toBeNull()
    expect(parseListenerCount(countEvent({ tags: [['h', 'club'], ['count', '-1'], ['sent_at', '1']] }), 'club')).toBeNull()
  })
})

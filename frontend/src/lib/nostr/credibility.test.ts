import { describe, expect, it } from 'vitest'
import type { Event } from 'nostr-tools/pure'
import { CLUB_RELAY_PUBKEY } from './pool'
import { CREDIBILITY_NAMESPACE, KIND_CREDIBILITY, parseCredibility } from './credibility'

function event(overrides: Partial<Event> = {}): Event {
  return {
    id: 'id',
    pubkey: CLUB_RELAY_PUBKEY,
    created_at: 1,
    kind: KIND_CREDIBILITY,
    tags: [
      ['h', CREDIBILITY_NAMESPACE],
      ['d', 'zapclub:credibility:alice'],
      ['p', 'alice'],
      ['score', '-2'],
      ['tracks', '9'],
      ['bangers', '7'],
      ['skipped', '3'],
    ],
    content: '',
    sig: '',
    ...overrides,
  } as Event
}

describe('parseCredibility', () => {
  it('accepts a relay-attested score including negative credibility', () => {
    expect(parseCredibility(event(), 'alice')).toEqual({ score: -2, tracks: 9, bangers: 7, skipped: 3 })
  })

  it('rejects self-reported or mismatched profiles', () => {
    expect(parseCredibility(event({ pubkey: 'mallory' }), 'alice')).toBeNull()
    expect(parseCredibility(event(), 'bob')).toBeNull()
  })

  it('rejects malformed counters', () => {
    const malformed = event()
    malformed.tags = malformed.tags.map((tag) => tag[0] === 'tracks' ? ['tracks', '-1'] : tag)
    expect(parseCredibility(malformed, 'alice')).toBeNull()
  })
})

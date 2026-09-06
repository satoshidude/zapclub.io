// @vitest-environment happy-dom
// (groups.ts → nostrLogin.ts → router reads location.pathname at import time)
import { describe, it, expect } from 'vitest'
import { KIND_MEMBER_COUNT, parseOwner, parseAdmins, selectClubMemberCounts, selectOnAirClubDjs, selectOnAirClubTracks, selectOnStageClubDjs } from './groups'
import type { Event } from 'nostr-tools/pure'
import { CLUB_RELAY_PUBKEY } from './pool'
import { SESSION_EVENT_MARKER } from './sessionSigner'

// Minimal 39001 admins event with given [pubkey, role] p-tags (in tag order).
function adminsEvent(tags: Array<[string, string]>): Event {
  return {
    kind: 39001,
    tags: tags.map(([pk, role]) => ['p', pk, role]),
    content: '',
    created_at: 0,
    pubkey: 'relay',
    id: 'x',
    sig: 'x',
  } as Event
}

describe('parseOwner (regression: owner by role, not tag position)', () => {
  it('picks the owner even when a moderator is listed FIRST', () => {
    // The diskbuster repro: moderator first, owner second.
    const ev = adminsEvent([
      ['7bea8ec2', 'moderator'],
      ['661419f8', 'owner'],
    ])
    expect(parseOwner(ev)).toBe('661419f8')
    // parseAdmins still returns the full list (for the admin set)
    expect(parseAdmins(ev)).toEqual(['7bea8ec2', '661419f8'])
  })

  it('picks the owner when listed first too', () => {
    const ev = adminsEvent([
      ['661419f8', 'owner'],
      ['7bea8ec2', 'moderator'],
    ])
    expect(parseOwner(ev)).toBe('661419f8')
  })

  it('falls back to the first admin when no owner role is tagged', () => {
    const ev = adminsEvent([
      ['aaa', 'moderator'],
      ['bbb', 'moderator'],
    ])
    expect(parseOwner(ev)).toBe('aaa')
  })

  it('returns empty string for an admins event with no p-tags', () => {
    expect(parseOwner(adminsEvent([]))).toBe('')
  })
})

function nowPlayingEvent({
  club,
  dj,
  sentAt,
  status = 'playing',
}: {
  club: string
  dj: string
  sentAt: number
  status?: 'playing' | 'paused'
}): Event {
  return {
    kind: 30100,
    tags: [['h', club], ['dj', dj], ['sent_at', String(sentAt)], ['status', status]],
    content: 'Artist - Track',
    created_at: Math.floor(sentAt / 1000),
    pubkey: 'relay',
    id: `${club}-${sentAt}`,
    sig: 'x',
  } as Event
}

describe('selectOnAirClubDjs', () => {
  const nowMs = 2_000_000

  it('returns the DJ of a fresh playing club', () => {
    const events = [nowPlayingEvent({ club: 'club-a', dj: 'dj-a', sentAt: nowMs - 10_000 })]
    expect(selectOnAirClubDjs(events, ['club-a'], nowMs)).toEqual(new Map([['club-a', 'dj-a']]))
  })

  it('ignores stale, paused and unrequested clubs', () => {
    const events = [
      nowPlayingEvent({ club: 'stale', dj: 'dj-a', sentAt: nowMs - 150_000 }),
      nowPlayingEvent({ club: 'paused', dj: 'dj-b', sentAt: nowMs - 1_000, status: 'paused' }),
      nowPlayingEvent({ club: 'other', dj: 'dj-c', sentAt: nowMs - 1_000 }),
    ]
    expect(selectOnAirClubDjs(events, ['stale', 'paused'], nowMs)).toEqual(new Map())
  })

  it('does not present the relay author as a DJ when the dj tag is missing', () => {
    const event = nowPlayingEvent({ club: 'club-a', dj: 'dj-a', sentAt: nowMs - 1_000 })
    event.tags = event.tags.filter((tag) => tag[0] !== 'dj')
    expect(selectOnAirClubDjs([event], ['club-a'], nowMs)).toEqual(new Map())
  })

  it('uses the newest on-air event per club', () => {
    const events = [
      nowPlayingEvent({ club: 'club-a', dj: 'old-dj', sentAt: nowMs - 20_000 }),
      nowPlayingEvent({ club: 'club-a', dj: 'current-dj', sentAt: nowMs - 5_000 }),
    ]
    expect(selectOnAirClubDjs(events, ['club-a'], nowMs).get('club-a')).toBe('current-dj')
  })

  it('keeps the current track title for the club directory player row', () => {
    const event = nowPlayingEvent({ club: 'club-a', dj: 'dj-a', sentAt: nowMs - 1_000 })
    event.content = 'Artist – Track title'
    expect(selectOnAirClubTracks([event], ['club-a'], nowMs).get('club-a')).toEqual({
      dj: 'dj-a',
      sentAt: nowMs - 1_000,
      title: 'Artist – Track title',
    })
  })
})

describe('selectClubMemberCounts', () => {
  const countEvent = (club: string, count: string, sentAt: number, pubkey = CLUB_RELAY_PUBKEY) => ({
    kind: KIND_MEMBER_COUNT,
    tags: [['d', club], ['h', club], ['count', count], ['sent_at', String(sentAt)]],
    content: '',
    created_at: Math.floor(sentAt / 1000),
    pubkey,
    id: `${club}-${sentAt}`,
    sig: 'x',
  }) as Event

  it('accepts the newest relay-signed aggregate without exposing identities', () => {
    const events = [countEvent('club-a', '2', 1_000), countEvent('club-a', '3', 2_000)]
    expect(selectClubMemberCounts(events, ['club-a']).get('club-a')).toEqual({ count: 3, sentAt: 2_000 })
    expect(events[1].tags.some((tag) => tag[0] === 'p')).toBe(false)
  })

  it('rejects forged, malformed and unrequested aggregates', () => {
    const events = [
      countEvent('club-a', '99', 2_000, 'f'.repeat(64)),
      countEvent('club-a', '-1', 2_000),
      countEvent('club-b', '4', 2_000),
    ]
    expect(selectClubMemberCounts(events, ['club-a'])).toEqual(new Map())
  })
})

function stageEvent({
  club,
  dj,
  principal,
  createdAt,
  since = createdAt,
  on = true,
  id,
}: {
  club: string
  dj: string
  principal?: string
  createdAt: number
  since?: number
  on?: boolean
  id?: string
}): Event {
  return {
    kind: 30102,
    tags: [
      ['h', club],
      ['since', String(since)],
      ...(principal ? [['p', principal], ['client', SESSION_EVENT_MARKER]] : []),
    ],
    content: on ? 'on' : 'off',
    created_at: createdAt,
    pubkey: dj,
    id: id ?? `${club}-${dj}-${createdAt}`,
    sig: 'x',
  } as Event
}

describe('selectOnStageClubDjs', () => {
  const nowMs = 2_000_000

  it('returns a fresh DJ who is waiting on stage', () => {
    const events = [stageEvent({ club: 'club-a', dj: 'bot', createdAt: 1_950 })]
    expect(selectOnStageClubDjs(events, ['club-a'], nowMs)).toEqual(new Map([['club-a', 'bot']]))
  })

  it('honours the newest off event and ignores stale DJs', () => {
    const events = [
      stageEvent({ club: 'club-a', dj: 'left', createdAt: 1_800 }),
      stageEvent({ club: 'club-a', dj: 'left', createdAt: 1_990, on: false }),
      stageEvent({ club: 'club-a', dj: 'stale', createdAt: 1_700 }),
    ]
    expect(selectOnStageClubDjs(events, ['club-a'], nowMs)).toEqual(new Map())
  })

  it('selects the DJ who joined the stage first', () => {
    const events = [
      stageEvent({ club: 'club-a', dj: 'second', createdAt: 1_990, since: 1_200 }),
      stageEvent({ club: 'club-a', dj: 'first', createdAt: 1_980, since: 1_100 }),
    ]
    expect(selectOnStageClubDjs(events, ['club-a'], nowMs).get('club-a')).toBe('first')
  })

  it('deduplicates session-key events by their relay-bound principal', () => {
    const principal = 'a'.repeat(64)
    const events = [
      stageEvent({ club: 'club-a', dj: 'b'.repeat(64), principal, createdAt: 1_980 }),
      stageEvent({ club: 'club-a', dj: 'c'.repeat(64), principal, createdAt: 1_990, on: false }),
    ]
    expect(selectOnStageClubDjs(events, ['club-a'], nowMs)).toEqual(new Map())
  })

  it('uses the canonical lower id when replaceable events share a timestamp', () => {
    const events = [
      stageEvent({ club: 'club-a', dj: 'same', createdAt: 1_990, on: true, id: 'f'.repeat(64) }),
      stageEvent({ club: 'club-a', dj: 'same', createdAt: 1_990, on: false, id: '0'.repeat(64) }),
    ]
    expect(selectOnStageClubDjs(events, ['club-a'], nowMs)).toEqual(new Map())
    expect(selectOnStageClubDjs([...events].reverse(), ['club-a'], nowMs)).toEqual(new Map())
  })
})

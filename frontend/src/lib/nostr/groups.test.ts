// @vitest-environment happy-dom
// (groups.ts → nostrLogin.ts → router reads location.pathname at import time)
import { describe, it, expect } from 'vitest'
import { parseOwner, parseAdmins } from './groups'
import type { Event } from 'nostr-tools/pure'

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

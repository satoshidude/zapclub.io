// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Event } from 'nostr-tools/pure'

const PRINCIPAL = 'a'.repeat(64)
const SESSION_KEY = 'b'.repeat(64)

const mocked = vi.hoisted(() => ({
  querySync: vi.fn(),
  queryClubAuthed: vi.fn().mockResolvedValue([]),
}))

vi.mock('./pool', () => ({
  CLUB_RELAY: 'wss://relay.test',
  pool: { querySync: mocked.querySync },
}))
vi.mock('./auth.svelte', () => ({ auth: { pubkey: 'a'.repeat(64) } }))
vi.mock('./nip98', () => ({ nip98Header: vi.fn() }))
vi.mock('./groups', () => ({
  queryClubAuthed: mocked.queryClubAuthed,
  parseClubMetadata: () => ({ id: 'club', name: 'Club', open: true, isPublic: true, closed: false, isPrivate: false }),
  parseMembers: () => [],
  parseAdmins: () => [],
  parseOwner: () => '',
}))

function event(kind: number, id: string, pubkey: string, content = ''): Event {
  return {
    kind,
    id,
    pubkey,
    sig: 'sig',
    created_at: Math.floor(Date.now() / 1000),
    content,
    tags: kind === 39000
      ? [['d', 'club']]
      : [['h', 'club'], ['p', PRINCIPAL], ['client', 'zapclub-session-v1']],
  } as Event
}

beforeEach(() => {
  mocked.querySync.mockReset()
  mocked.queryClubAuthed.mockResolvedValue([])
})

describe('admin stage principals', () => {
  it('shows a session-signed DJ under the authenticated identity with canonical ties', async () => {
    const meta = event(39000, 'meta', 'relay')
    const losingOn = event(30102, 'f'.repeat(64), SESSION_KEY, 'on')
    const canonicalOff = event(30102, '0'.repeat(64), SESSION_KEY, 'off')
    mocked.querySync.mockImplementation((_relays, filter) => {
      if (filter.kinds[0] === 39000) return Promise.resolve([meta])
      if (filter.kinds[0] === 30102) return Promise.resolve([losingOn, canonicalOff])
      return Promise.resolve([])
    })

    const { loadAdminData } = await import('./admin')
    const result = await loadAdminData()

    expect(result).toHaveLength(1)
    expect(result[0].djs).toEqual([])

    canonicalOff.content = 'on'
    const active = await loadAdminData()
    expect(active[0].djs).toEqual([PRINCIPAL])
  })
})

// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Event, EventTemplate } from 'nostr-tools/pure'

const mocked = vi.hoisted(() => ({
  publishSessionClub: vi.fn<(template: EventTemplate, intent?: 'automatic' | 'explicit') => Promise<Event>>(),
}))

vi.mock('./groups', () => ({
  KIND_STAGE: 30102,
  publishSessionClub: mocked.publishSessionClub,
}))
vi.mock('./auth.svelte', () => ({ auth: { pubkey: 'a'.repeat(64) } }))
vi.mock('./presence.svelte', () => ({ startPresence: vi.fn(), stopPresence: vi.fn() }))
vi.mock('./sessionSigner', () => ({ sessionEventPrincipal: (event: Event) => event.pubkey }))

beforeEach(() => {
  vi.useFakeTimers()
  mocked.publishSessionClub.mockReset()
  mocked.publishSessionClub.mockImplementation(async (template) => ({
    ...template,
    pubkey: 'a'.repeat(64),
    id: 'event',
    sig: 'sig',
  } as Event))
})

afterEach(async () => {
  const { resetStage } = await import('./stage.svelte')
  resetStage()
  vi.useRealTimers()
})

describe('stage leave intent', () => {
  it('keeps relay-kick cleanup automatic while user/logout leave stays explicit', async () => {
    const { leaveStage } = await import('./stage.svelte')

    await leaveStage('club', 'automatic')
    await leaveStage('club')

    expect(mocked.publishSessionClub.mock.calls.map(([, intent]) => intent)).toEqual([
      'automatic',
      'explicit',
    ])
  })
})

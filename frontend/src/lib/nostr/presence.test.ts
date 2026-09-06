// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { EventTemplate } from 'nostr-tools'

const mocked = vi.hoisted(() => ({
  auth: { pubkey: 'a'.repeat(64), canSign: true, method: 'extension' },
  publishSessionClub: vi.fn<(event: EventTemplate) => Promise<void>>(async (_event) => undefined),
}))

vi.mock('./auth.svelte', () => ({ auth: mocked.auth }))
vi.mock('./groups', () => ({
  KIND_PRESENCE: 20100,
  KIND_STAGE: 30102,
  publishSessionClub: mocked.publishSessionClub,
}))

describe('shared presence publisher', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.resetModules()
    mocked.publishSessionClub.mockClear()
    const values = new Map<string, string>()
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      value: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
        removeItem: (key: string) => values.delete(key),
        clear: () => values.clear(),
      },
    })
  })

  afterEach(() => vi.useRealTimers())

  it('deduplicates equal view and stage claims across timer and visibility impulses', async () => {
    const { startPresence, stopPresence } = await import('./presence.svelte')
    startPresence('club', 'view')
    startPresence('club', 'stage')
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(300_000)

    expect(mocked.publishSessionClub.mock.calls.filter(([event]) => event.kind === 20100)).toHaveLength(13)

    stopPresence('view')
    await vi.advanceTimersByTimeAsync(25_000)
    expect(mocked.publishSessionClub.mock.calls.filter(([event]) => event.kind === 20100)).toHaveLength(14)

    stopPresence('stage')
    await vi.advanceTimersByTimeAsync(50_000)
    expect(mocked.publishSessionClub.mock.calls.filter(([event]) => event.kind === 20100)).toHaveLength(14)
  })

  it('keeps distinct sticky-stage and viewed-club presence alive', async () => {
    const { startPresence, resetPresence } = await import('./presence.svelte')
    startPresence('view-club', 'view')
    startPresence('stage-club', 'stage')
    await vi.advanceTimersByTimeAsync(25_000)

    const presence = mocked.publishSessionClub.mock.calls
      .filter(([event]) => event.kind === 20100)
      .map(([event]) => event.tags.find((tag: string[]) => tag[0] === 'h')?.[1])
    expect(presence.filter((group) => group === 'view-club')).toHaveLength(2)
    expect(presence.filter((group) => group === 'stage-club')).toHaveLength(2)
    resetPresence()
  })

  it('uses one local presence stream plus the stage lease for an active DJ', async () => {
    const { startPresence } = await import('./presence.svelte')
    const { joinStage } = await import('./stage.svelte')
    startPresence('club', 'view')
    await joinStage('club')
    await vi.advanceTimersByTimeAsync(300_000)

    expect(mocked.publishSessionClub.mock.calls.filter(([event]) => event.kind === 20100)).toHaveLength(13)
    expect(mocked.publishSessionClub.mock.calls.filter(([event]) => event.kind === 30102)).toHaveLength(3)
  })
})

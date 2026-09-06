// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Event, EventTemplate, VerifiedEvent } from 'nostr-tools/pure'

const USER = 'a'.repeat(64)
const AUTH_TEMPLATE: EventTemplate = {
  kind: 22242,
  created_at: 1,
  tags: [['relay', 'wss://relay.test'], ['challenge', 'challenge']],
  content: '',
}

const mocked = vi.hoisted(() => ({
  auth: { pubkey: 'a'.repeat(64) as string | null, canSign: true },
  signEvent: vi.fn<(template: EventTemplate, options?: { allowNip46Reconnect?: boolean }) => Promise<Event>>(),
  closeClubRelay: vi.fn(),
  setClubAuthFailureHandler: vi.fn(),
  publish: vi.fn(),
  subscribeEose: vi.fn(),
  subscribe: vi.fn(),
  subscribeMany: vi.fn(),
  querySync: vi.fn(),
  signingStateListeners: new Set<(state: {
    pubkey: string | null
    signerReady: boolean
    canSign: boolean
  }) => void>(),
}))

vi.mock('./auth.svelte', () => ({
  auth: mocked.auth,
  onAuthSigningStateChange: vi.fn((listener) => {
    mocked.signingStateListeners.add(listener)
    return () => mocked.signingStateListeners.delete(listener)
  }),
}))
vi.mock('./nostrLogin', () => ({ signEvent: mocked.signEvent }))
vi.mock('./pool', () => ({
  CLUB_RELAY: 'wss://relay.test',
  CLUB_RELAY_PUBKEY: 'relay',
  PROFILE_RELAYS: [],
  closeClubRelay: mocked.closeClubRelay,
  setClubAuthFailureHandler: mocked.setClubAuthFailureHandler,
  pool: {
    publish: mocked.publish,
    subscribeEose: mocked.subscribeEose,
    subscribe: mocked.subscribe,
    subscribeMany: mocked.subscribeMany,
    querySync: mocked.querySync,
  },
}))
vi.mock('./zaps.svelte', () => ({ resolveZapper: vi.fn() }))
vi.mock('./playlog', () => ({ SESSION_LOOKBACK_MS: 3_600_000 }))

function authEvent(): VerifiedEvent {
  return {
    ...AUTH_TEMPLATE,
    pubkey: USER,
    id: 'auth',
    sig: '0'.repeat(128),
  } as VerifiedEvent
}

function sessionTemplate(kind = 20100): EventTemplate {
  return { kind, created_at: 1, tags: [['h', 'club']], content: '' }
}

function publishThroughAuth(): void {
  mocked.publish.mockImplementation((_relays, _event, options) => [
    new Promise<string>((resolve) => {
      void options.onauth(AUTH_TEMPLATE).then(() => resolve('ok')).catch(() => {})
    }),
  ])
}

beforeEach(() => {
  vi.resetModules()
  vi.useFakeTimers()
  mocked.auth.pubkey = USER
  mocked.auth.canSign = true
  mocked.signEvent.mockReset()
  mocked.closeClubRelay.mockReset()
  mocked.setClubAuthFailureHandler.mockReset()
  mocked.publish.mockReset()
  mocked.subscribeEose.mockReset()
  mocked.subscribe.mockReset()
  mocked.subscribeMany.mockReset()
  mocked.querySync.mockReset()
  mocked.signingStateListeners.clear()
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})

afterEach(async () => {
  const presence = await import('./presence.svelte')
  presence.resetPresence()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('club relay AUTH lifecycle', () => {
  it('rejects a publish immediately when AUTH signing fails and closes the socket', async () => {
    mocked.signEvent.mockRejectedValue(new Error('User rejected AUTH'))
    publishThroughAuth()
    const { publishSessionClub } = await import('./groups')

    await expect(publishSessionClub(sessionTemplate())).rejects.toThrow('User rejected AUTH')

    expect(mocked.signEvent).toHaveBeenCalledTimes(1)
    expect(mocked.signEvent).toHaveBeenCalledWith(AUTH_TEMPLATE, { allowNip46Reconnect: false })
    expect(mocked.closeClubRelay).toHaveBeenCalledTimes(1)
  })

  it('latches and closes when the relay rejects an otherwise signed AUTH event', async () => {
    mocked.signEvent.mockResolvedValue(authEvent())
    mocked.publish.mockImplementation((_relays, _event, options) => [
      options.onauth(AUTH_TEMPLATE).then(() => Promise.reject(new Error('error: failed to authenticate'))),
    ])
    const groups = await import('./groups')

    await expect(groups.publishSessionClub(sessionTemplate())).rejects.toThrow('failed to authenticate')
    await expect(groups.publishSessionClub(sessionTemplate())).rejects.toBeInstanceOf(groups.ClubAuthPausedError)
    expect(mocked.closeClubRelay).toHaveBeenCalledTimes(1)
    expect(mocked.publish).toHaveBeenCalledTimes(1)
  })

  it('keeps five minutes of presence timers from making another signer call while paused', async () => {
    mocked.signEvent.mockRejectedValue(new Error('User rejected AUTH'))
    publishThroughAuth()
    const groups = await import('./groups')
    await expect(groups.publishSessionClub(sessionTemplate())).rejects.toThrow()
    const callsAtPause = mocked.signEvent.mock.calls.length
    const publishesAtPause = mocked.publish.mock.calls.length

    const { startPresence } = await import('./presence.svelte')
    startPresence('club')
    await vi.advanceTimersByTimeAsync(300_000)

    expect(mocked.signEvent).toHaveBeenCalledTimes(callsAtPause)
    expect(mocked.publish).toHaveBeenCalledTimes(publishesAtPause)
  })

  it('bounds a signer that never settles and closes its club socket', async () => {
    mocked.signEvent.mockReturnValue(new Promise(() => {}))
    publishThroughAuth()
    const { publishSessionClub } = await import('./groups')
    const result = publishSessionClub(sessionTemplate())
    const rejection = expect(result).rejects.toThrow('club relay AUTH: timeout')

    await vi.advanceTimersByTimeAsync(15_000)
    await rejection
    expect(mocked.signEvent).toHaveBeenCalledTimes(1)
    expect(mocked.closeClubRelay).toHaveBeenCalledTimes(1)
  })

  it('allows exactly one fresh AUTH attempt for the next explicit stage join', async () => {
    mocked.signEvent.mockRejectedValueOnce(new Error('User rejected AUTH'))
      .mockResolvedValue(authEvent())
    publishThroughAuth()
    const groups = await import('./groups')
    await expect(groups.publishSessionClub(sessionTemplate(30102))).rejects.toThrow()

    await expect(groups.publishSessionClub(sessionTemplate(30102), 'automatic'))
      .rejects.toBeInstanceOf(groups.ClubAuthPausedError)
    expect(mocked.signEvent).toHaveBeenCalledTimes(1)

    await expect(groups.publishSessionClub(sessionTemplate(30102), 'explicit')).resolves.toBeTruthy()
    expect(mocked.signEvent).toHaveBeenCalledTimes(2)
    expect(mocked.closeClubRelay).toHaveBeenCalledTimes(2)
  })

  it('settles and cleans up an authenticated query when AUTH is rejected', async () => {
    mocked.signEvent.mockRejectedValue(new Error('AUTH denied'))
    const close = vi.fn()
    mocked.subscribeEose.mockImplementation((_relays, _filter, options) => {
      queueMicrotask(() => { void options.onauth(AUTH_TEMPLATE).catch(() => {}) })
      return { close }
    })
    const { queryClubAuthed } = await import('./groups')

    await expect(queryClubAuthed({ kinds: [39002] })).rejects.toThrow('AUTH denied')
    expect(close).toHaveBeenCalledWith('club query cleanup')
    expect(mocked.closeClubRelay).toHaveBeenCalledTimes(1)
  })

  it('fetches join-request proof data only through the authenticated query path', async () => {
    mocked.subscribeEose.mockImplementation((_relays, _filter, options) => {
      queueMicrotask(() => {
        options.onevent({
          kind: 9021,
          created_at: 10,
          pubkey: 'b'.repeat(64),
          id: 'join',
          sig: 'sig',
          content: '',
          tags: [['h', 'club'], ['proof', 'private-proof']],
        } as Event)
        options.onclose(['closed automatically on eose'])
      })
      return { close: vi.fn() }
    })
    const { fetchJoinRequests } = await import('./groups')

    await expect(fetchJoinRequests('club')).resolves.toEqual([
      { pubkey: 'b'.repeat(64), createdAt: 10 },
    ])
    expect(mocked.subscribeEose).toHaveBeenCalledWith(
      ['wss://relay.test'],
      { kinds: [9021], '#h': ['club'] },
      expect.objectContaining({ onauth: expect.any(Function) }),
    )
    expect(mocked.querySync).not.toHaveBeenCalled()
  })

  it('treats SimplePool connection-failure strings as failures and latches automatic work', async () => {
    mocked.publish.mockReturnValue([Promise.resolve('connection failure: offline')])
    const groups = await import('./groups')

    await expect(groups.publishSessionClub(sessionTemplate())).rejects.toThrow('connection failure: offline')
    await expect(groups.publishSessionClub(sessionTemplate())).rejects.toBeInstanceOf(groups.ClubAuthPausedError)
    expect(mocked.publish).toHaveBeenCalledTimes(1)
    expect(mocked.closeClubRelay).toHaveBeenCalledTimes(1)
  })

  it('signs an automatic broken-track report without allowing a NIP-46 reconnect', async () => {
    mocked.signEvent.mockImplementation(async (template) => ({
      ...template,
      pubkey: USER,
      id: 'report',
      sig: '0'.repeat(128),
    } as Event))
    mocked.publish.mockReturnValue([Promise.resolve('ok')])
    const { reportBrokenTrack } = await import('./groups')

    await reportBrokenTrack('club', 'video')

    expect(mocked.signEvent).toHaveBeenCalledTimes(1)
    expect(mocked.signEvent).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 20102, content: 'video' }),
      { allowNip46Reconnect: false },
    )
  })

  it('does not start a scheduled publish after the account context changes', async () => {
    publishThroughAuth()
    const groups = await import('./groups')
    const pending = groups.publishSessionClub(sessionTemplate())

    mocked.auth.pubkey = 'b'.repeat(64)
    groups.resetClubAuthState()

    await expect(pending).rejects.toThrow('authentication reset')
    await Promise.resolve()
    expect(mocked.publish).not.toHaveBeenCalled()
    expect(mocked.signEvent).not.toHaveBeenCalled()
  })

  it('reports session presence under its strict relay-bound principal', async () => {
    let onEvent!: (event: Event) => void
    mocked.subscribeMany.mockImplementation((_relays, _filter, options) => {
      onEvent = options.onevent
      return { close: vi.fn() }
    })
    const onBeat = vi.fn()
    const { subscribeClubPresence } = await import('./groups')
    const stop = subscribeClubPresence(['club'], onBeat)
    onEvent({
      kind: 20100,
      created_at: 10,
      pubkey: 'b'.repeat(64),
      id: 'event',
      sig: 'sig',
      content: '',
      tags: [['h', 'club'], ['p', USER], ['client', 'zapclub-session-v1']],
    } as Event)

    expect(onBeat).toHaveBeenCalledWith('club', USER, 10_000)
    stop()
  })

  it('reopens public club streams without AUTH as soon as authentication pauses', async () => {
    mocked.subscribe.mockImplementation(() => ({ close: vi.fn() }))
    mocked.signEvent.mockRejectedValue(new Error('AUTH denied'))
    publishThroughAuth()
    const groups = await import('./groups')
    const stop = groups.subscribeClub('club', {})
    expect(mocked.subscribe).toHaveBeenCalledTimes(6)

    await expect(groups.publishSessionClub(sessionTemplate())).rejects.toThrow('AUTH denied')

    expect(mocked.subscribe).toHaveBeenCalledTimes(9)
    for (const call of mocked.subscribe.mock.calls.slice(-3)) {
      expect(call[2]).not.toHaveProperty('onauth')
    }
    stop()
  })

  it('scopes live membership transitions to the active authenticated account', async () => {
    mocked.subscribe.mockImplementation(() => ({ close: vi.fn() }))
    const groups = await import('./groups')
    const stop = groups.subscribeClub('club', {})

    const membershipCall = mocked.subscribe.mock.calls.find((call) =>
      call[1]?.kinds?.includes(9000) && call[1]?.kinds?.includes(9001))
    expect(membershipCall?.[1]).toMatchObject({
      kinds: [9000, 9001],
      '#h': ['club'],
      '#p': [USER],
    })
    stop()
  })

  it('adds protected club streams once delayed signer readiness becomes available', async () => {
    mocked.auth.canSign = false
    mocked.subscribe.mockImplementation(() => ({ close: vi.fn() }))
    const groups = await import('./groups')
    const stop = groups.subscribeClub('club', {})

    expect(mocked.subscribe).toHaveBeenCalledTimes(3)
    for (const call of mocked.subscribe.mock.calls) expect(call[2]).not.toHaveProperty('onauth')

    mocked.auth.canSign = true
    for (const listener of mocked.signingStateListeners) {
      listener({ pubkey: USER, signerReady: true, canSign: true })
    }

    expect(mocked.subscribe).toHaveBeenCalledTimes(9)
    expect(mocked.subscribe.mock.calls.slice(-6).filter((call) => 'onauth' in call[2])).toHaveLength(6)
    expect(mocked.signEvent).not.toHaveBeenCalled()

    // Repeated ready=true notifications must not spawn another subscription/auth wave.
    for (const listener of mocked.signingStateListeners) {
      listener({ pubkey: USER, signerReady: true, canSign: true })
    }
    expect(mocked.subscribe).toHaveBeenCalledTimes(9)
    stop()
  })

  it('does not reopen an old club subscription during a direct account switch', async () => {
    mocked.subscribe.mockImplementation(() => ({ close: vi.fn() }))
    const groups = await import('./groups')
    const stop = groups.subscribeClub('club', {})
    expect(mocked.subscribe).toHaveBeenCalledTimes(6)

    const nextPubkey = 'b'.repeat(64)
    mocked.auth.pubkey = nextPubkey
    for (const listener of mocked.signingStateListeners) {
      listener({ pubkey: nextPubkey, signerReady: true, canSign: true })
    }

    expect(mocked.subscribe).toHaveBeenCalledTimes(6)
    expect(mocked.signEvent).not.toHaveBeenCalled()
    stop()
  })
})

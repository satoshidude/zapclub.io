// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  finalizeEvent,
  generateSecretKey,
  getEventHash,
  type EventTemplate,
  type VerifiedEvent,
} from 'nostr-tools/pure'

vi.mock('./pool', () => ({
  fetchProfile: vi.fn().mockResolvedValue(null),
  closeClubRelay: vi.fn(),
}))
vi.mock('./groups', () => ({ resetClubAuthState: vi.fn() }))
vi.mock('../router.svelte', () => ({ goHome: vi.fn() }))
vi.mock('./loginDialog.svelte', () => ({
  openLoginDialog: vi.fn(),
  closeLoginDialog: vi.fn(),
}))
vi.mock('./sync.svelte', () => ({ resetSync: vi.fn() }))
vi.mock('./stage.svelte', () => ({ resetStage: vi.fn(), leaveStage: vi.fn().mockResolvedValue(undefined) }))
vi.mock('./presence.svelte', () => ({ resetPresence: vi.fn() }))
vi.mock('./queue.svelte', () => ({ resetQueues: vi.fn() }))
vi.mock('./playlists.svelte', () => ({ resetPlaylists: vi.fn() }))
vi.mock('./zaps.svelte', () => ({ resetZaps: vi.fn() }))

const USER_PUBKEY = 'a'.repeat(64)
const REMOTE_PUBKEY = 'b'.repeat(64)

function memoryStorage(): Storage {
  const values = new Map<string, string>()
  return {
    get length() {
      return values.size
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    removeItem: (key) => values.delete(key),
    setItem: (key, value) => values.set(key, String(value)),
  }
}

function setNostrProvider(provider: {
  getPublicKey(): Promise<string>
  signEvent(template: EventTemplate): Promise<VerifiedEvent>
}): void {
  Object.defineProperty(window, 'nostr', { value: provider, configurable: true, writable: true })
}

function clearNostrProvider(): void {
  Reflect.deleteProperty(window, 'nostr')
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.resetModules()
  Object.defineProperty(globalThis, 'localStorage', {
    value: memoryStorage(),
    configurable: true,
  })
  localStorage.clear()
  clearNostrProvider()
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
  localStorage.clear()
  clearNostrProvider()
})

describe('signer lifecycle', () => {
  it('hands a successful extension login to accountWatch without a second focus key read', async () => {
    const getPublicKey = vi.fn().mockResolvedValue(USER_PUBKEY)
    setNostrProvider({
      getPublicKey,
      signEvent: vi.fn().mockRejectedValue(new Error('unused')),
    })

    const login = await import('./nostrLogin')
    const watch = await import('./accountWatch.svelte')
    login.initAuth()
    watch.startAccountWatch()
    expect(getPublicKey).not.toHaveBeenCalled()

    await login.loginExtension()
    expect(getPublicKey).toHaveBeenCalledTimes(1)

    window.dispatchEvent(new Event('focus'))
    await Promise.resolve()
    expect(getPublicKey).toHaveBeenCalledTimes(1)
    watch.stopAccountWatch()
  })

  it('clears an extension-account mismatch on logout without waiting for focus', async () => {
    const getPublicKey = vi.fn()
      .mockResolvedValueOnce(USER_PUBKEY)
      .mockResolvedValue(REMOTE_PUBKEY)
    setNostrProvider({
      getPublicKey,
      signEvent: vi.fn().mockRejectedValue(new Error('unused')),
    })

    const login = await import('./nostrLogin')
    const watch = await import('./accountWatch.svelte')
    login.initAuth()
    await login.loginExtension()
    watch.startAccountWatch()
    await Promise.resolve()
    await Promise.resolve()
    expect(watch.accountWatch.mismatch).toBe(true)

    await login.logout()
    expect(watch.accountWatch.mismatch).toBe(false)
    watch.stopAccountWatch()
  })

  it('sends a rejected extension signature exactly once and classifies the error', async () => {
    const getPublicKey = vi.fn().mockResolvedValue(USER_PUBKEY)
    const physicalSign = vi.fn().mockRejectedValue(new Error('User rejected the request'))
    setNostrProvider({ getPublicKey, signEvent: physicalSign })

    const diagnostics = await import('./signingDiagnostics')
    diagnostics.setSigningDiagnosticsEnabled(true)
    const login = await import('./nostrLogin')
    login.initAuth()
    await login.loginExtension()

    const template: EventTemplate = { kind: 1, created_at: 1, tags: [], content: 'share' }
    const failure = await login.signEvent(template).catch((error: unknown) => error)

    expect(failure).toBeInstanceOf(login.SignerOperationError)
    expect((failure as InstanceType<typeof login.SignerOperationError>).code).toBe('rejected')
    expect(physicalSign).toHaveBeenCalledTimes(1)
    expect(getPublicKey).toHaveBeenCalledTimes(1)

    const snapshot = diagnostics.signingDiagnosticsSnapshot()
    expect(snapshot.logicalSignRequests).toContainEqual({
      kind: 1,
      trigger: 'nostrLogin',
      count: 1,
    })
    expect(snapshot.physicalSignEventCalls).toContainEqual({
      kind: 1,
      trigger: 'nostrLogin',
      count: 1,
    })
    expect(snapshot.nip07GetPublicKeyCalls).toContainEqual({ source: 'nostrLogin', count: 1 })
  })

  it('rejects a valid event from a different extension account without retrying', async () => {
    const getPublicKey = vi.fn().mockResolvedValue(USER_PUBKEY)
    const otherKey = generateSecretKey()
    const physicalSign = vi.fn().mockImplementation(async (template: EventTemplate) =>
      finalizeEvent(template, otherKey),
    )
    setNostrProvider({ getPublicKey, signEvent: physicalSign })

    const login = await import('./nostrLogin')
    login.initAuth()
    await login.loginExtension()

    const failure = await login.signEvent({ kind: 1, created_at: 1, tags: [], content: '' })
      .catch((error: unknown) => error)

    expect(failure).toBeInstanceOf(login.SignerOperationError)
    expect((failure as InstanceType<typeof login.SignerOperationError>).code).toBe('invalid')
    expect(physicalSign).toHaveBeenCalledTimes(1)
  })

  it('closes account-authenticated relay sockets when the active account changes', async () => {
    const getPublicKey = vi.fn()
      .mockResolvedValueOnce(USER_PUBKEY)
      .mockResolvedValueOnce(REMOTE_PUBKEY)
    setNostrProvider({
      getPublicKey,
      signEvent: vi.fn().mockRejectedValue(new Error('unused')),
    })

    const login = await import('./nostrLogin')
    const { closeClubRelay } = await import('./pool')
    const { resetPresence } = await import('./presence.svelte')
    login.initAuth()
    await login.loginExtension()
    await login.loginExtension()

    expect(closeClubRelay).toHaveBeenCalledTimes(1)
    expect(resetPresence).toHaveBeenCalledTimes(1)
  })

  it('shares one restore connection between warm-up and concurrent first signatures', async () => {
    const { NostrConnectSigner } = await import('applesauce-signers')
    let resolveConnect!: (value: string) => void
    const connected = new Promise<string>((resolve) => {
      resolveConnect = resolve
    })
    const connect = vi.spyOn(NostrConnectSigner.prototype, 'connect').mockImplementation(function (
      this: InstanceType<typeof NostrConnectSigner>,
    ) {
      return connected.then((value) => {
        this.isConnected = true
        return value
      })
    })
    const physicalSign = vi.spyOn(NostrConnectSigner.prototype, 'signEvent').mockImplementation(
      async (template) => {
        const unsigned = template as EventTemplate & { pubkey: string }
        return {
          ...unsigned,
          id: getEventHash(unsigned),
          sig: '0'.repeat(128),
        } as VerifiedEvent
      },
    )

    localStorage.setItem('zapclub:accounts', JSON.stringify({
      active: USER_PUBKEY,
      accounts: [{
        id: 'remote-account',
        type: 'nostr-connect',
        pubkey: USER_PUBKEY,
        signer: {
          clientKey: '1'.repeat(64),
          remote: REMOTE_PUBKEY,
          relays: ['wss://remote.test'],
        },
      }],
    }))

    const login = await import('./nostrLogin')
    const { auth, onAuthSigningStateChange } = await import('./auth.svelte')
    const readiness = vi.fn()
    const stopReadiness = onAuthSigningStateChange(readiness)
    login.initAuth()

    expect(auth.isLoggedIn).toBe(true)
    expect(auth.signerReady).toBe(false)
    expect(auth.canSign).toBe(false)
    expect(connect).toHaveBeenCalledTimes(1)

    const first = login.signEvent({ kind: 9, created_at: 1, tags: [], content: 'one' })
    const second = login.signEvent({ kind: 9, created_at: 2, tags: [], content: 'two' })
    expect(connect).toHaveBeenCalledTimes(1)

    resolveConnect('ack')
    await expect(Promise.all([first, second])).resolves.toHaveLength(2)

    expect(connect).toHaveBeenCalledTimes(1)
    expect(connect).toHaveBeenCalledWith(undefined, [...login.NIP46_PERMISSIONS])
    expect(physicalSign).toHaveBeenCalledTimes(2)
    expect(auth.signerReady).toBe(true)
    expect(auth.canSign).toBe(true)
    expect(readiness).toHaveBeenCalledWith(expect.objectContaining({
      pubkey: USER_PUBKEY,
      signerReady: true,
      canSign: true,
    }))
    stopReadiness()
  })

  it('replaces a timed-out bunker flight only on the next explicit signer action', async () => {
    vi.useFakeTimers()
    const { NostrConnectSigner } = await import('applesauce-signers')
    let resolveOld!: (value: string) => void
    const oldFlight = new Promise<string>((resolve) => {
      resolveOld = resolve
    })
    let connectCalls = 0
    const connect = vi.spyOn(NostrConnectSigner.prototype, 'connect').mockImplementation(function (
      this: InstanceType<typeof NostrConnectSigner>,
    ) {
      connectCalls++
      if (connectCalls === 1) {
        return oldFlight.then((value) => {
          this.isConnected = true
          return value
        })
      }
      this.isConnected = true
      return Promise.resolve('ack')
    })
    const close = vi.spyOn(NostrConnectSigner.prototype, 'close')
    const physicalSign = vi.spyOn(NostrConnectSigner.prototype, 'signEvent').mockImplementation(
      async (template) => {
        const unsigned = template as EventTemplate & { pubkey: string }
        return {
          ...unsigned,
          id: getEventHash(unsigned),
          sig: '0'.repeat(128),
        } as VerifiedEvent
      },
    )

    localStorage.setItem('zapclub:accounts', JSON.stringify({
      active: USER_PUBKEY,
      accounts: [{
        id: 'remote-account',
        type: 'nostr-connect',
        pubkey: USER_PUBKEY,
        signer: {
          clientKey: '1'.repeat(64),
          remote: REMOTE_PUBKEY,
          relays: ['wss://remote.test'],
        },
      }],
    }))

    const login = await import('./nostrLogin')
    const { auth } = await import('./auth.svelte')
    login.initAuth()
    expect(connect).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(15_000)
    expect(auth.signerReady).toBe(false)
    expect(close).not.toHaveBeenCalled()

    await expect(login.signEvent({ kind: 9, created_at: 1, tags: [], content: 'retry' })).resolves.toBeTruthy()
    expect(close).toHaveBeenCalledTimes(1)
    expect(connect).toHaveBeenCalledTimes(2)
    expect(physicalSign).toHaveBeenCalledTimes(1)
    expect(auth.signerReady).toBe(true)

    resolveOld('late-ack')
    await Promise.resolve()
    await Promise.resolve()
    expect(auth.signerReady).toBe(true)
    expect(connect).toHaveBeenCalledTimes(2)
  })

  it('uses the same exact permission allowlist for bunker and nostrconnect flows', async () => {
    const { NostrConnectSigner } = await import('applesauce-signers')
    vi.spyOn(NostrConnectSigner.prototype, 'waitForSigner').mockReturnValue(new Promise(() => {}))
    const login = await import('./nostrLogin')

    expect(login.NIP46_SIGNING_KINDS).toEqual([
      0, 1, 5, 7, 9,
      9000, 9001, 9002, 9007, 9021, 9022,
      9734,
      20101, 20102, 20103, 20104,
      22242, 24242, 27235,
      30101, 30103, 30104, 30105, 30106, 30107,
    ])
    expect(login.NIP46_PERMISSIONS).toEqual([
      'get_public_key',
      ...login.NIP46_SIGNING_KINDS.map((kind) => `sign_event:${kind}`),
    ])
    expect(new Set(login.NIP46_SIGNING_KINDS).size).toBe(login.NIP46_SIGNING_KINDS.length)
    expect(login.NIP46_SIGNING_KINDS).toContain(22242)
    expect(login.NIP46_SIGNING_KINDS).not.toContain(20100)
    expect(login.NIP46_SIGNING_KINDS).not.toContain(30102)

    const { uri } = login.startNostrConnect()
    expect(new URL(uri).searchParams.get('perms')?.split(',')).toEqual(login.NIP46_PERMISSIONS)

    const fromBunker = vi.spyOn(NostrConnectSigner, 'fromBunkerURI')
      .mockRejectedValue(new Error('stop after options'))
    await expect(login.loginBunker('  bunker://example  ')).rejects.toThrow('stop after options')
    expect(fromBunker).toHaveBeenCalledWith('bunker://example', {
      permissions: [...login.NIP46_PERMISSIONS],
    })
  })
})

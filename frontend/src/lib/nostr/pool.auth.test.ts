// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { EventTemplate, VerifiedEvent } from 'nostr-tools/pure'

const AUTH_TEMPLATE: EventTemplate = { kind: 22242, created_at: 1, tags: [], content: '' }

const mocked = vi.hoisted(() => ({
  ensureRelay: vi.fn(),
  closePool: vi.fn(),
  relayClose: vi.fn(),
  originalAuth: vi.fn(),
}))

vi.mock('nostr-tools/pool', () => ({
  SimplePool: class {
    ensureRelay = mocked.ensureRelay
    close = mocked.closePool
  },
}))

function signedAuth(): VerifiedEvent {
  return {
    ...AUTH_TEMPLATE,
    pubkey: 'a'.repeat(64),
    id: 'auth',
    sig: '0'.repeat(128),
  } as VerifiedEvent
}

beforeEach(() => {
  vi.resetModules()
  vi.useRealTimers()
  mocked.ensureRelay.mockReset()
  mocked.closePool.mockReset()
  mocked.relayClose.mockReset()
  mocked.originalAuth.mockReset()
})

async function safeRelay() {
  const relay = { auth: mocked.originalAuth, close: mocked.relayClose }
  mocked.ensureRelay.mockResolvedValue(relay)
  const module = await import('./pool')
  const failure = vi.fn()
  module.setClubAuthFailureHandler(failure)
  const patched = await module.pool.ensureRelay(module.CLUB_RELAY)
  return { relay: patched, failure, module }
}

describe('safe club relay AUTH adapter', () => {
  it('shares one AUTH flight and reports a relay ACK rejection', async () => {
    let rejectAck!: (error: Error) => void
    const ack = new Promise<string>((_resolve, reject) => { rejectAck = reject })
    mocked.originalAuth.mockImplementation(async (signer) => {
      await signer(AUTH_TEMPLATE)
      return ack
    })
    const { relay, failure } = await safeRelay()
    const signer = vi.fn().mockResolvedValue(signedAuth())

    const first = relay.auth(signer)
    const second = relay.auth(signer)
    rejectAck(new Error('auth rejected by relay'))

    await expect(first).rejects.toThrow('auth rejected by relay')
    await expect(second).rejects.toThrow('auth rejected by relay')
    expect(mocked.originalAuth).toHaveBeenCalledTimes(1)
    expect(signer).toHaveBeenCalledTimes(1)
    expect(mocked.relayClose).toHaveBeenCalledTimes(1)
    expect(failure).toHaveBeenCalledTimes(1)
  })

  it('surfaces a swallowed signer rejection to every waiter', async () => {
    mocked.originalAuth.mockImplementation((signer) => {
      void signer(AUTH_TEMPLATE).catch(() => {})
      return new Promise(() => {})
    })
    const { relay, failure } = await safeRelay()
    const signer = vi.fn().mockRejectedValue(new Error('user denied'))

    await expect(relay.auth(signer)).rejects.toThrow('user denied')
    expect(mocked.relayClose).toHaveBeenCalledTimes(1)
    expect(failure).toHaveBeenCalledTimes(1)
  })

  it('bounds a missing AUTH acknowledgement', async () => {
    vi.useFakeTimers()
    mocked.originalAuth.mockReturnValue(new Promise(() => {}))
    const { relay, failure } = await safeRelay()
    const result = relay.auth(vi.fn().mockResolvedValue(signedAuth()))
    const rejection = expect(result).rejects.toThrow('AUTH acknowledgement: timeout')

    await vi.advanceTimersByTimeAsync(20_000)
    await rejection
    expect(mocked.relayClose).toHaveBeenCalledTimes(1)
    expect(failure).toHaveBeenCalledTimes(1)
  })

  it('rejects AUTH on a stale closed relay without calling its original AUTH', async () => {
    const { relay, module } = await safeRelay()
    const signer = vi.fn().mockResolvedValue(signedAuth())

    module.closeClubRelay()

    await expect(relay.auth(signer)).rejects.toThrow('connection was replaced')
    expect(mocked.originalAuth).not.toHaveBeenCalled()
    expect(signer).not.toHaveBeenCalled()
  })

  it('ignores a stale failure after a fresh relay generation has succeeded', async () => {
    let rejectOld!: (error: Error) => void
    const oldAck = new Promise<string>((_resolve, reject) => { rejectOld = reject })
    const oldRelay = {
      auth: vi.fn(async (signer) => {
        await signer(AUTH_TEMPLATE)
        return oldAck
      }),
      close: vi.fn(),
    }
    const freshRelay = {
      auth: vi.fn(async (signer) => {
        await signer(AUTH_TEMPLATE)
        return 'ok'
      }),
      close: vi.fn(),
    }
    mocked.ensureRelay.mockResolvedValueOnce(oldRelay).mockResolvedValueOnce(freshRelay)
    const module = await import('./pool')
    const failure = vi.fn()
    module.setClubAuthFailureHandler(failure)
    const old = await module.pool.ensureRelay(module.CLUB_RELAY)
    const stale = old.auth(vi.fn().mockResolvedValue(signedAuth()))

    module.closeClubRelay()
    const fresh = await module.pool.ensureRelay(module.CLUB_RELAY)
    await expect(fresh.auth(vi.fn().mockResolvedValue(signedAuth()))).resolves.toBe('ok')
    rejectOld(new Error('late old rejection'))

    await expect(stale).rejects.toThrow('late old rejection')
    expect(failure).not.toHaveBeenCalled()
    expect(oldRelay.close).toHaveBeenCalledTimes(1)
    expect(freshRelay.close).not.toHaveBeenCalled()
  })
})

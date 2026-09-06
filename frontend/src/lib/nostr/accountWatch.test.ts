// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const USER_PUBKEY = 'a'.repeat(64)
const OTHER_PUBKEY = 'b'.repeat(64)

function setDocumentHidden(hidden: boolean): void {
  Object.defineProperty(document, 'hidden', { value: hidden, configurable: true })
  Object.defineProperty(document, 'visibilityState', {
    value: hidden ? 'hidden' : 'visible',
    configurable: true,
  })
}

async function flushPromises(): Promise<void> {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
}

beforeEach(() => {
  vi.resetModules()
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-09-06T12:00:00Z'))
  setDocumentHidden(false)
  Reflect.deleteProperty(window, 'nostr')
})

afterEach(() => {
  vi.useRealTimers()
  Reflect.deleteProperty(window, 'nostr')
  setDocumentHidden(false)
})

describe('accountWatch', () => {
  it('coalesces focus signals and stays idle until focus or visibility changes', async () => {
    let resolveInitial!: (pubkey: string) => void
    const initial = new Promise<string>((resolve) => {
      resolveInitial = resolve
    })
    const getPublicKey = vi.fn()
      .mockImplementationOnce(() => initial)
      .mockResolvedValue(OTHER_PUBKEY)
    Object.defineProperty(window, 'nostr', {
      value: { getPublicKey },
      configurable: true,
      writable: true,
    })

    const { setLoggedIn } = await import('./auth.svelte')
    setLoggedIn(USER_PUBKEY, 'extension')
    const diagnostics = await import('./signingDiagnostics')
    diagnostics.setSigningDiagnosticsEnabled(true)
    const watch = await import('./accountWatch.svelte')

    watch.startAccountWatch()
    expect(getPublicKey).toHaveBeenCalledTimes(1)

    // Browsers commonly emit both signals for the same return to the tab. While the
    // provider request is pending they share one promise instead of reading the key again.
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getPublicKey).toHaveBeenCalledTimes(1)

    // Model a provider prompt that keeps the tab away for a while. Its completion starts
    // a fresh cooldown, so the focus emitted while closing the prompt is still coalesced.
    vi.advanceTimersByTime(10_000)
    resolveInitial(USER_PUBKEY)
    await flushPromises()
    expect(watch.accountWatch.mismatch).toBe(false)

    // A second focus burst inside the completion-based cooldown is ignored.
    window.dispatchEvent(new Event('focus'))
    expect(getPublicKey).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(watch.ACCOUNT_WATCH_COOLDOWN_MS)
    window.dispatchEvent(new Event('focus'))
    expect(getPublicKey).toHaveBeenCalledTimes(2)
    await flushPromises()
    expect(watch.accountWatch.mismatch).toBe(true)

    // No fallback poll: an idle foreground tab never calls the extension again.
    vi.advanceTimersByTime(300_000)
    expect(getPublicKey).toHaveBeenCalledTimes(2)

    setDocumentHidden(true)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getPublicKey).toHaveBeenCalledTimes(2)

    setDocumentHidden(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getPublicKey).toHaveBeenCalledTimes(3)
    await flushPromises()

    expect(diagnostics.signingDiagnosticsSnapshot().nip07GetPublicKeyCalls).toContainEqual({
      source: 'accountWatch',
      count: 3,
    })

    watch.stopAccountWatch()
    vi.advanceTimersByTime(300_000)
    window.dispatchEvent(new Event('focus'))
    expect(getPublicKey).toHaveBeenCalledTimes(3)
  })
})

import { auth } from './auth.svelte'
import { recordNip07GetPublicKeyCall } from './signingDiagnostics'

// Detects when the browser's Nostr extension (NIP-07) has switched to a DIFFERENT account than
// the one zapclub is logged in as. When that happens EVERY write fails ("Extension returned an
// invalid event") because the extension signs with the wrong key — silent and confusing. We
// surface a banner so the user can re-login as the extension's current account.

interface NostrExt {
  getPublicKey?: () => Promise<string>
}

const state = $state<{ mismatch: boolean }>({ mismatch: false })

export const accountWatch = {
  get mismatch() {
    return state.mismatch
  },
}

export const ACCOUNT_WATCH_COOLDOWN_MS = 5_000

let inFlight: Promise<void> | null = null
let lastCheckAt: number | null = null
let lastCheckedPubkey: string | null = null
let watchGeneration = 0

function check(force = false): Promise<void> {
  if (inFlight) return inFlight
  const now = Date.now()
  if (
    !force
    && lastCheckAt !== null
    && lastCheckedPubkey === auth.pubkey
    && now - lastCheckAt < ACCOUNT_WATCH_COOLDOWN_MS
  ) {
    return Promise.resolve()
  }

  const me = auth.pubkey
  const ext = (typeof window !== 'undefined' ? (window as unknown as { nostr?: NostrExt }).nostr : null) ?? null
  // Only meaningful for an extension login with a window.nostr that can report its key.
  if (!me || auth.method !== 'extension' || !ext?.getPublicKey) {
    state.mismatch = false
    return Promise.resolve()
  }

  lastCheckAt = now
  lastCheckedPubkey = me
  recordNip07GetPublicKeyCall('accountWatch')
  const generation = watchGeneration
  const request = (async () => {
    try {
      const current = await ext.getPublicKey!()
      // Ignore a result belonging to an account that stopped being active while the
      // extension dialog was open.
      if (watchGeneration === generation && auth.pubkey === me && auth.method === 'extension') {
        state.mismatch = !!current && current !== me
      }
    } catch {
      if (watchGeneration === generation && auth.pubkey === me && auth.method === 'extension') {
        state.mismatch = false
      }
    }
  })()
  inFlight = request
  void request.finally(() => {
    if (inFlight === request) {
      inFlight = null
      // Closing a provider prompt commonly focuses the window. Start the cooldown when
      // the provider finishes as well, so that focus signal does not immediately ask again.
      if (watchGeneration === generation) lastCheckAt = Date.now()
    }
  })
  return request
}

/** Reuses loginExtension's successful key read as the watcher's latest account check. */
export function handoffSuccessfulNip07Login(pubkey: string): void {
  watchGeneration++
  inFlight = null
  lastCheckedPubkey = pubkey
  lastCheckAt = Date.now()
  state.mismatch = false
}

/** Invalidates account-bound checks and clears a stale mismatch at logout/account switch. */
export function resetAccountWatchState(): void {
  watchGeneration++
  inFlight = null
  lastCheckAt = null
  lastCheckedPubkey = null
  state.mismatch = false
}

let started = false

function onFocus(): void {
  void check()
}

function onVisibilityChange(): void {
  if (!document.hidden) void check()
}

/** Start watching for an extension/app account mismatch. Switching an extension account moves
 * focus away from the page, so focus/visibility are sufficient without polling the provider. */
export function startAccountWatch(): void {
  if (started || typeof window === 'undefined') return
  started = true
  void check(true)
  window.addEventListener('focus', onFocus)
  document.addEventListener('visibilitychange', onVisibilityChange)
}

/** Symmetric cleanup for tests, app teardown and future remounts. */
export function stopAccountWatch(): void {
  if (started) {
    started = false
    window.removeEventListener('focus', onFocus)
    document.removeEventListener('visibilitychange', onVisibilityChange)
  }
  resetAccountWatchState()
}

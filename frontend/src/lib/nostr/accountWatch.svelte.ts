import { auth } from './auth.svelte'

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

async function check(): Promise<void> {
  const me = auth.pubkey
  const ext = (typeof window !== 'undefined' ? (window as unknown as { nostr?: NostrExt }).nostr : null) ?? null
  // Only meaningful for an extension login with a window.nostr that can report its key.
  if (!me || auth.method !== 'extension' || !ext?.getPublicKey) {
    state.mismatch = false
    return
  }
  try {
    const current = await ext.getPublicKey()
    state.mismatch = !!current && current !== me
  } catch {
    state.mismatch = false
  }
}

let started = false

/** Start watching for an extension/app account mismatch (poll + on focus/visibility). */
export function startAccountWatch(): void {
  if (started || typeof window === 'undefined') return
  started = true
  void check()
  setInterval(() => void check(), 8000)
  window.addEventListener('focus', () => void check())
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) void check()
  })
}

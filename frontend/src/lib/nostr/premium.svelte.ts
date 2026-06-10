import { CLUB_RELAY, CLUB_RELAY_PUBKEY, pool } from './pool'
import { auth } from './auth.svelte'

const KIND_PREMIUM = 30108
const RELAY_HTTP = 'https://relay.zapclub.io'
const NWC_KEY = 'zapclub:nwc'
const RENEW_WINDOW_S = 3 * 24 * 60 * 60 // 3 days before expiry

// Cache: pubkey → { until: unix-sec, fetchedAt: ms }
const cache = new Map<string, { until: number; fetchedAt: number }>()
const CACHE_TTL_MS = 60_000 // 1 minute

/** Returns the premium subscription expiry (unix seconds) for pubkey, or 0. */
export async function premiumUntil(pubkey: string): Promise<number> {
  const cached = cache.get(pubkey)
  if (cached && Date.now() - cached.fetchedAt < CACHE_TTL_MS) {
    return cached.until
  }
  let until = 0
  try {
    const res = await fetch(`${RELAY_HTTP}/premium/check?pubkey=${pubkey}`)
    if (res.ok) {
      const data = (await res.json()) as { premium: boolean; until: number }
      until = data.until ?? 0
    }
  } catch {
    // network error — treat as not premium
  }
  cache.set(pubkey, { until, fetchedAt: Date.now() })
  return until
}

/** Returns true when pubkey has an active premium subscription right now. */
export async function isPremium(pubkey: string): Promise<boolean> {
  const until = await premiumUntil(pubkey)
  return until > Math.floor(Date.now() / 1000)
}

/** Clears the cache entry for pubkey — call after a grant event is received. */
export function invalidatePremiumCache(pubkey: string): void {
  cache.delete(pubkey)
}

/** Force-refreshes ownPremium state from the HTTP endpoint (call after confirmed payment). */
export async function refreshOwnPremium(): Promise<void> {
  const pk = auth.pubkey
  if (!pk) return
  invalidatePremiumCache(pk)
  const until = await premiumUntil(pk)
  _ownPremiumUntil = until
  _ownPremium = until > Math.floor(Date.now() / 1000)
}

// ── Reactive own-premium state (for the logged-in user) ─────────────────────

let _ownPremium = $state(false)
let _ownPremiumUntil = $state(0)
let _subActive = false

/** Reactive: true when the logged-in user has an active premium subscription. */
export const ownPremium = {
  get active() {
    return _ownPremium
  },
  get until() {
    return _ownPremiumUntil
  },
}

/** Start a live subscription for the logged-in user's 30108. Call once after login. */
export function watchOwnPremium(): () => void {
  const pk = auth.pubkey
  if (!pk || _subActive) return () => {}
  _subActive = true

  // Initial fetch
  void isPremium(pk).then((v) => {
    _ownPremium = v
    void premiumUntil(pk).then((u) => {
      _ownPremiumUntil = u
      // Auto-renew check: if expiry is within RENEW_WINDOW_S and NWC is configured, renew.
      if (u > 0 && u - Math.floor(Date.now() / 1000) < RENEW_WINDOW_S) {
        void tryNwcRenew(pk)
      }
    })
  })

  // Live sub: when the relay writes a fresh 30108 for us, update immediately
  const sub = pool.subscribeMany(
    [CLUB_RELAY],
    [{ kinds: [KIND_PREMIUM], '#d': [pk] }],
    {
      onevent(ev) {
        if (ev.pubkey !== CLUB_RELAY_PUBKEY) return
        const t = ev.tags.find((t) => t[0] === 'premium_until')
        const until = t?.[1] ? parseInt(t[1], 10) || 0 : 0
        cache.set(pk, { until, fetchedAt: Date.now() })
        _ownPremiumUntil = until
        _ownPremium = until > Math.floor(Date.now() / 1000)
      },
    }
  )

  return () => {
    _subActive = false
    sub.close()
  }
}

// ── Payment flow ─────────────────────────────────────────────────────────────

/** Fetches a new premium invoice from the relay. Returns { bolt11, hash }. */
export async function fetchPremiumInvoice(): Promise<{ bolt11: string; hash: string }> {
  const pk = auth.pubkey
  if (!pk) throw new Error('Not signed in')

  // NIP-98 Authorization: sign a kind-27235 event for the POST request
  const url = `${RELAY_HTTP}/premium/invoice`
  const { signEvent } = await import('./nostrLogin')
  const authEvent = {
    kind: 27235,
    created_at: Math.floor(Date.now() / 1000),
    tags: [
      ['u', url],
      ['method', 'POST'],
    ],
    content: '',
    pubkey: pk,
  }
  const signed = await signEvent(authEvent)

  const res = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Nostr ${btoa(JSON.stringify(signed))}`,
    },
  })
  if (!res.ok) throw new Error(`Invoice request failed: ${res.status}`)
  return res.json() as Promise<{ bolt11: string; hash: string }>
}

/** Polls the relay until the invoice with `hash` is paid. Resolves to true on payment. */
export async function pollPremiumPaid(hash: string, isActive: () => boolean): Promise<boolean> {
  for (let i = 0; i < 60; i++) {
    if (!isActive()) return false
    await new Promise((r) => setTimeout(r, 3000))
    if (!isActive()) return false
    try {
      const res = await fetch(`${RELAY_HTTP}/premium/status?hash=${hash}`)
      if (res.ok) {
        const data = (await res.json()) as { paid: boolean }
        if (data.paid) return true
      }
    } catch {
      // network error — keep polling
    }
  }
  return false
}

// ── NWC auto-renew ───────────────────────────────────────────────────────────

/** Saves a NWC connection string (nostr+walletconnect://...) for this user. */
export function saveNwcConnection(connStr: string): void {
  try {
    localStorage.setItem(NWC_KEY, connStr)
  } catch { /* ignore */ }
}

/** Removes the stored NWC connection. */
export function clearNwcConnection(): void {
  try {
    localStorage.removeItem(NWC_KEY)
  } catch { /* ignore */ }
}

/** Returns the stored NWC connection string, or null. */
export function loadNwcConnection(): string | null {
  try {
    return localStorage.getItem(NWC_KEY)
  } catch { return null }
}

/**
 * Try auto-renewing premium via NWC if a connection is stored.
 * Only renews when expiry is within the grace window.
 * Silently no-ops if NWC is not configured or payment fails.
 */
async function tryNwcRenew(pubkey: string): Promise<void> {
  const connStr = loadNwcConnection()
  if (!connStr) return
  try {
    const { NWCClient } = await import('@getalby/sdk/nwc')
    const client = new NWCClient({ nostrWalletConnectUrl: connStr })
    const { bolt11, hash } = await fetchPremiumInvoice()
    await client.payInvoice({ invoice: bolt11 })
    client.close()
    // Relay will detect payment and write 30108 — our live sub picks it up.
    // Optimistically start polling to confirm, invalidate cache on success.
    for (let i = 0; i < 10; i++) {
      await new Promise((r) => setTimeout(r, 3000))
      const res = await fetch(`${RELAY_HTTP}/premium/status?hash=${hash}`)
      if (res.ok) {
        const data = (await res.json()) as { paid: boolean }
        if (data.paid) {
          invalidatePremiumCache(pubkey)
          break
        }
      }
    }
  } catch (e) {
    console.warn('NWC auto-renew failed:', e)
  }
}

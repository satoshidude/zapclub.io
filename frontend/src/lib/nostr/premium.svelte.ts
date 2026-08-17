import { CLUB_RELAY, CLUB_RELAY_PUBKEY, pool, PROFILE_RELAYS } from './pool'
import { auth } from './auth.svelte'
import { signEvent, nip44EncryptSelf, nip44DecryptSelf } from './nostrLogin'

const KIND_PREMIUM = 30108
const RELAY_HTTP = 'https://relay.zapclub.io'
const nwcKey = (pubkey: string) => `zapclub:nwc:${pubkey}`
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

const KIND_NWC_STORE = 30078
const NWC_DTAG = 'zapclub:nwc'

/**
 * Saves a NWC connection string for the current user.
 * Writes to localStorage immediately, then publishes a NIP-44 self-encrypted
 * kind-30078 event to public relays so the connection syncs to other devices.
 */
export async function saveNwcConnection(connStr: string): Promise<void> {
  if (!auth.pubkey) return
  try { localStorage.setItem(nwcKey(auth.pubkey), connStr) } catch { /* ignore */ }
  try {
    const encrypted = await nip44EncryptSelf(connStr)
    const ev = await signEvent({
      kind: KIND_NWC_STORE,
      created_at: Math.floor(Date.now() / 1000),
      tags: [['d', NWC_DTAG]],
      content: encrypted,
    })
    await Promise.allSettled(pool.publish(PROFILE_RELAYS, ev))
  } catch (e) {
    console.warn('[nwc] nostr sync failed (local save kept):', e)
  }
}

/**
 * Removes the stored NWC connection for the current user.
 * Clears localStorage and publishes an empty replacement event to public relays.
 */
export async function clearNwcConnection(): Promise<void> {
  if (!auth.pubkey) return
  try { localStorage.removeItem(nwcKey(auth.pubkey)) } catch { /* ignore */ }
  try {
    const ev = await signEvent({
      kind: KIND_NWC_STORE,
      created_at: Math.floor(Date.now() / 1000),
      tags: [['d', NWC_DTAG]],
      content: '',
    })
    await Promise.allSettled(pool.publish(PROFILE_RELAYS, ev))
  } catch (e) {
    console.warn('[nwc] nostr clear failed (local cleared):', e)
  }
}

/** Returns the stored NWC connection string for the current user, or null. */
export function loadNwcConnection(): string | null {
  if (!auth.pubkey) return null
  try {
    return localStorage.getItem(nwcKey(auth.pubkey))
  } catch { return null }
}

/**
 * Syncs NWC from Nostr (kind 30078) to localStorage on a new device.
 * Only fetches if no local NWC is stored. Returns true if a connection was found and saved.
 */
export async function syncNwcFromNostr(): Promise<boolean> {
  const pk = auth.pubkey
  if (!pk) return false
  if (loadNwcConnection()) return false // already have it locally
  try {
    const ev = await pool.get(
      PROFILE_RELAYS,
      { kinds: [KIND_NWC_STORE], authors: [pk], '#d': [NWC_DTAG] },
      { maxWait: 5000 },
    )
    if (!ev?.content) return false
    const plaintext = await nip44DecryptSelf(ev.content)
    if (!plaintext.startsWith('nostr+walletconnect://')) return false
    try { localStorage.setItem(nwcKey(pk), plaintext) } catch { /* ignore */ }
    return true
  } catch (e) {
    console.warn('[nwc] sync from nostr failed:', e)
    return false
  }
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

/**
 * Pay a bolt11 invoice silently in the background via the stored NWC connection.
 * NIP-47: `amount` is only for zero-amount invoices — do NOT pass it for fixed-amount
 * invoices or many wallets will reject the payment.
 */
export async function payViaBackground(bolt11: string): Promise<void> {
  const connStr = loadNwcConnection()
  if (!connStr) throw new Error('No NWC connection stored')
  const { NWCClient } = await import('@getalby/sdk/nwc')
  const client = new NWCClient({ nostrWalletConnectUrl: connStr })
  try {
    await client.payInvoice({ invoice: bolt11 })
  } finally {
    client.close()
  }
}

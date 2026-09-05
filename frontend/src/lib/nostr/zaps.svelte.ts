import type { Event } from 'nostr-tools/pure'
import { finalizeEvent, generateSecretKey } from 'nostr-tools/pure'
import { pool, ZAP_RELAYS } from './pool'
import { signEvent } from './nostrLogin'
import { auth } from './auth.svelte'

const KIND_ZAP_RECEIPT = 9735

interface ZapState {
  /** Sats per DJ pubkey (running session) — voting becomes economic. */
  scoreByDj: Record<string, number>
  /** Last incoming zap — triggers the animation. */
  lastZap: { dj: string; sats: number; at: number } | null
}
const state = $state<ZapState>({ scoreByDj: {}, lastZap: null })

export const zaps = {
  get scoreByDj() {
    return state.scoreByDj
  },
  get lastZap() {
    return state.lastZap
  },
  score(dj: string): number {
    return state.scoreByDj[dj] ?? 0
  },
}

function nowSec(): number {
  return Math.floor(Date.now() / 1000)
}

// A guest (no signer) zaps anonymously. We sign the 9734 with a STABLE per-browser ephemeral
// key persisted here — so a guest's repeat zaps share ONE anonymous identity instead of a fresh
// random npub every time (which cluttered recipients' "received" lists). It's a throwaway,
// non-user key by design; storing it in localStorage is fine.
const GUEST_KEY_LS = 'zapclub:guestZapKey'
let guestSk: Uint8Array | null = null
function guestZapKey(): Uint8Array {
  if (guestSk) return guestSk
  try {
    const stored = localStorage.getItem(GUEST_KEY_LS)
    const arr = stored ? (JSON.parse(stored) as number[]) : null
    if (Array.isArray(arr) && arr.length === 32) return (guestSk = Uint8Array.from(arr))
  } catch {
    /* ignore — fall through to a fresh key */
  }
  guestSk = generateSecretKey()
  try {
    localStorage.setItem(GUEST_KEY_LS, JSON.stringify(Array.from(guestSk)))
  } catch {
    /* ignore */
  }
  return guestSk
}

interface LnurlPay {
  callback: string
  allowsNostr?: boolean
  nostrPubkey?: string
  minSendable?: number
  maxSendable?: number
}

// nsnip.io's LNURL discovery response currently omits CORS headers. Zapclub already exposes
// the same nsnip-backed discovery endpoint with CORS enabled, so browser clients must use it
// for nsnip addresses. The returned LNbits callback has its own CORS support and stays intact.
export function lnurlPayEndpoint(lud16: string): string {
  const at = lud16.indexOf('@')
  if (at < 1) throw new Error('Invalid lightning address')
  const name = lud16.slice(0, at)
  const domain = lud16.slice(at + 1).toLowerCase()
  if (!domain) throw new Error('Invalid lightning address')
  const discoveryDomain = domain === 'nsnip.io' ? 'zapclub.io' : domain
  return `https://${discoveryDomain}/.well-known/lnurlp/${encodeURIComponent(name)}`
}

// fetch with a hard timeout — a hung LNURL host must not wedge the zap UI forever.
async function fetchTimeout(url: string, ms = 9000): Promise<Response> {
  const ctrl = new AbortController()
  const t = setTimeout(() => ctrl.abort(), ms)
  try {
    return await fetch(url, { signal: ctrl.signal })
  } finally {
    clearTimeout(t)
  }
}

/** Resolves a lightning address (lud16) to its LNURL-pay parameters. */
async function lnurlPayData(lud16: string): Promise<LnurlPay> {
  const res = await fetchTimeout(lnurlPayEndpoint(lud16))
  if (!res.ok) throw new Error('Could not reach the lightning address')
  const j = (await res.json()) as LnurlPay & { tag?: string }
  if (!j.callback) throw new Error('Not a valid LNURL-pay endpoint')
  return j
}

/** Resolves a lightning address to its NIP-57 zapper pubkey (nostrPubkey), or '' if the
 *  endpoint doesn't support nostr zaps. Stored in a paid club's config so the relay can later
 *  verify entry receipts (it can't do the HTTP LNURL lookup itself). */
export async function resolveZapper(lud16: string): Promise<string> {
  try {
    const d = await lnurlPayData(lud16)
    return d.allowsNostr && d.nostrPubkey ? d.nostrPubkey : ''
  } catch {
    return ''
  }
}

export interface ZapInvoice {
  invoice: string // bolt11
  verify?: string // LUD-21 verify URL (to detect external payment)
  request?: Event // signed 9734 used to attribute this Zapclub zap after payment
}

/**
 * Builds a zap invoice for a recipient (NIP-57). Signs a kind-9734 zap request (so the
 * payment produces a 9735 receipt) when the LNURL server supports nostr; otherwise it requests a
 * plain LNURL payment. Its signed request also attributes the Zapclub-local history. Returns the bolt11 invoice to pay —
 * by any wallet (Alby Go via the lightning: link, copy, or QR).
 *
 * A logged-in user signs with their own key (attributable zap). A GUEST (no signer / read-only)
 * zaps ANONYMOUSLY with a throwaway ephemeral key — still a valid NIP-57 zap that pays the DJ
 * and yields a 9735, so guests can support DJs on stage too (no login prompt).
 */
export async function requestZapInvoice(
  recipientPubkey: string,
  lud16: string,
  sats: number,
  comment: string,
): Promise<ZapInvoice> {
  const data = await lnurlPayData(lud16)
  const msats = sats * 1000
  if (data.minSendable && msats < data.minSendable) {
    throw new Error(`Minimum is ${Math.ceil(data.minSendable / 1000)} sats`)
  }
  if (data.maxSendable && msats > data.maxSendable) {
    throw new Error(`Maximum is ${Math.floor(data.maxSendable / 1000)} sats`)
  }

  const url = new URL(data.callback)
  url.searchParams.set('amount', String(msats))
  // recipientPubkey === '' → a plain LNURL payment (e.g. a donation), no zap request.
  let request: Event | undefined
  if (recipientPubkey) {
    const tags: string[][] = [
      ['relays', ...ZAP_RELAYS],
      ['amount', String(msats)],
      ['p', recipientPubkey],
      ['client', 'zapclub.io'],
    ]
    if (!auth.canSign) tags.push(['anon']) // mark a guest zap anonymous → shown as "Anonymous"
    const req = { kind: 9734, created_at: nowSec(), tags, content: comment || '' }
    // Logged-in → sign with the user's key; guest → the stable per-browser anonymous key.
    request = auth.canSign ? await signEvent(req) : finalizeEvent(req, guestZapKey())
    if (data.allowsNostr) url.searchParams.set('nostr', JSON.stringify(request))
    else if (comment) url.searchParams.set('comment', comment.slice(0, 120))
  } else if (comment) {
    url.searchParams.set('comment', comment.slice(0, 120))
  }

  const res = await fetchTimeout(url.toString())
  const json = (await res.json()) as { pr?: string; verify?: string; reason?: string }
  if (!json.pr) throw new Error(json.reason || 'No invoice received')
  return { invoice: json.pr, verify: json.verify, request }
}

/**
 * Builds a club ENTRY invoice (NIP-57). Like requestZapInvoice but the signed 9734 carries
 * the club tags the relay's entry gate verifies: `h`=club and `club_entry`=club (so a normal
 * track-zap receipt can't be reused as entry), plus `p`=the club's entry zapper pubkey. The
 * resulting 9735 receipt (published by the LNURL server) is the proof attached to the 9021.
 */
export async function requestEntryInvoice(
  clubId: string,
  zapper: string,
  lud16: string,
  sats: number,
): Promise<ZapInvoice> {
  const data = await lnurlPayData(lud16)
  const msats = sats * 1000
  if (data.minSendable && msats < data.minSendable) {
    throw new Error(`Entry minimum is ${Math.ceil(data.minSendable / 1000)} sats`)
  }
  if (!data.allowsNostr || !zapper) {
    throw new Error("This club's entry address doesn't support Nostr zaps")
  }
  const zr = await signEvent({
    kind: 9734,
    created_at: nowSec(),
    tags: [
      ['relays', ...ZAP_RELAYS],
      ['amount', String(msats)],
      ['p', zapper],
      ['h', clubId],
      ['club_entry', clubId],
    ],
    content: 'club entry',
  })
  const url = new URL(data.callback)
  url.searchParams.set('amount', String(msats))
  url.searchParams.set('nostr', JSON.stringify(zr))
  const res = await fetchTimeout(url.toString())
  const json = (await res.json()) as { pr?: string; verify?: string; reason?: string }
  if (!json.pr) throw new Error(json.reason || 'No invoice received')
  return { invoice: json.pr, verify: json.verify }
}

/**
 * Waits for the 9735 receipt of `invoice` (published by the LNURL server to ZAP_RELAYS) and
 * returns the full event — the entry proof to attach to the 9021 join. Null on timeout.
 */
export function captureEntryReceipt(invoice: string, zapper: string, timeoutMs = 180_000): Promise<Event | null> {
  return new Promise((resolve) => {
    let done = false
    const fin = (ev: Event | null) => {
      if (done) return
      done = true
      try {
        sub.close()
      } catch {
        /* ignore */
      }
      clearTimeout(t)
      resolve(ev)
    }
    const sub = pool.subscribe(
      ZAP_RELAYS,
      { kinds: [KIND_ZAP_RECEIPT], '#p': [zapper] },
      {
        onevent(ev) {
          if (ev.tags.find((t) => t[0] === 'bolt11')?.[1] === invoice) fin(ev)
        },
      },
    )
    const t = setTimeout(() => fin(null), timeoutMs)
  })
}

/**
 * Polls a LUD-21 verify URL until the invoice is paid (or timeout). Lets us detect an
 * EXTERNAL payment (QR scan / Alby Go) and close the pay modal. Resolves true if paid.
 */
export async function pollPaid(verifyUrl: string, stillOpen: () => boolean): Promise<boolean> {
  for (let i = 0; i < 90; i++) {
    await new Promise((r) => setTimeout(r, 2000))
    if (!stillOpen()) return false
    try {
      const r = await fetch(verifyUrl)
      const j = (await r.json()) as { settled?: boolean; paid?: boolean }
      if (j.settled || j.paid) return true
    } catch {
      /* transient — keep polling */
    }
  }
  return false
}

const seen = new Set<string>()
// bolt11 invoices already counted — so an optimistic local credit and the matching club
// broadcast for the same zap don't double-count.
const creditedInvoices = new Set<string>()

function applyZap(dj: string, sats: number): void {
  state.scoreByDj[dj] = (state.scoreByDj[dj] ?? 0) + sats
  state.lastZap = { dj, sats, at: Date.now() }
}

/**
 * Credits a confirmed zap locally without waiting for the club broadcast echo. Idempotent
 * per invoice, so the echo cannot double-count it. Lets the zapper see their zap immediately.
 */
export function creditZap(dj: string, sats: number, invoice?: string): void {
  if (!dj || sats <= 0) return
  if (invoice) {
    if (creditedInvoices.has(invoice)) return
    creditedInvoices.add(invoice)
  }
  applyZap(dj, sats)
}

/**
 * Watches for the 9735 zap RECEIPT of a specific invoice and fires onPaid when it lands.
 * This is the only automatic payment signal when the LNURL server provides no LUD-21 verify
 * URL (e.g. nsnip.io and many LNURL providers) — the receipt the server publishes on payment doubles as
 * the "paid" confirmation. Matches by bolt11 (exact). Returns a close function.
 */
export function watchInvoicePaid(
  invoice: string,
  recipientPubkey: string,
  onPaid: () => void,
): () => void {
  if (!invoice || !recipientPubkey) return () => {}
  const sub = pool.subscribe(
    ZAP_RELAYS,
    { kinds: [KIND_ZAP_RECEIPT], '#p': [recipientPubkey] },
    {
      onevent(ev) {
        if (ev.tags.find((t) => t[0] === 'bolt11')?.[1] === invoice) onPaid()
      },
    },
  )
  return () => sub.close()
}

/** Handles an incoming club zap broadcast (kind 20101) → animation + session score.
 *  See publishZapBroadcast in groups.ts for the why (LNURL providers that don't publish a
 *  9735). `bolt11` dedup keeps the broadcast and the zapper's local credit from counting
 *  the same zap twice. */
export function ingestZapBroadcast(ev: Event): void {
  if (seen.has(ev.id)) return
  seen.add(ev.id)
  const dj = ev.tags.find((t) => t[0] === 'p')?.[1]
  const sats = Math.round(Number(ev.tags.find((t) => t[0] === 'amount')?.[1]))
  if (!dj || !sats || sats <= 0) return
  const inv = ev.tags.find((t) => t[0] === 'bolt11')?.[1]
  if (inv) {
    // Dedup against the local credit when the zapper receives their own broadcast echo.
    if (creditedInvoices.has(inv)) return
    creditedInvoices.add(inv)
  }
  applyZap(dj, sats)
}

export function resetZaps(): void {
  state.scoreByDj = {}
  state.lastZap = null
  seen.clear()
  creditedInvoices.clear()
}

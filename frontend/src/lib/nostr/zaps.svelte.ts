import type { Event } from 'nostr-tools/pure'
import { verifyEvent } from 'nostr-tools/pure'
import { pool, ZAP_RELAYS } from './pool'
import { signEvent } from './nostrLogin'

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

interface LnurlPay {
  callback: string
  allowsNostr?: boolean
  nostrPubkey?: string
  minSendable?: number
  maxSendable?: number
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
  const at = lud16.indexOf('@')
  if (at < 1) throw new Error('Invalid lightning address')
  const name = lud16.slice(0, at)
  const domain = lud16.slice(at + 1)
  const res = await fetchTimeout(`https://${domain}/.well-known/lnurlp/${name}`)
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
}

/**
 * Builds a zap invoice for a recipient (NIP-57). Signs a kind-9734 zap request (so the
 * payment is attributable to the DJ and produces a 9735 receipt) when the LNURL server
 * supports nostr; otherwise a plain LNURL payment. Returns the bolt11 invoice to pay —
 * by any wallet (Alby Go via the lightning: link, copy, or QR).
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
  if (data.allowsNostr && recipientPubkey) {
    const zr = await signEvent({
      kind: 9734,
      created_at: nowSec(),
      tags: [
        ['relays', ...ZAP_RELAYS],
        ['amount', String(msats)],
        ['p', recipientPubkey],
      ],
      content: comment || '',
    })
    url.searchParams.set('nostr', JSON.stringify(zr))
  } else if (comment) {
    url.searchParams.set('comment', comment.slice(0, 120))
  }

  const res = await fetchTimeout(url.toString())
  const json = (await res.json()) as { pr?: string; verify?: string; reason?: string }
  if (!json.pr) throw new Error(json.reason || 'No invoice received')
  return { invoice: json.pr, verify: json.verify }
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

// Parses a 9735 zap receipt to {recipient, sender, sats}, verifying the embedded 9734
// request. sender = the zap-request author (the person who zapped).
function parseReceiptDetail(ev: Event): { recipient: string; sender: string; sats: number } | null {
  const recipient = ev.tags.find((t) => t[0] === 'p')?.[1]
  const desc = ev.tags.find((t) => t[0] === 'description')?.[1]
  if (!recipient || !desc) return null
  let req: Event
  try {
    req = JSON.parse(desc) as Event
  } catch {
    return null
  }
  if (req.kind !== 9734 || !verifyEvent(req)) return null
  if (req.tags.find((t) => t[0] === 'p')?.[1] !== recipient) return null
  const amountTag = req.tags.find((t) => t[0] === 'amount')?.[1]
  const sats = amountTag ? Math.round(Number(amountTag) / 1000) : 0
  if (!sats || sats <= 0) return null
  return { recipient, sender: req.pubkey, sats }
}

function parseReceipt(ev: Event): { dj: string; sats: number } | null {
  const d = parseReceiptDetail(ev)
  return d ? { dj: d.recipient, sats: d.sats } : null
}

export interface ReceivedZaps {
  total: number
  count: number
  bySender: { sender: string; sats: number; count: number }[]
}

/** Aggregates all zaps a user has RECEIVED (9735 with #p = pubkey), grouped by sender. */
export async function fetchReceivedZaps(pubkey: string): Promise<ReceivedZaps> {
  const evs = await pool.querySync(
    ZAP_RELAYS,
    { kinds: [KIND_ZAP_RECEIPT], '#p': [pubkey] },
    { maxWait: 5000 },
  )
  const map = new Map<string, { sats: number; count: number }>()
  let total = 0
  let count = 0
  const dedup = new Set<string>()
  for (const ev of evs) {
    if (dedup.has(ev.id)) continue
    dedup.add(ev.id)
    const d = parseReceiptDetail(ev)
    if (!d || d.recipient !== pubkey) continue
    total += d.sats
    count++
    const cur = map.get(d.sender) ?? { sats: 0, count: 0 }
    cur.sats += d.sats
    cur.count++
    map.set(d.sender, cur)
  }
  const bySender = [...map.entries()]
    .map(([sender, v]) => ({ sender, ...v }))
    .sort((a, b) => b.sats - a.sats)
  return { total, count, bySender }
}

const seen = new Set<string>()
// bolt11 invoices already counted — so an optimistic local credit and the later 9735
// receipt for the SAME zap don't double-count (and vice versa).
const creditedInvoices = new Set<string>()

function applyZap(dj: string, sats: number): void {
  state.scoreByDj[dj] = (state.scoreByDj[dj] ?? 0) + sats
  state.lastZap = { dj, sats, at: Date.now() }
}

export function ingestZapReceipt(ev: Event): void {
  if (seen.has(ev.id)) return
  seen.add(ev.id)
  const r = parseReceipt(ev)
  if (!r) return
  const inv = ev.tags.find((t) => t[0] === 'bolt11')?.[1]
  if (inv) {
    if (creditedInvoices.has(inv)) return // already credited optimistically
    creditedInvoices.add(inv)
  }
  applyZap(r.dj, r.sats)
}

/**
 * Optimistically credits a confirmed zap locally, without waiting for the 9735 receipt
 * (which is slow/unreliable on public relays). Idempotent per invoice, so the receipt —
 * if it ever lands — won't double-count. Lets the zapper see their zap immediately.
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
 * URL (e.g. nsnip.io / many LNbits) — the receipt the server publishes on payment doubles as
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

/** Subscribes to zap receipts (9735) for the stage DJs on the public relays. */
export function subscribeZaps(djPubkeys: string[]): () => void {
  if (djPubkeys.length === 0) return () => {}
  const sub = pool.subscribe(
    ZAP_RELAYS,
    { kinds: [KIND_ZAP_RECEIPT], '#p': djPubkeys },
    { onevent: ingestZapReceipt },
  )
  return () => sub.close()
}

export function resetZaps(): void {
  state.scoreByDj = {}
  state.lastZap = null
  seen.clear()
  creditedInvoices.clear()
}

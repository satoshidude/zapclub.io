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

/** Resolves a lightning address (lud16) to its LNURL-pay parameters. */
async function lnurlPayData(lud16: string): Promise<LnurlPay> {
  const at = lud16.indexOf('@')
  if (at < 1) throw new Error('Invalid lightning address')
  const name = lud16.slice(0, at)
  const domain = lud16.slice(at + 1)
  const res = await fetch(`https://${domain}/.well-known/lnurlp/${name}`)
  if (!res.ok) throw new Error('Could not reach the lightning address')
  const j = (await res.json()) as LnurlPay & { tag?: string }
  if (!j.callback) throw new Error('Not a valid LNURL-pay endpoint')
  return j
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

  const res = await fetch(url.toString())
  const json = (await res.json()) as { pr?: string; verify?: string; reason?: string }
  if (!json.pr) throw new Error(json.reason || 'No invoice received')
  return { invoice: json.pr, verify: json.verify }
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

function parseReceipt(ev: Event): { dj: string; sats: number } | null {
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
  return { dj: recipient, sats }
}

const seen = new Set<string>()
export function ingestZapReceipt(ev: Event): void {
  if (seen.has(ev.id)) return
  seen.add(ev.id)
  const r = parseReceipt(ev)
  if (!r) return
  state.scoreByDj[r.dj] = (state.scoreByDj[r.dj] ?? 0) + r.sats
  state.lastZap = { dj: r.dj, sats: r.sats, at: Date.now() }
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
}

// Global public ranking from the relay's aggregate endpoint. Building the candidate
// set from kind 39002 would expose the protected club membership roster.

import type { Event } from 'nostr-tools/pure'
import { nip98Header } from './nip98'

export interface LeaderboardEntry {
  pubkey: string
  sats: number
  zaps: number
  zappers: number // distinct people who zapped this DJ
  rank: number
}

export interface ZapRank {
  rank: number
  total: number // ranked DJs
  sats: number
  zaps: number
  zappers: number
}

let cache: { at: number; promise: Promise<{ total: number; top: LeaderboardEntry[] }> } | null = null
const TTL_MS = 60_000
const LEADERBOARD_URL = 'https://relay.zapclub.io/leaderboard'
const ZAPS_URL = 'https://relay.zapclub.io/zaps'

export interface ReceivedZaps {
  total: number
  count: number
  bySender: { sender: string; sats: number; count: number; exact: boolean; anon: boolean }[]
}

/** The public ranking of DJs by sats received (only DJs who HAVE been zapped). Cached ~60s. */
export function fetchLeaderboard(): Promise<{ total: number; top: LeaderboardEntry[] }> {
  if (cache && Date.now() - cache.at < TTL_MS) return cache.promise
  const promise = build()
  cache = { at: Date.now(), promise }
  return promise
}

async function build(): Promise<{ total: number; top: LeaderboardEntry[] }> {
  try {
    const response = await fetch(LEADERBOARD_URL)
    if (!response.ok) throw new Error(`leaderboard: ${response.status}`)
    return (await response.json()) as { total: number; top: LeaderboardEntry[] }
  } catch {
    return { total: 0, top: [] }
  }
}

/** A DJ's global zap placement + totals, or null if they've not been zapped (not on the board). */
export async function fetchZapRank(pubkey: string): Promise<ZapRank | null> {
  try {
    const response = await fetch(`${LEADERBOARD_URL}?pubkey=${encodeURIComponent(pubkey)}`)
    if (!response.ok) return null
    const result = (await response.json()) as ZapRank & { ranked: boolean }
    if (!result.ranked) return null
    return { rank: result.rank, total: result.total, sats: result.sats, zaps: result.zaps, zappers: result.zappers }
  } catch {
    return null
  }
}

/** Records a payment confirmed inside Zapclub using its already-signed NIP-57 request. */
export async function recordZap(request: Event, invoice: string): Promise<void> {
  const response = await fetch(ZAPS_URL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ request, invoice }),
  })
  if (!response.ok) throw new Error(`record zap: ${response.status}`)
}

/** Returns only Zapclub-recorded zaps received by the NIP-98 signer. */
export async function fetchReceivedZaps(): Promise<ReceivedZaps> {
  const url = `${ZAPS_URL}/received`
  const response = await fetch(url, {
    headers: { Authorization: await nip98Header(url, 'GET') },
  })
  if (!response.ok) throw new Error(`received zaps: ${response.status}`)
  return response.json() as Promise<ReceivedZaps>
}

import { fetchClubPeople } from './groups'
import { aggregateReceived } from './zaps.svelte'

// Global zap leaderboard, built CLIENT-SIDE from verified 9735 receipts — that's where the
// historical zap data actually lives (the relay's 20101 board only accrues going forward and
// would start empty). We take every DJ (club member/owner), aggregate the zaps they've received,
// keep those with sats > 0, and rank by sats. Cached briefly so the leaderboard page, the home
// hero teaser, and profile rank badges share one fetch.

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

/** The public ranking of DJs by sats received (only DJs who HAVE been zapped). Cached ~60s. */
export function fetchLeaderboard(): Promise<{ total: number; top: LeaderboardEntry[] }> {
  if (cache && Date.now() - cache.at < TTL_MS) return cache.promise
  const promise = build()
  cache = { at: Date.now(), promise }
  return promise
}

async function build(): Promise<{ total: number; top: LeaderboardEntry[] }> {
  try {
    const people = await fetchClubPeople()
    const totals = await aggregateReceived(people)
    const top = [...totals.entries()]
      .map(([pubkey, t]) => ({ pubkey, sats: t.sats, zaps: t.zaps, zappers: t.zappers, rank: 0 }))
      .filter((e) => e.sats > 0)
      .sort((a, b) => b.sats - a.sats || a.pubkey.localeCompare(b.pubkey))
    top.forEach((e, i) => (e.rank = i + 1))
    return { total: top.length, top }
  } catch {
    return { total: 0, top: [] }
  }
}

/** A DJ's global zap placement + totals, or null if they've not been zapped (not on the board). */
export async function fetchZapRank(pubkey: string): Promise<ZapRank | null> {
  const { total, top } = await fetchLeaderboard()
  const e = top.find((x) => x.pubkey === pubkey)
  if (!e) return null
  return { rank: e.rank, total, sats: e.sats, zaps: e.zaps, zappers: e.zappers }
}

// Global public ranking from the relay's aggregate endpoint. Building the candidate
// set from kind 39002 would expose the protected club membership roster.

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

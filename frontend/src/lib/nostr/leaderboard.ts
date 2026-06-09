// Global zap leaderboard — the relay aggregates kind-20101 zap broadcasts into an all-time,
// per-recipient board and serves it at GET /leaderboard (see relay/leaderboard.go). Public,
// unauthenticated, CORS-open. We use it for the rank + headline numbers on a user's profile
// (total sats received, how many people zapped, global placement). The detailed "who zapped"
// breakdown stays client-side from verified 9735 receipts (zaps.svelte.ts) and is owner-only.

const LEADERBOARD_BASE = 'https://relay.zapclub.io'

export interface ZapRank {
  rank: number
  total: number // participants on the board
  sats: number
  zaps: number
  zappers: number // distinct people who zapped this user
}

export interface LeaderboardEntry {
  pubkey: string
  sats: number
  zaps: number
  zappers: number
  rank: number
}

/** The public top-N ranking of DJs by sats received, plus the total number of ranked users. */
export async function fetchLeaderboard(): Promise<{ total: number; top: LeaderboardEntry[] }> {
  try {
    const res = await fetch(`${LEADERBOARD_BASE}/leaderboard`)
    if (!res.ok) return { total: 0, top: [] }
    const j = (await res.json()) as { total?: number; top?: LeaderboardEntry[] }
    return { total: j.total ?? 0, top: Array.isArray(j.top) ? j.top : [] }
  } catch {
    return { total: 0, top: [] }
  }
}

/** A user's global zap placement + totals, or null if they're not on the board (no zaps yet). */
export async function fetchZapRank(pubkey: string): Promise<ZapRank | null> {
  try {
    const res = await fetch(`${LEADERBOARD_BASE}/leaderboard?pubkey=${encodeURIComponent(pubkey)}`)
    if (!res.ok) return null
    const j = (await res.json()) as {
      ranked?: boolean
      rank?: number
      total?: number
      sats?: number
      zaps?: number
      zappers?: number
    }
    if (!j?.ranked) return null
    return {
      rank: j.rank ?? 0,
      total: j.total ?? 0,
      sats: j.sats ?? 0,
      zaps: j.zaps ?? 0,
      zappers: j.zappers ?? 0,
    }
  } catch {
    return null // relay unreachable / dev CORS — the profile just hides the rank
  }
}

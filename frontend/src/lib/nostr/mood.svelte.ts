import type { Event } from 'nostr-tools/pure'
import { publishClub, KIND_MOOD } from './groups'
import { auth } from './auth.svelte'

export type MoodVote = 'banger' | 'skip'

// Per-pubkey votes for each (club, pos) — only fresh events (after last heartbeat) live here.
const voteMap = $state<Record<string, Record<number, Record<string, MoodVote>>>>({})

// Authoritative baseline from the conductor's now_playing heartbeat.
// sentAt (ms) marks what the conductor had already counted — we skip ephemeral events ≤ sentAt.
const baseline = $state<Record<string, { b: number; s: number; sentAt: number }>>({})

// Own vote stored separately so it survives baseline clears.
const ownVotes = $state<Record<string, MoodVote>>({})

// Track which (club:pos) combos have already fired the banger threshold — one fireworks per track.
const bangerFired = new Set<string>()

const THRESHOLD = 3

export const vibeMeter = {
  bangerCount(club: string, pos: number): number {
    const key = `${club}:${pos}`
    const base = baseline[key]?.b ?? 0
    const fresh = Object.values(voteMap[club]?.[pos] ?? {}).filter((v) => v === 'banger').length
    return base + fresh
  },
  skipCount(club: string, pos: number): number {
    const key = `${club}:${pos}`
    const base = baseline[key]?.s ?? 0
    const fresh = Object.values(voteMap[club]?.[pos] ?? {}).filter((v) => v === 'skip').length
    return base + fresh
  },
  ownVote(club: string, pos: number): MoodVote | null {
    if (!auth.pubkey) return null
    return ownVotes[`${club}:${pos}`] ?? null
  },
  /** Returns true once when banger threshold is first crossed for this track; resets on track change. */
  checkBanger(club: string, pos: number): boolean {
    const key = `${club}:${pos}`
    if (bangerFired.has(key)) return false
    if (vibeMeter.bangerCount(club, pos) >= THRESHOLD) {
      bangerFired.add(key)
      return true
    }
    return false
  },
}

/**
 * Seed vote counts from the conductor's now_playing heartbeat (kind 30100).
 * Called every ~15 s so late-joining users immediately see the current state.
 * Clears stale ephemeral entries for this track; fresh events (after sentAt) accumulate on top.
 */
export function setMoodBaseline(
  club: string,
  pos: number,
  bangers: number,
  skips: number,
  sentAt: number,
): void {
  const key = `${club}:${pos}`
  baseline[key] = { b: bangers, s: skips, sentAt }
  // Drop the ephemeral voteMap for this pos — baseline already includes everything
  // the conductor has seen. Fresh ephemeral events (created_at > sentAt) will re-populate it.
  if (voteMap[club]) delete voteMap[club][pos]
}

/** Ingest a mood vote event (kind 20104). Only the latest vote per (club, pos, pubkey) counts. */
export function ingestMood(ev: Event): void {
  const club = ev.tags.find((t) => t[0] === 'h')?.[1]
  const posStr = ev.tags.find((t) => t[0] === 'pos')?.[1]
  const vote = ev.tags.find((t) => t[0] === 'v')?.[1] as MoodVote | undefined
  if (!club || posStr === undefined || (vote !== 'banger' && vote !== 'skip')) return
  const pos = parseInt(posStr, 10)
  if (isNaN(pos) || pos < 0) return

  const key = `${club}:${pos}`
  const base = baseline[key]
  // Skip events the conductor has already counted in the baseline.
  if (base && ev.created_at * 1000 <= base.sentAt) return

  if (!voteMap[club]) voteMap[club] = {}
  if (!voteMap[club][pos]) voteMap[club][pos] = {}
  voteMap[club][pos][ev.pubkey] = vote
}

/** Optimistic local update — applied immediately on click so the UI responds before relay echo. */
export function optimisticVote(club: string, pos: number, pubkey: string, vote: MoodVote): void {
  // Track own vote in a separate map so it survives baseline clears.
  if (pubkey === auth.pubkey) ownVotes[`${club}:${pos}`] = vote
  // Also add to voteMap as a fresh entry so bangerCount/skipCount see it immediately.
  if (!voteMap[club]) voteMap[club] = {}
  if (!voteMap[club][pos]) voteMap[club][pos] = {}
  voteMap[club][pos][pubkey] = vote
}

/** Publish a mood vote (kind 20104, ephemeral). */
export async function sendMood(clubId: string, pos: number, vote: MoodVote): Promise<void> {
  await publishClub({
    kind: KIND_MOOD,
    created_at: Math.floor(Date.now() / 1000),
    tags: [
      ['h', clubId],
      ['pos', String(pos)],
      ['v', vote],
    ],
    content: '',
  })
}

export function resetMood(): void {
  for (const k in voteMap) delete voteMap[k]
  for (const k in baseline) delete baseline[k]
  for (const k in ownVotes) delete ownVotes[k]
  bangerFired.clear()
}

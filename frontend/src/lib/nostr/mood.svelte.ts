import type { Event } from 'nostr-tools/pure'
import { publishClub, KIND_MOOD } from './groups'
import { auth } from './auth.svelte'

export type MoodVote = 'banger' | 'skip'

// Per-pubkey reaction counts for each (club, pos). The relay accepts at most one reaction per
// account every 10 seconds and caps the room totals at five bangers / three skips.
const voteMap = $state<Record<string, Record<number, Record<string, { b: number; s: number }>>>>({})

// Authoritative baseline from the conductor's now_playing heartbeat.
// sentAt (ms) marks what the conductor had already counted — we skip ephemeral events ≤ sentAt.
const baseline = $state<Record<string, { b: number; s: number; sentAt: number }>>({})

// Own vote stored separately so it survives baseline clears (used for button highlight).
const ownVotes = $state<Record<string, MoodVote>>({})
const ownVoteAt = $state<Record<string, number>>({})
const clock = $state({ now: Date.now() })

// Dedup relay echoes of own events — own votes are counted optimistically, so we skip
// the relay echo to avoid double-counting.
const seenMoodIds = new Set<string>()

// Track which (club:pos) combos have already fired the banger threshold — one fireworks per track.
const bangerFired = new Set<string>()

export const SKIP_THRESHOLD = 3
export const BANGER_MAX = 5
export const VOTE_COOLDOWN_MS = 10_000

if (typeof setInterval !== 'undefined') {
  setInterval(() => { clock.now = Date.now() }, 250)
}

export const vibeMeter = {
  bangerCount(club: string, pos: number): number {
    const base = baseline[`${club}:${pos}`]?.b ?? 0
    const fresh = Object.values(voteMap[club]?.[pos] ?? {}).reduce((s, e) => s + e.b, 0)
    return Math.min(BANGER_MAX, base + fresh)
  },
  skipCount(club: string, pos: number): number {
    const base = baseline[`${club}:${pos}`]?.s ?? 0
    const fresh = Object.values(voteMap[club]?.[pos] ?? {}).reduce((s, e) => s + e.s, 0)
    return Math.min(SKIP_THRESHOLD, base + fresh)
  },
  ownVote(club: string, pos: number): MoodVote | null {
    if (!auth.pubkey) return null
    return ownVotes[`${club}:${pos}`] ?? null
  },
  cooldownSeconds(): number {
    if (!auth.pubkey) return 0
    const remaining = (ownVoteAt[auth.pubkey] ?? 0) + VOTE_COOLDOWN_MS - clock.now
    return Math.max(0, Math.ceil(remaining / 1000))
  },
  /** Returns true once when banger threshold is first crossed for this track; resets on track change. */
  checkBanger(club: string, pos: number): boolean {
    const key = `${club}:${pos}`
    if (bangerFired.has(key)) return false
    if (vibeMeter.bangerCount(club, pos) >= BANGER_MAX) {
      bangerFired.add(key)
      return true
    }
    return false
  },
}

/**
 * Seed vote counts from the conductor's now_playing heartbeat (kind 30100).
 * Called every ~15 s so late-joining users immediately see the current state.
 * Clears stale ephemeral entries and keeps an own optimistic reaction only when it is newer
 * than the conductor snapshot, so a heartbeat neither drops nor double-counts it.
 */
export function setMoodBaseline(
  club: string,
  pos: number,
  bangers: number,
  skips: number,
  sentAt: number,
): void {
  const key = `${club}:${pos}`
  baseline[key] = {
    b: Math.min(BANGER_MAX, Math.max(0, bangers)),
    s: Math.min(SKIP_THRESHOLD, Math.max(0, skips)),
    sentAt,
  }
  // Preserve only an own reaction newer than this authoritative heartbeat. Once the relay's
  // sentAt passes it, retaining the optimistic copy would double-count it.
  const myPk = auth.pubkey
  const ownKey = myPk ?? ''
  const keepOwn = !!ownKey && (ownVoteAt[ownKey] ?? 0) > sentAt
  const myVotes = keepOwn && myPk ? (voteMap[club]?.[pos]?.[myPk] ?? { b: 0, s: 0 }) : { b: 0, s: 0 }
  if (voteMap[club]) delete voteMap[club][pos]
  if ((myVotes.b > 0 || myVotes.s > 0) && myPk) {
    if (!voteMap[club]) voteMap[club] = {}
    voteMap[club][pos] = { [myPk]: myVotes }
  }
}

/** Ingest a mood reaction event (kind 20104). */
export function ingestMood(ev: Event): void {
  if (seenMoodIds.has(ev.id)) return
  seenMoodIds.add(ev.id)

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

  // Own events are already counted optimistically — skip relay echo to avoid double-counting.
  if (ev.pubkey === auth.pubkey) return

  if (!voteMap[club]) voteMap[club] = {}
  if (!voteMap[club][pos]) voteMap[club][pos] = {}
  const cur = voteMap[club][pos][ev.pubkey] ?? { b: 0, s: 0 }
  if (vote === 'banger') {
    if (vibeMeter.bangerCount(club, pos) < BANGER_MAX) {
      voteMap[club][pos][ev.pubkey] = { ...cur, b: cur.b + 1 }
    }
  } else {
    if (vibeMeter.skipCount(club, pos) < SKIP_THRESHOLD) {
      voteMap[club][pos][ev.pubkey] = { ...cur, s: cur.s + 1 }
    }
  }
}

/** Optimistic local update — applied immediately on click so the UI responds before relay echo. */
export function optimisticVote(club: string, pos: number, pubkey: string, vote: MoodVote): void {
  if (pubkey === auth.pubkey) {
    ownVotes[`${club}:${pos}`] = vote
    ownVoteAt[pubkey] = Date.now()
  }
  if (!voteMap[club]) voteMap[club] = {}
  if (!voteMap[club][pos]) voteMap[club][pos] = {}
  const cur = voteMap[club][pos][pubkey] ?? { b: 0, s: 0 }
  if (vote === 'banger') {
    if (vibeMeter.bangerCount(club, pos) < BANGER_MAX) {
      voteMap[club][pos][pubkey] = { ...cur, b: cur.b + 1 }
    }
  } else {
    if (vibeMeter.skipCount(club, pos) < SKIP_THRESHOLD) {
      voteMap[club][pos][pubkey] = { ...cur, s: cur.s + 1 }
    }
  }
}

/** Publish a Vibemeter reaction (kind 20104, ephemeral). */
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
  for (const k in ownVoteAt) delete ownVoteAt[k]
  seenMoodIds.clear()
  bangerFired.clear()
}

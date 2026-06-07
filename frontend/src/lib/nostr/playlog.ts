// Pure round-robin progress logic (no Nostr/state/clock) — easy to test.
//
// The round-robin's "already played" state used to live ONLY in a per-conductor-client,
// ephemeral Set. When the conductor role moved (rescue / bootstrap / owner reclaim / a fresh
// club-view mount), the new conductor started with an empty set and replayed the tracks of
// AWAY DJs (whose `off` flag is never written, since only their own client writes their
// queue). The fix: derive the played state from the SHARED kind-1313 play-log the conductor
// already emits on every track start — so any conductor reconstructs the same progress.
//
// A "session" is one contiguous run of playback: consecutive plays whose start times are
// within SESSION_GAP_MS of each other. A larger gap means the room fell to the lobby (no
// conductor refreshed now_playing) → a new session. Within a session, a `loop` epoch lets
// the rotation reset (replay from the top) in a way every client agrees on: advance() bumps
// the epoch on exhaustion and the next play is tagged with it, so reconstruction only counts
// plays in the current (max) epoch.

import type { Event } from 'nostr-tools/pure'
import { isValidVideoId } from '../util'

export interface PlayRecord {
  videoId: string
  startedAt: number // ms (conductor clock)
  pos: number
  loop: number // rotation epoch (0 if untagged / legacy)
}

/** Parse a kind-1313 play record. Returns null for a malformed / invalid-id event. */
export function parsePlayRecord(ev: Event): PlayRecord | null {
  const videoId = ev.content
  if (!isValidVideoId(videoId)) return null
  const tag = (n: string) => ev.tags.find((t) => t[0] === n)?.[1]
  // started_at is published in ms; fall back to created_at (s→ms) for any legacy record.
  const startedAt = Number(tag('started_at')) || ev.created_at * 1000
  return {
    videoId,
    startedAt,
    pos: Number(tag('pos')) || 0,
    loop: Number(tag('loop')) || 0,
  }
}

/**
 * Slice `records` to the latest session: sort ascending by startedAt, then walk back from the
 * newest and cut at the first adjacent pair whose gap exceeds `sessionGapMs`. Everything from
 * the cut onward is "this session". Pure.
 */
export function latestSession(records: PlayRecord[], sessionGapMs: number): PlayRecord[] {
  if (records.length === 0) return []
  const sorted = [...records].sort((a, b) => a.startedAt - b.startedAt)
  let cut = 0
  for (let i = sorted.length - 1; i > 0; i--) {
    if (sorted[i].startedAt - sorted[i - 1].startedAt > sessionGapMs) {
      cut = i
      break
    }
  }
  return sorted.slice(cut)
}

/** Current loop epoch = max(loop) among the latest session's records (0 if none). Pure. */
export function currentLoop(records: PlayRecord[], sessionGapMs: number): number {
  return latestSession(records, sessionGapMs).reduce((m, r) => Math.max(m, r.loop), 0)
}

/**
 * Reconstruct the played-videoId set for the current session AND current loop epoch, minus
 * the currently-playing track (which must never exclude itself). Deterministic: same records
 * (any order) → same set + loop. Pure.
 */
export function reconstructPlayed(
  records: PlayRecord[],
  sessionGapMs: number,
  currentVideoId: string | null,
): { played: Set<string>; loop: number } {
  const session = latestSession(records, sessionGapMs)
  const loop = session.reduce((m, r) => Math.max(m, r.loop), 0)
  const played = new Set<string>()
  for (const r of session) {
    if (r.loop === loop && r.videoId !== currentVideoId) played.add(r.videoId)
  }
  return { played, loop }
}

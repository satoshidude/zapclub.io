import type { Event } from 'nostr-tools/pure'
import { KIND_SKIP, publishClub, reportBrokenTrack } from './groups'
import { auth } from './auth.svelte'
import { stage } from './stage.svelte'
import { queues } from './queue.svelte'
import { posToSlot, nextPlayablePos } from './roundrobin'
import { presence } from './presence.svelte'
import { sessionPlayed } from './playlog.svelte'
import { isValidVideoId } from '../util'
import type { NowPlaying } from './types'

// The RELAY is the conductor — it is the sole writer of now_playing (kind 30100). This module is
// now purely a CONSUMER: it ingests now_playing, drift-corrects the playback position, exposes
// the live track, previews "Up next" (read-only mirror of the relay's round-robin), and lets an
// owner/mod request a skip (kind 30107, the relay enacts + role-validates). No client conducting.

/** Listeners fall back to the lobby once now_playing has been silent this long (relay down /
 *  no DJ). Also the SINGLE source for the play-log's session-gap (playlog.svelte.ts): a gap
 *  larger than this == the room was in the lobby == a new playback session. */
export const LIVE_STALE_MS = 150_000

interface SyncState {
  np: NowPlaying | null
  /** offset = conductor (relay) clock − local clock (ms), recalibrated per event. */
  offsetMs: number
  /** Reactive clock (ms) so `live` re-evaluates staleness even when no events arrive. */
  now: number
}

const state = $state<SyncState>({ np: null, offsetMs: 0, now: Date.now() })

// Reactive clock: without incoming events, `live` must still flip to the lobby once the relay
// goes silent. A plain Date.now() in the getter wouldn't be reactive.
if (typeof setInterval !== 'undefined') {
  setInterval(() => {
    state.now = Date.now()
  }, 5000)
}

export const sync = {
  get nowPlaying() {
    return state.np
  },
  /**
   * What's actually playing: a now_playing only counts if someone is on stage AND it's been
   * refreshed recently. If the relay goes silent past LIVE_STALE_MS we return null → lobby track.
   */
  get live() {
    const np = state.np
    if (!np || stage.djs.length === 0) return null
    if (state.now - np.sentAt > LIVE_STALE_MS) return null
    return np
  },
  get isPlaying() {
    return state.np?.status === 'playing'
  },
}

function parseNowPlaying(ev: Event): NowPlaying | null {
  const tag = (n: string) => ev.tags.find((t) => t[0] === n)?.[1]
  const track = tag('track') ?? ''
  if (!track.startsWith('yt:')) return null
  const videoId = track.slice(3)
  if (!isValidVideoId(videoId)) return null // drop a foreign event with a malformed id
  return {
    videoId,
    startedAt: Number(tag('started_at')) || 0,
    sentAt: Number(tag('sent_at')) || 0,
    duration: Number(tag('duration')) || 0,
    status: tag('status') === 'paused' ? 'paused' : 'playing',
    dj: tag('dj') ?? ev.pubkey,
    pos: Number(tag('pos')) || 0,
    title: ev.content,
    writer: ev.pubkey,
  }
}

/** Accept an incoming now_playing (newest wins) + calibrate the clock offset. */
export function ingestNowPlaying(ev: Event): void {
  const np = parseNowPlaying(ev)
  if (!np) return
  if (state.np && np.sentAt < state.np.sentAt) return
  if (np.sentAt > 0) state.offsetMs = np.sentAt - Date.now()
  state.np = np
}

/** Current target position of the track in seconds (relay-clock calibrated). */
export function targetPosition(): number {
  const np = state.np
  if (!np) return 0
  const refMs = np.status === 'paused' ? np.sentAt : Date.now() + state.offsetMs
  return Math.max(0, (refMs - np.startedAt) / 1000)
}

// ── "Up next" preview (read-only mirror of the relay's round-robin) ──────────

/**
 * Playability matrix mirroring the relay's conductor (conductor.go matrix()): a slot is playable
 * if the track is active (`off`!==true), not the currently-playing one, and — for an AWAY DJ —
 * not already played this session. A PRESENT DJ's own `active`/`off` flags are authoritative (so
 * a reactivated track shows again); the shared played-set (from the 1313 log) only guards away
 * DJs whose client couldn't mark a played track `off`. Read-only — the relay drives the actual
 * rotation; this is just the preview, kept in lockstep with the relay's rule.
 */
function playableMatrix(djs: string[]): boolean[][] {
  const cur = state.np?.videoId
  const { played } = sessionPlayed(cur ?? null)
  return djs.map((pk) =>
    // Mirror the relay's matrix exactly (conductor.go): a track is playable if active, not the
    // running one, and not already played this session — for ALL DJs (the relay's played-set is
    // authoritative; `off` is just a manual disable). The session played-set comes from the 1313
    // play-log the relay writes.
    (queues.get(pk)?.tracks ?? []).map(
      (t) => t.active !== false && t.videoId !== cur && !played.has(t.videoId),
    ),
  )
}

/** Preview of the next round-robin tracks (across all DJs), max `max`. Scans FORWARD from the
 *  relay's current `pos` (matching the relay's `advance` — it goes forward, not from the top),
 *  excluding the running track. Falls back to the top only when nothing is playing yet. */
export function upcomingTracks(max = 5): { dj: string; videoId: string; title: string }[] {
  const djs = stage.djs.map((d) => d.pubkey)
  if (djs.length === 0) return []
  const playable = playableMatrix(djs)
  const out: { dj: string; videoId: string; title: string }[] = []
  let pos = state.np?.pos ?? -1
  for (let i = 0; i < max; i++) {
    const next = nextPlayablePos(pos, djs.length, playable)
    if (next === -1) break
    const { djIndex, trackIndex } = posToSlot(next, djs.length)
    const dj = djs[djIndex]
    const track = queues.trackAt(dj, trackIndex)
    if (track) out.push({ dj, videoId: track.videoId, title: track.title })
    pos = next
  }
  return out
}

// ── skip (owner/mod or the playing DJ → the relay enacts + role-validates) ───

/** Whether the local user may skip — an owner/moderator (the relay validates the role too). */
export function canSkip(canModerate = false): boolean {
  if (!auth.pubkey || !sync.live) return false
  // The currently-playing DJ may skip their own track; an owner/moderator may always skip.
  // (The relay validates the same authors on the 30107 skip-request.)
  return canModerate || sync.live.dj === auth.pubkey
}

/**
 * Request a skip of the current track (kind 30107). The relay is the conductor: it advances on a
 * matching skip-request only from an authorized author (owner/mod, or the playing DJ). Posting it
 * requires club membership (relay write-protection).
 */
export async function requestSkip(groupId: string): Promise<void> {
  if (!auth.pubkey || !state.np) return
  await publishClub({
    kind: KIND_SKIP,
    created_at: Math.floor(Date.now() / 1000),
    tags: [
      ['h', groupId],
      ['d', groupId],
      ['pos', String(state.np.pos)],
    ],
    content: '',
  })
}

/**
 * Track end reported by the local player. No-op: the relay advances by the track's duration on
 * its own clock (so playback continues even with no client present). Track end is not a client
 * decision.
 */
export function onTrackEnded(_groupId: string): void {
  /* relay-driven: nothing to do */
}

/**
 * Playback error reported by the player (deleted, region-locked, embedding off). The relay can't
 * detect this itself, so we report the track unplayable (kind 20102); the relay skips it when an
 * authorized user (owner/mod/playing-DJ) or a quorum of members reports it. Any member may report
 * — it's "I can't play this", not a moderation skip.
 */
export function onTrackError(groupId: string, videoId: string): void {
  if (!state.np || state.np.videoId !== videoId) return // stale error of an old track
  void reportBrokenTrack(groupId, videoId)
}

export function resetSync(): void {
  state.np = null
  state.offsetMs = 0
}

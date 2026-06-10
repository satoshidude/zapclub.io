import type { Event } from 'nostr-tools/pure'
import { KIND_SKIP, publishClub, reportBrokenTrack } from './groups'
import { auth } from './auth.svelte'
import { stage } from './stage.svelte'
import { queues } from './queue.svelte'
import { fairSequence } from './roundrobin'
import { presence } from './presence.svelte'
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
    if (!np) return null
    // Auto DJ plays with no real stage DJs — bypass the stage-empty check for it.
    if (!np.auto && stage.djs.length === 0) return null
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
    auto: tag('auto') === '1' || undefined,
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
 * if the track is `active` (NOT marked `off`) and not the currently-playing one. The DJ's QUEUE is
 * the single source of truth — a played track becomes `off` and drops out; a re-activated track
 * plays again; reorder changes the top. No hidden played-set on either side. Read-only preview.
 */
function playableMatrix(djs: string[]): boolean[][] {
  const cur = state.np?.videoId
  return djs.map((pk) =>
    (queues.get(pk)?.tracks ?? []).map((t) => t.active !== false && t.videoId !== cur),
  )
}

/** Preview of the next round-robin tracks (across all DJs), max `max`. Fair rotation starting
 *  after the currently-playing DJ — each DJ contributes its TOP PLAYABLE (active, not-off) track in
 *  turn, exactly like the relay's repeated `advance` (so a reorder is reflected immediately and the
 *  interleave alternates fairly per DJ regardless of where off tracks sit). Off tracks drop out. */
export function upcomingTracks(max = 5): { dj: string; videoId: string; title: string }[] {
  const djs = stage.djs.map((d) => d.pubkey)
  if (djs.length === 0) return []
  const playable = playableMatrix(djs)
  const lastDjIndex = djs.indexOf(state.np?.dj ?? '')
  const out: { dj: string; videoId: string; title: string }[] = []
  for (const { djIndex, trackIndex } of fairSequence(djs.length, playable, lastDjIndex, max)) {
    const dj = djs[djIndex]
    const track = queues.trackAt(dj, trackIndex)
    if (track) out.push({ dj, videoId: track.videoId, title: track.title })
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

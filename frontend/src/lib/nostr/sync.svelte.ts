import type { Event } from 'nostr-tools/pure'
import { KIND_NOW_PLAYING, KIND_SKIP, publishClub, publishPlay } from './groups'
import { auth } from './auth.svelte'
import { stage } from './stage.svelte'
import { queues } from './queue.svelte'
import { posToSlot, nextPlayablePos, firstPlayablePos, reanchoredPos } from './roundrobin'
import { isValidVideoId } from '../util'
import type { NowPlaying } from './types'

/** The conductor counts as down if the last now_playing is older (ms). Generous (45s),
 *  matching the stickier stage — brief stalls/background don't trigger an unnecessary
 *  conductor switch. */
const FAILOVER_MS = 45_000

interface SyncState {
  np: NowPlaying | null
  /** offset = conductor clock − local clock (ms), recalibrated per event. */
  offsetMs: number
  /** Latest skip request (from an owner/mod without the conductor role). */
  skipIntent: { pos: number; author: string; at: number } | null
}

const state = $state<SyncState>({ np: null, offsetMs: 0, skipIntent: null })

export const sync = {
  get nowPlaying() {
    return state.np
  },
  /**
   * What's actually playing: a now_playing only counts if someone is on stage (the
   * conductor drives the round-robin). Without active DJs at most a stale event lingers —
   * then "nothing plays" and the lobby track takes over.
   */
  get live() {
    return state.np && stage.djs.length > 0 ? state.np : null
  },
  get isPlaying() {
    return state.np?.status === 'playing'
  },
  get skipIntent() {
    return state.skipIntent
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

/** Current target position of the track in seconds (conductor-clock calibrated). */
export function targetPosition(): number {
  const np = state.np
  if (!np) return 0
  const refMs = np.status === 'paused' ? np.sentAt : Date.now() + state.offsetMs
  return Math.max(0, (refMs - np.startedAt) / 1000)
}

// ── Conductor (time authority, round-robin progress) ───────────────────────

/**
 * Publishes a CONCRETE track as now_playing (fresh sent_at). The source of truth for the
 * running track is this object — NOT pos+queue. That keeps the track stable no matter how
 * stage/queues change.
 */
function publishNp(groupId: string, np: NowPlaying): void {
  const sentAt = Date.now()
  state.np = { ...np, sentAt }
  state.offsetMs = 0
  void publishClub({
    kind: KIND_NOW_PLAYING,
    created_at: Math.floor(sentAt / 1000),
    tags: [
      ['h', groupId],
      ['d', groupId],
      ['track', `yt:${np.videoId}`],
      ['dj', np.dj ?? ''],
      ['pos', String(np.pos ?? 0)],
      ['started_at', String(np.startedAt)],
      ['sent_at', String(sentAt)],
      ['duration', String(np.duration)],
      ['status', np.status],
    ],
    content: np.title,
  })
}

/** Derives the concrete track from `pos` and starts it (a real track change). */
function startAt(groupId: string, pos: number, startedAtMs: number): void {
  const djs = stage.djs.map((d) => d.pubkey)
  if (djs.length === 0) return
  const { djIndex, trackIndex } = posToSlot(pos, djs.length)
  const dj = djs[djIndex]
  const track = queues.trackAt(dj, trackIndex)
  if (!track) return
  publishNp(groupId, {
    videoId: track.videoId,
    title: track.title,
    duration: track.duration,
    startedAt: startedAtMs,
    sentAt: Date.now(),
    status: 'playing',
    dj,
    pos,
  })
  // Countable play record (now_playing is replaceable, keeps no history).
  void publishPlay(groupId, dj, track.videoId, startedAtMs)
}

/**
 * Re-anchors `pos` to the index the CURRENTLY-PLAYING track now sits at in its DJ's queue.
 * The round-robin is positional (`pos`→`trackIndex`), so when a DJ reorders, the playing
 * track may move to a different index; without re-anchoring the next `advance()` would
 * follow stale positions (and a track moved before the play head would be skipped until a
 * wrap). We keep the playing track untouched — only correct `pos` — so reorders take
 * effect on the NEXT track, in the new order.
 */
function reanchorPos(np: NowPlaying): NowPlaying {
  const djs = stage.djs.map((d) => d.pubkey)
  const pos = reanchoredPos(djs, np.dj, np.pos, np.videoId, (dj) =>
    (queues.get(dj)?.tracks ?? []).map((t) => t.videoId),
  )
  return pos === np.pos ? np : { ...np, pos }
}

/** Heartbeat: re-send the same running track (only sent_at fresh), pos re-anchored. */
function heartbeat(groupId: string): void {
  if (!state.np) return
  state.np = reanchorPos(state.np)
  publishNp(groupId, state.np)
}

/** Advances to the next round-robin track (or loops). Only on end/skip. */
function advance(groupId: string, fromPos: number): void {
  const djs = stage.djs.map((d) => d.pubkey)
  // Honor a reorder made since the last heartbeat: re-anchor to the playing track's
  // current index, so the NEXT pick follows the new order even if the track ends before
  // the next heartbeat would have corrected pos.
  if (state.np) {
    const re = reanchorPos(state.np)
    state.np = re
    fromPos = re.pos
  }
  const playable = queues.playable(djs)
  let next = nextPlayablePos(fromPos, djs.length, playable)
  if (next === -1) next = firstPlayablePos(djs.length, playable) // loop
  if (next === -1) {
    state.np = null // no tracks available
    return
  }
  startAt(groupId, next, Date.now())
}

/**
 * Called periodically (by ClubView). Only the conductor acts: starts the first track,
 * keeps the heartbeat, takes over on failover.
 * IMPORTANT: the heartbeat leaves the running track untouched — stage/queue changes only
 * affect the NEXT track (on end or skip).
 */
export function conductorTick(groupId: string): void {
  const me = auth.pubkey
  if (!me || me !== stage.conductor) return // conductor only

  const djs = stage.djs.map((d) => d.pubkey)
  const playable = queues.playable(djs)
  const np = state.np
  const npFresh = !!np && Date.now() - np.sentAt < FAILOVER_MS

  if (!np) {
    const pos = firstPlayablePos(djs.length, playable)
    if (pos >= 0) startAt(groupId, pos, Date.now())
    return
  }

  if (!npFresh) {
    // Very old now_playing = residue of an earlier session (kind 30100 is replaceable and
    // lingers on the relay). Then `elapsed` against the ancient started_at is huge → would
    // be falsely treated as "finished" and the first track skipped. Instead re-start the
    // same pos (never skip).
    if (Date.now() - np.sentAt > 120_000) {
      const pos = firstPlayablePos(djs.length, playable)
      if (pos >= 0) startAt(groupId, pos, Date.now())
      else state.np = null
      return
    }
    // Real failover (conductor only briefly away): KEEP the running track; only advance if
    // it actually finished within this window.
    const elapsed = (Date.now() - np.startedAt) / 1000
    if (np.duration > 0 && elapsed >= np.duration) advance(groupId, np.pos)
    else heartbeat(groupId) // same track, new conductor takes over the heartbeat
    return
  }

  // Fresh & I'm conductor → heartbeat (track frozen, new sent_at).
  heartbeat(groupId)
}

/**
 * Conductor: immediately re-anchor pos to the running track and republish now_playing, so
 * a just-made reorder is pushed to everyone's Up next without waiting for the heartbeat.
 * No-op for a non-conductor (their republished queue reaches the conductor instead).
 */
export function applyOrderNow(groupId: string): void {
  const me = auth.pubkey
  if (!me || me !== stage.conductor || !state.np) return
  state.np = reanchorPos(state.np)
  publishNp(groupId, state.np)
}

/** Manual skip to the next track — ONLY the leading DJ (conductor). */
export function skipTrack(groupId: string): void {
  const me = auth.pubkey
  if (!me || me !== stage.conductor || !state.np) return
  advance(groupId, state.np.pos)
}

/**
 * Skip the current track. The conductor does it directly. An owner/moderator who is NOT
 * the conductor (maybe not even on stage) publishes a skip request that the conductor
 * enacts — only the conductor writes now_playing, so this is the safe path.
 */
export async function requestSkip(groupId: string): Promise<void> {
  const me = auth.pubkey
  if (!me || !state.np) return
  if (me === stage.conductor) {
    skipTrack(groupId)
    return
  }
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

/** A skip request seen on the relay (the conductor validates the author's role). */
export function ingestSkipIntent(ev: Event): void {
  const pos = Number(ev.tags.find((t) => t[0] === 'pos')?.[1])
  if (Number.isNaN(pos)) return
  const at = ev.created_at * 1000
  if (state.skipIntent && state.skipIntent.at >= at) return
  state.skipIntent = { pos, author: ev.pubkey, at }
}

export function clearSkipIntent(): void {
  state.skipIntent = null
}

/** Whether the local user may skip — the conductor, OR an owner/moderator. */
export function canSkip(canModerate = false): boolean {
  return !!auth.pubkey && !!sync.live && (auth.pubkey === stage.conductor || canModerate)
}

/** Preview of the next round-robin tracks (across all DJs), max `max`. */
export function upcomingTracks(max = 5): { dj: string; videoId: string; title: string }[] {
  const djs = stage.djs.map((d) => d.pubkey)
  if (djs.length === 0) return []
  const playable = queues.playable(djs)
  // Start from the re-anchored pos so a reorder shows in Up next immediately (before the
  // conductor's next heartbeat corrects the published pos).
  let pos = state.np ? reanchorPos(state.np).pos : -1
  const out: { dj: string; videoId: string; title: string }[] = []
  const seen = new Set<number>()
  if (state.np) seen.add(state.np.pos) // never list the running track as "next" (wrap)
  for (let i = 0; i < max; i++) {
    let next = nextPlayablePos(pos, djs.length, playable)
    // Like the real round-robin: loop at the end → DJs with shorter playlists (whose
    // positions are already past) reappear after the wrap instead of being missing.
    if (next === -1) next = firstPlayablePos(djs.length, playable)
    if (next === -1 || seen.has(next)) break // nothing playable / once fully around
    seen.add(next)
    const { djIndex, trackIndex } = posToSlot(next, djs.length)
    const dj = djs[djIndex]
    const track = queues.trackAt(dj, trackIndex)
    if (track) out.push({ dj, videoId: track.videoId, title: track.title })
    pos = next
  }
  return out
}

/** Track end reported by the player — only the conductor advances. */
export function onTrackEnded(groupId: string): void {
  const me = auth.pubkey
  if (!me || me !== stage.conductor || !state.np) return
  errorStreak = 0 // clean end → reset error streak
  advance(groupId, state.np.pos)
}

// Guard against an endless skip if (almost) all tracks are unplayable.
let errorWindowStart = 0
let errorStreak = 0

/**
 * Playback error reported by the player (deleted, region-locked, embedding off). Only the
 * conductor advances — otherwise the whole room would hang on the dead track. If errors
 * pile up (≥6 in 30s), the club falls back to the lobby track.
 */
export function onTrackError(groupId: string, videoId: string): void {
  const me = auth.pubkey
  if (!me || me !== stage.conductor || !state.np) return
  if (state.np.videoId !== videoId) return // stale error of an old track
  const nowMs = Date.now()
  if (nowMs - errorWindowStart > 30_000) {
    errorWindowStart = nowMs
    errorStreak = 0
  }
  errorStreak++
  if (errorStreak >= 6) {
    state.np = null // too many dead tracks → lobby takes over
    return
  }
  advance(groupId, state.np.pos)
}

export function resetSync(): void {
  state.np = null
  state.offsetMs = 0
  state.skipIntent = null
}

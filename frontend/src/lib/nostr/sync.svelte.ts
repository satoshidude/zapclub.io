import type { Event } from 'nostr-tools/pure'
import { KIND_NOW_PLAYING, KIND_SKIP, publishClub, publishPlay } from './groups'
import { auth } from './auth.svelte'
import { stage } from './stage.svelte'
import { queues } from './queue.svelte'
import { posToSlot, nextPlayablePos, firstPlayablePos, reanchoredPos } from './roundrobin'
import { shouldConduct } from './conductor'
import { presence } from './presence.svelte'
import { sessionPlayed } from './playlog.svelte'
import { isValidVideoId } from '../util'
import type { NowPlaying } from './types'

/** The conductor counts as down if the last now_playing is older (ms). Generous (45s),
 *  matching the stickier stage — brief stalls/background don't trigger an unnecessary
 *  conductor switch. */
const FAILOVER_MS = 45_000
/** A DIFFERENT present DJ rescues a silent conductor only after this much staleness — larger
 *  than a backgrounded tab's worst-case heartbeat gap (~60s) so we don't fight a merely
 *  backgrounded-but-alive conductor; far below the lobby fallback. */
const RESCUE_STALE_MS = 90_000
/** Listeners stop showing a track and fall back to the lobby once now_playing has been
 *  silent this long — a phantom conductor (navigated away while sticky-on-stage) must not
 *  freeze the room indefinitely. Beyond RESCUE_STALE_MS so a present DJ takes over first.
 *  Exported as the SINGLE source for the play-log's session-gap (playlog.svelte.ts): a gap
 *  larger than the lobby-fallback == the room was in the lobby == a new playback session. */
export const LIVE_STALE_MS = 150_000

interface SyncState {
  np: NowPlaying | null
  /** offset = conductor clock − local clock (ms), recalibrated per event. */
  offsetMs: number
  /** Latest skip request (from an owner/mod without the conductor role). */
  skipIntent: { pos: number; author: string; at: number } | null
  /** Reactive clock (ms) so `live` re-evaluates staleness even when no events arrive. */
  now: number
}

const state = $state<SyncState>({ np: null, offsetMs: 0, skipIntent: null, now: Date.now() })

// Reactive clock: without incoming events, `live` must still flip to the lobby once the
// conductor goes silent. A plain Date.now() in the getter wouldn't be reactive.
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
   * refreshed recently. A silent conductor (navigated away while sticky-on-stage) must not
   * keep the room frozen on a dead track — past LIVE_STALE_MS we return null → lobby track.
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
    writer: ev.pubkey, // who is actually driving — for phantom-conductor detection
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
  state.np = { ...np, sentAt, writer: auth.pubkey ?? np.writer }
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
  // Shared play record (now_playing is replaceable, keeps no history): the conductor-
  // independent source of round-robin progress, carrying pos + the current loop epoch.
  void publishPlay(groupId, dj, track.videoId, startedAtMs, pos, currentLoopEpoch)
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

// Tracks the conductor has already played THIS session (by videoId). Needed because a track's
// `off` flag is written by its own DJ — when that DJ is away (sticky on stage, client gone),
// the conductor plays their tracks but can't mark them off, so a top-down scan would keep
// re-picking the same just-played track → an endless 2-song skip loop. Excluding played
// videoIds breaks that; cleared on exhaustion (to loop the rotation) and on reset.
const playedThisSession = new Set<string>()

/** Rotation epoch. advance() bumps it on exhaustion so a replay is agreed across clients via
 *  the 1313 `loop` tag (see playlog.ts), instead of each conductor silently looping locally. */
let currentLoopEpoch = 0

/**
 * Merge the SHARED played-set (reconstructed from the 1313 play-log) into the local set, so a
 * fresh / rescuing / bootstrapping conductor continues exactly where the room left off instead
 * of replaying away DJs' tracks. Additive — never un-excludes what advance() has locally added.
 *  - log epoch > local → another conductor looped the rotation; adopt it and reset locally.
 *  - log epoch < local → we just bumped and our publishPlay hasn't echoed back yet; skip, our
 *    cleared local set is authoritative until the log catches up (avoids re-adding old plays).
 */
function seedPlayedFromLog(): void {
  const { played, loop } = sessionPlayed(state.np?.videoId ?? null)
  if (loop < currentLoopEpoch) return
  if (loop > currentLoopEpoch) {
    currentLoopEpoch = loop
    playedThisSession.clear()
  }
  for (const v of played) playedThisSession.add(v)
}

/**
 * Playability matrix: a slot is playable if the track is active (`off`!==true) AND not the
 * currently-playing one AND not already played this session. Built directly (queues.playable
 * returns fresh arrays per DJ).
 */
function playableExcluding(djs: string[]): boolean[][] {
  const excluded = new Set(playedThisSession)
  if (state.np?.videoId) excluded.add(state.np.videoId)
  return djs.map((pk) =>
    (queues.get(pk)?.tracks ?? []).map((t) => t.active !== false && !excluded.has(t.videoId)),
  )
}

/**
 * Advances to the next track. ALWAYS scans from the TOP: the next track is the first active
 * track per DJ in round-robin order, skipping `off`, the current track, AND anything already
 * played this session (so a skip never loops back to a just-played track even when its DJ is
 * away and never marked it off). When everything's been played, the session history is cleared
 * so the rotation loops; truly nothing playable → lobby.
 */
function advance(groupId: string): void {
  const djs = stage.djs.map((d) => d.pubkey)
  if (djs.length === 0) {
    state.np = null
    return
  }
  seedPlayedFromLog() // continue from the SHARED progress, not just this client's local set
  if (state.np?.videoId) playedThisSession.add(state.np.videoId)
  let next = firstPlayablePos(djs.length, playableExcluding(djs))
  if (next === -1) {
    // Rotation exhausted → bump the loop epoch (so all clients agree on the replay via the
    // 1313 `loop` tag) and forget history. The current track stays excluded so we don't
    // immediately replay it.
    currentLoopEpoch++
    playedThisSession.clear()
    next = firstPlayablePos(djs.length, playableExcluding(djs))
  }
  if (next === -1) {
    state.np = null // nothing active at all → lobby
    return
  }
  startAt(groupId, next, Date.now())
}

/**
 * Whether the local user should DRIVE playback right now — the elected conductor normally,
 * or (if that conductor went silent while staying sticky-on-stage) the deterministic rescuer.
 * Bridges the lifecycle gap between conducting (club-view only) and stage presence (sticky).
 */
export function isActingConductor(): boolean {
  const np = state.np
  return shouldConduct(
    auth.pubkey,
    stage.conductor,
    stage.djs.map((d) => d.pubkey),
    np?.writer ?? null,
    np ? Date.now() - np.sentAt : Infinity,
    RESCUE_STALE_MS,
    (pk) => presence.isOnline(pk),
  )
}

/**
 * Called periodically (by ClubView). Only the acting conductor acts: starts the first track,
 * keeps the heartbeat, takes over on failover — and rescues a silent/phantom conductor.
 * IMPORTANT: the heartbeat leaves the running track untouched — stage/queue changes only
 * affect the NEXT track (on end or skip).
 */
export function conductorTick(groupId: string): void {
  if (!isActingConductor()) return // acting conductor only

  const djs = stage.djs.map((d) => d.pubkey)
  const np = state.np

  // Orphan guard: never keep playing a track whose DJ is no longer on an active stage slot
  // (left / went stale). Their leftover now_playing lingers (kind 30100 is replaceable); advance
  // to an active DJ's track. An away-but-sticky DJ is still in stage.djs, so their curated set
  // keeps playing — only a truly off-stage DJ triggers this.
  if (np && djs.length > 0 && np.dj && !djs.includes(np.dj)) {
    advance(groupId)
    return
  }

  const npFresh = !!np && Date.now() - np.sentAt < FAILOVER_MS

  if (!np) {
    // Bootstrap. advance() seeds from the SHARED play-log (playlog) first, so a fresh takeover
    // / lobby-recovery continues the session instead of replaying from the top, and loops via
    // the shared epoch when everything's been played. Empty/away DJs' slots are skipped.
    advance(groupId)
    return
  }

  if (!npFresh) {
    // Very old now_playing = residue of an earlier session (kind 30100 is replaceable and
    // lingers on the relay). Don't resurrect a long-dead track at a stale pos — advance()
    // continues from the shared progress (and won't replay, since the residue is excluded).
    if (Date.now() - np.sentAt > 120_000) {
      advance(groupId)
      return
    }
    // Real failover (conductor only briefly away): KEEP the running track; only advance if
    // it actually finished within this window.
    const elapsed = (Date.now() - np.startedAt) / 1000
    if (np.duration > 0 && elapsed >= np.duration) advance(groupId)
    else heartbeat(groupId) // same track, new conductor takes over the heartbeat
    return
  }

  // Fresh & I'm conductor. The track-end advance is normally driven by the player's `ended`
  // event (onTrackEnded) — but that only fires while a club view's player is mounted. So the
  // tick is ALSO self-driving: if the running track has played out, advance here. This is what
  // lets the conductor keep the rotation going off the club page (ConductorService) — and is a
  // harmless backstop on-club (onTrackEnded usually fires first; the just-started next track
  // is fresh → no double-advance).
  const elapsed = (Date.now() - np.startedAt) / 1000
  if (np.duration > 0 && elapsed >= np.duration) advance(groupId)
  else heartbeat(groupId) // track frozen, new sent_at
}

/**
 * Conductor: immediately re-anchor pos to the running track and republish now_playing, so
 * a just-made reorder is pushed to everyone's Up next without waiting for the heartbeat.
 * No-op for a non-conductor (their republished queue reaches the conductor instead).
 */
export function applyOrderNow(groupId: string): void {
  if (!isActingConductor() || !state.np) return
  state.np = reanchorPos(state.np)
  publishNp(groupId, state.np)
}

/** Manual skip to the next track — ONLY the acting conductor. */
export function skipTrack(groupId: string): void {
  if (!isActingConductor() || !state.np) return
  advance(groupId)
}

/**
 * Skip the current track. The conductor does it directly. An owner/moderator who is NOT
 * the conductor (maybe not even on stage) publishes a skip request that the conductor
 * enacts — only the conductor writes now_playing, so this is the safe path.
 */
export async function requestSkip(groupId: string): Promise<void> {
  const me = auth.pubkey
  if (!me || !state.np) return
  if (isActingConductor()) {
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

/** Whether the local user may skip — the acting conductor, OR an owner/moderator. */
export function canSkip(canModerate = false): boolean {
  return !!auth.pubkey && !!sync.live && (isActingConductor() || canModerate)
}

/** Preview of the next round-robin tracks (across all DJs), max `max`. Mirrors advance():
 *  scans from the TOP (the first active track per DJ, round-robin), excluding the running
 *  track — so "Up next" shows exactly what will play. */
export function upcomingTracks(max = 5): { dj: string; videoId: string; title: string }[] {
  const djs = stage.djs.map((d) => d.pubkey)
  if (djs.length === 0) return []
  const playable = playableExcluding(djs)
  const out: { dj: string; videoId: string; title: string }[] = []
  let pos = -1
  for (let i = 0; i < max; i++) {
    const next = nextPlayablePos(pos, djs.length, playable) // walk active slots from the top
    if (next === -1) break // no more active tracks
    const { djIndex, trackIndex } = posToSlot(next, djs.length)
    const dj = djs[djIndex]
    const track = queues.trackAt(dj, trackIndex)
    if (track) out.push({ dj, videoId: track.videoId, title: track.title })
    pos = next
  }
  return out
}

/** Track end reported by the player — only the acting conductor advances. */
export function onTrackEnded(groupId: string): void {
  if (!isActingConductor() || !state.np) return
  errorStreak = 0 // clean end → reset error streak
  advance(groupId)
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
  if (!isActingConductor() || !state.np) return
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
  advance(groupId)
}

export function resetSync(): void {
  state.np = null
  state.offsetMs = 0
  state.skipIntent = null
  playedThisSession.clear()
  currentLoopEpoch = 0
}

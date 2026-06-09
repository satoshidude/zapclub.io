import type { Event } from 'nostr-tools/pure'
import { KIND_QUEUE, publishClub, fetchClubQueues } from './groups'
import { auth } from './auth.svelte'
import { enrichTitles } from '../player/youtube'
import { isValidVideoId } from '../util'
import type { DjQueue, QueueTrack } from './types'

const state = $state<{ byDj: Record<string, DjQueue> }>({ byDj: {} })

export const queues = {
  /** A DJ's queue (or null). */
  get(pubkey: string): DjQueue | null {
    return state.byDj[pubkey] ?? null
  },
  /**
   * Playability matrix for the round-robin: per DJ an array, true = active (not yet
   * played) track at that index. Played/disabled tracks (active === false) are false
   * → skipped in the rotation.
   */
  playable(djPubkeys: string[]): boolean[][] {
    return djPubkeys.map((pk) =>
      (state.byDj[pk]?.tracks ?? []).map((t) => t.active !== false),
    )
  },
  /** Fetch the track at a round-robin position (full queue, index unshifted). */
  trackAt(dj: string, trackIndex: number): QueueTrack | null {
    return state.byDj[dj]?.tracks[trackIndex] ?? null
  },
}

function parseTracks(ev: Event): QueueTrack[] {
  return ev.tags
    .filter((t) => t[0] === 'track' && t[1]?.startsWith('yt:'))
    .map((t) => ({
      videoId: t[1].slice(3),
      title: t[2] ?? t[1],
      duration: Number(t[3]) || 0,
      // 5th element 'off' = already played/disabled (else active).
      active: t[4] !== 'off',
    }))
    // Only valid YouTube ids — never feed a foreign queue event blindly into player URLs.
    .filter((tr) => isValidVideoId(tr.videoId))
}

/** Handles an incoming queue event (kind 30103). Idempotent: an equal/older event for a DJ
 *  never regresses their state (newest created_at wins) — safe to re-ingest from the poll. */
export function ingestQueue(ev: Event): void {
  const prev = state.byDj[ev.pubkey]
  if (prev && ev.created_at < prev.updatedAt) return
  state.byDj[ev.pubkey] = {
    dj: ev.pubkey,
    tracks: parseTracks(ev),
    updatedAt: ev.created_at,
  }
}

// ── reliable queue re-sync ──────────────────────────────────────────────────
// The live subscription (subscribeClub) pushes queue edits, but pushes can be missed on
// reconnects / relay restarts / network blips — and the round-robin is only as correct as
// the queue state behind it. So we ALSO poll all DJ queues for the active club on an
// interval and re-ingest them (idempotent). This guarantees the round-robin reliably
// reflects the current playlists even when a 30103 push never arrived. This is only a BACKSTOP
// for missed pushes — the live subscription delivers edits instantly, and a roster change /
// tab-return triggers an immediate refresh — so a slow interval suffices and cuts read traffic.
const QUEUE_SYNC_MS = 60_000
let syncTimer: ReturnType<typeof setInterval> | null = null
let syncGroup: string | null = null
let syncing = false

/** Re-query all DJ queues for a club and ingest them (one-shot). Overlap-guarded. */
export async function refreshQueues(groupId: string): Promise<void> {
  if (syncing) return
  syncing = true
  try {
    const events = await fetchClubQueues(groupId)
    for (const ev of events) ingestQueue(ev)
  } catch {
    /* transient relay/network error — the next tick retries */
  } finally {
    syncing = false
  }
}

/** Start the periodic queue re-sync for a club (immediate refresh + interval). Idempotent. */
export function startQueueSync(groupId: string): void {
  if (syncGroup === groupId && syncTimer) return
  stopQueueSync()
  syncGroup = groupId
  void refreshQueues(groupId)
  syncTimer = setInterval(() => {
    if (syncGroup) void refreshQueues(syncGroup)
  }, QUEUE_SYNC_MS)
}

export function stopQueueSync(): void {
  if (syncTimer) {
    clearInterval(syncTimer)
    syncTimer = null
  }
  syncGroup = null
}

// A backgrounded tab throttles the interval; refresh immediately on return so the round-robin
// is current the moment the user looks again.
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && syncGroup) void refreshQueues(syncGroup)
  })
}

// ── edit my own queue ───────────────────────────────────────────────────────

function myTracks(): QueueTrack[] {
  const me = auth.pubkey
  return me ? (state.byDj[me]?.tracks ?? []) : []
}

async function publishMyQueue(groupId: string, tracks: QueueTrack[]): Promise<void> {
  const me = auth.pubkey
  if (!me) return
  // Apply locally right away (optimistic), then publish.
  state.byDj[me] = { dj: me, tracks, updatedAt: Math.floor(Date.now() / 1000) }
  await publishClub({
    kind: KIND_QUEUE,
    created_at: Math.floor(Date.now() / 1000),
    tags: [
      ['h', groupId],
      ['d', groupId],
      ...tracks.map((t) =>
        t.active === false
          ? ['track', `yt:${t.videoId}`, t.title, String(t.duration), 'off']
          : ['track', `yt:${t.videoId}`, t.title, String(t.duration)],
      ),
    ],
    content: '',
  })
}

/**
 * Folds the artist into one of MY tracks' titles ("Artist - Title") and republishes — so a
 * channel-derived artist (learned from the YouTube embed when the track plays) PERSISTS and
 * shows in the Live Set, "Up next", and saved playlists, not just live in the now-playing card.
 * No-op unless the track is mine and its title is still bare (no spaced dash) — never clobbers a
 * title that already carries an artist.
 */
export function enrichMyTrackTitle(groupId: string, videoId: string, title: string): Promise<void> {
  const tracks = myTracks()
  const idx = tracks.findIndex((t) => t.videoId === videoId)
  if (idx < 0 || tracks[idx].title === title || / [–—-] /.test(tracks[idx].title)) return Promise.resolve()
  return publishMyQueue(groupId, tracks.map((t, i) => (i === idx ? { ...t, title } : t)))
}

/**
 * Backfills MY queue's BARE titles (no "Artist - Title" yet) with the interpreter — resolved via
 * the relay's oEmbed channel lookup (one batch, not bot-gated). One republish for all changes.
 * So the Live Set / saved playlists show the artist like the card, not just played/new tracks.
 */
export async function enrichQueueTitles(groupId: string): Promise<void> {
  const me = auth.pubkey
  if (!me) return
  const tracks = state.byDj[me]?.tracks
  if (!tracks?.length) return
  const bare = tracks.filter((t) => !/ [–—-] /.test(t.title)).map((t) => t.videoId).slice(0, 40)
  if (bare.length === 0) return
  const map = await enrichTitles(bare)
  let changed = false
  const next = tracks.map((t) => {
    const nt = map[t.videoId]
    // Only adopt a resolved title that actually adds an artist (a spaced dash) and differs.
    if (nt && nt !== t.title && / [–—-] /.test(nt)) {
      changed = true
      return { ...t, title: nt }
    }
    return t
  })
  if (changed) await publishMyQueue(groupId, next)
}

/** Edits a track's title (by videoId) in MY queue + republishes (only on a real change). */
export function setTrackTitle(groupId: string, videoId: string, title: string): Promise<void> {
  const t = title.trim()
  const tracks = myTracks()
  const idx = tracks.findIndex((x) => x.videoId === videoId)
  if (idx < 0 || !t || tracks[idx].title === t) return Promise.resolve()
  return publishMyQueue(groupId, tracks.map((x, i) => (i === idx ? { ...x, title: t } : x)))
}

/** Sets a track's active state (by videoId) + publishes (only on change). */
export function setTrackActive(groupId: string, videoId: string, active: boolean): Promise<void> {
  const tracks = myTracks()
  const idx = tracks.findIndex((t) => t.videoId === videoId)
  if (idx < 0 || (tracks[idx].active !== false) === active) return Promise.resolve()
  const next = tracks.map((t, i) => (i === idx ? { ...t, active } : t))
  return publishMyQueue(groupId, next)
}

/**
 * Marks MY currently-playing track as played (off) — reliably, even when my club queue isn't
 * resident (I navigated away while staying sticky on stage): falls back to fetch-modify-
 * publish. Idempotent: no-op if already off or the track isn't in my queue. This is the
 * single source of truth for "played" (the round-robin scans from the top and skips `off`),
 * so it must run regardless of whether the club view is mounted — hence driven by the
 * persistent mini-player layer, not a club-view effect.
 */
export async function markMyTrackPlayed(groupId: string, videoId: string): Promise<void> {
  const me = auth.pubkey
  if (!me) return
  let tracks = state.byDj[me]?.tracks
  if (!tracks) {
    // Not resident → fetch my own queue for this club.
    const events = await fetchClubQueues(groupId)
    const mine = events
      .filter((e) => e.pubkey === me)
      .sort((a, b) => b.created_at - a.created_at)[0]
    if (!mine) return
    tracks = parseTracks(mine)
    state.byDj[me] = { dj: me, tracks, updatedAt: mine.created_at }
  }
  const idx = tracks.findIndex((t) => t.videoId === videoId)
  if (idx < 0 || tracks[idx].active === false) return // gone or already off
  await publishMyQueue(
    groupId,
    tracks.map((t, i) => (i === idx ? { ...t, active: false } : t)),
  )
}

/**
 * Reactivate ALL of my tracks for a club (clear the `off`/played flags). Called when I take the
 * stage so my full curated set re-enters the round-robin — `off` is a permanent played-marker,
 * so without this a returning DJ's set stays depleted (their top tracks vanish from Up next).
 * Tracks I actually played THIS session stay excluded by the session play-log (1313), not `off`,
 * so reactivating doesn't replay them. No-op if nothing is off / I'm not signed in.
 */
export async function reactivateMyQueue(groupId: string): Promise<void> {
  const me = auth.pubkey
  if (!me) return
  let tracks = state.byDj[me]?.tracks
  if (!tracks) {
    const events = await fetchClubQueues(groupId)
    const mine = events.filter((e) => e.pubkey === me).sort((a, b) => b.created_at - a.created_at)[0]
    if (!mine) return
    tracks = parseTracks(mine)
    state.byDj[me] = { dj: me, tracks, updatedAt: mine.created_at }
  }
  if (!tracks.some((t) => t.active === false)) return // already all active
  await publishMyQueue(groupId, tracks.map((t) => ({ ...t, active: true })))
}

export function addTrack(groupId: string, track: QueueTrack): Promise<void> {
  return publishMyQueue(groupId, [...myTracks(), track])
}

/** Appends several tracks at once (one publish) — e.g. a YouTube playlist import. */
export function addTracks(groupId: string, tracks: QueueTrack[]): Promise<void> {
  if (tracks.length === 0) return Promise.resolve()
  return publishMyQueue(groupId, [...myTracks(), ...tracks])
}

/** Replaces my own club queue. */
export function setMyQueue(groupId: string, tracks: QueueTrack[]): Promise<void> {
  return publishMyQueue(groupId, tracks)
}

/** Re-publishes my queue as-is (current order + active flags) so the round-robin/
 *  conductor re-reads the latest order. A manual "apply order" for the DJ. */
export function republishQueue(groupId: string): Promise<void> {
  return publishMyQueue(groupId, myTracks())
}

export function removeTrack(groupId: string, index: number): Promise<void> {
  const tracks = myTracks().filter((_, i) => i !== index)
  return publishMyQueue(groupId, tracks)
}

export function moveTrack(groupId: string, from: number, to: number): Promise<void> {
  const tracks = [...myTracks()]
  if (to < 0 || to >= tracks.length) return Promise.resolve()
  const [m] = tracks.splice(from, 1)
  tracks.splice(to, 0, m)
  return publishMyQueue(groupId, tracks)
}

/** Clears my own queue entirely. */
export function clearQueue(groupId: string): Promise<void> {
  return publishMyQueue(groupId, [])
}

/** Shuffles my own queue (Fisher–Yates). Affects the NEXT tracks (set-and-forget). */
export function shuffleQueue(groupId: string): Promise<void> {
  const tracks = [...myTracks()]
  for (let i = tracks.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[tracks[i], tracks[j]] = [tracks[j], tracks[i]]
  }
  return publishMyQueue(groupId, tracks)
}

export function resetQueues(): void {
  state.byDj = {}
}

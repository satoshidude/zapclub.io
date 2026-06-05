import type { Event } from 'nostr-tools/pure'
import { KIND_QUEUE, publishClub } from './groups'
import { auth } from './auth.svelte'
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

/** Handles an incoming queue event (kind 30103). */
export function ingestQueue(ev: Event): void {
  const prev = state.byDj[ev.pubkey]
  if (prev && ev.created_at < prev.updatedAt) return
  state.byDj[ev.pubkey] = {
    dj: ev.pubkey,
    tracks: parseTracks(ev),
    updatedAt: ev.created_at,
  }
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

/** Sets a track's active state (by videoId) + publishes (only on change). */
export function setTrackActive(groupId: string, videoId: string, active: boolean): Promise<void> {
  const tracks = myTracks()
  const idx = tracks.findIndex((t) => t.videoId === videoId)
  if (idx < 0 || (tracks[idx].active !== false) === active) return Promise.resolve()
  const next = tracks.map((t, i) => (i === idx ? { ...t, active } : t))
  return publishMyQueue(groupId, next)
}

/** Marks a just-played own track as "played" (disabled). */
export function markPlayed(groupId: string, videoId: string): Promise<void> {
  return setTrackActive(groupId, videoId, false)
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

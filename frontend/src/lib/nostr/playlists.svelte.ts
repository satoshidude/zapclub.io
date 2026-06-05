import type { Event } from 'nostr-tools/pure'
import { pool, PROFILE_RELAYS } from './pool'
import { signEvent } from './nostrLogin'
import { auth } from './auth.svelte'
import { isValidVideoId } from '../util'
import type { Playlist, QueueTrack } from './types'

// Named, reusable playlists (user-global) live — like the profile — on the public
// relays, parameterized-replaceable (d=playlist-id) per user. Not club-scoped, so they
// are NOT on the NIP-29 relay (which only accepts group #h events).
export const KIND_PLAYLIST = 30104

const state = $state<{ mine: Playlist[]; loaded: boolean }>({ mine: [], loaded: false })

// Local tombstones: ids the user deleted. NIP-09 deletions aren't reliably honored by
// every public relay (indexers like nostr.band keep re-serving the event), so we also
// hide deleted playlists client-side — persisted so they stay gone across reloads.
const DELETED_KEY = 'zapclub:deletedPlaylists'
function loadDeleted(): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(DELETED_KEY) || '[]') as string[])
  } catch {
    return new Set()
  }
}
const deletedIds = loadDeleted()
function persistDeleted(): void {
  try {
    localStorage.setItem(DELETED_KEY, JSON.stringify([...deletedIds]))
  } catch {
    /* ignore */
  }
}

export const playlists = {
  get mine(): Playlist[] {
    return state.mine
  },
  get loaded(): boolean {
    return state.loaded
  },
}

function nowSec(): number {
  return Math.floor(Date.now() / 1000)
}

function rndId(): string {
  const b = crypto.getRandomValues(new Uint8Array(8))
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
}

function parsePlaylist(ev: Event): Playlist {
  const tag = (n: string) => ev.tags.find((t) => t[0] === n)?.[1]
  const tracks: QueueTrack[] = ev.tags
    .filter((t) => t[0] === 'track' && t[1]?.startsWith('yt:'))
    .map((t) => ({ videoId: t[1].slice(3), title: t[2] ?? t[1], duration: Number(t[3]) || 0 }))
    .filter((t) => isValidVideoId(t.videoId))
  return { id: tag('d') ?? '', name: tag('title') || 'Playlist', tracks, updatedAt: ev.created_at }
}

/** Saves the current set as a new named playlist. Returns the created playlist. */
export async function savePlaylistAs(name: string, tracks: QueueTrack[]): Promise<Playlist> {
  const pl: Playlist = { id: rndId(), name: name.trim() || 'Untitled', tracks, updatedAt: nowSec() }
  state.mine = [pl, ...state.mine]
  await publishPlaylist(pl)
  return pl
}

async function publishPlaylist(pl: Playlist): Promise<void> {
  const signed = await signEvent({
    kind: KIND_PLAYLIST,
    created_at: nowSec(),
    tags: [
      ['d', pl.id],
      ['title', pl.name],
      ...pl.tracks.map((t) => ['track', `yt:${t.videoId}`, t.title, String(t.duration)]),
    ],
    content: '',
  })
  await Promise.allSettled(pool.publish(PROFILE_RELAYS, signed))
}

/** Deletes a playlist (local tombstone + NIP-09 request to the relays). */
export async function deletePlaylist(id: string): Promise<void> {
  deletedIds.add(id)
  persistDeleted()
  state.mine = state.mine.filter((p) => p.id !== id)
  const me = auth.pubkey
  if (!me) return
  const signed = await signEvent({
    kind: 5,
    created_at: nowSec(),
    tags: [
      ['a', `${KIND_PLAYLIST}:${me}:${id}`],
      ['k', String(KIND_PLAYLIST)],
    ],
    content: '',
  })
  await Promise.allSettled(pool.publish(PROFILE_RELAYS, signed))
}

/** Fetch a user's playlists (deduped per id, newest wins). */
export async function fetchPlaylists(pubkey: string): Promise<Playlist[]> {
  const events = await pool.querySync(
    PROFILE_RELAYS,
    { kinds: [KIND_PLAYLIST], authors: [pubkey] },
    { maxWait: 4000 },
  )
  const byId = new Map<string, Playlist>()
  for (const ev of events) {
    const pl = parsePlaylist(ev)
    if (!pl.id || deletedIds.has(pl.id)) continue // skip locally-deleted (tombstoned)
    const ex = byId.get(pl.id)
    if (!ex || pl.updatedAt > ex.updatedAt) byId.set(pl.id, pl)
  }
  return [...byId.values()].sort((a, b) => b.updatedAt - a.updatedAt)
}

/** Load the current user's playlists into the store (once). */
export async function loadMyPlaylists(): Promise<void> {
  const me = auth.pubkey
  if (!me) return
  state.mine = await fetchPlaylists(me)
  state.loaded = true
}

/** On logout/user switch: drop the old user's playlists. */
export function resetPlaylists(): void {
  state.mine = []
  state.loaded = false
}

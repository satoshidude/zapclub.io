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
    .map((t) => ({ videoId: t[1].slice(3), title: t[2] ?? t[1], duration: Number(t[3]) || 0, image: t[5] || undefined }))
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
      ...pl.tracks.map((t) =>
        t.image
          ? ['track', `yt:${t.videoId}`, t.title, String(t.duration), '', t.image]
          : ['track', `yt:${t.videoId}`, t.title, String(t.duration)],
      ),
    ],
    content: '',
  })
  await Promise.allSettled(pool.publish(PROFILE_RELAYS, signed))
}

/** Saves an existing playlist (same id, replaceable) — used for reorder/move/copy. */
export async function updatePlaylist(pl: Playlist): Promise<void> {
  const updated: Playlist = { ...pl, updatedAt: nowSec() }
  const i = state.mine.findIndex((p) => p.id === pl.id)
  if (i >= 0) state.mine[i] = updated
  else state.mine = [updated, ...state.mine]
  await publishPlaylist(updated)
}

/** Reorders a track within a playlist (drag-and-drop). */
export async function reorderTrack(playlistId: string, from: number, to: number): Promise<void> {
  const pl = state.mine.find((p) => p.id === playlistId)
  if (!pl || to < 0 || to >= pl.tracks.length || from === to) return
  const tracks = [...pl.tracks]
  const [m] = tracks.splice(from, 1)
  tracks.splice(to, 0, m)
  await updatePlaylist({ ...pl, tracks })
}

/** Edits a track's title within a playlist (only on a real change). */
export async function setPlaylistTrackTitle(playlistId: string, videoId: string, title: string): Promise<void> {
  const t = title.trim()
  const pl = state.mine.find((p) => p.id === playlistId)
  if (!pl || !t) return
  const idx = pl.tracks.findIndex((x) => x.videoId === videoId)
  if (idx < 0 || pl.tracks[idx].title === t) return
  await updatePlaylist({ ...pl, tracks: pl.tracks.map((x, i) => (i === idx ? { ...x, title: t } : x)) })
}

/** Sets/clears a track's custom cover image within a playlist (only on a real change). */
export async function setPlaylistTrackImage(playlistId: string, videoId: string, image: string | undefined): Promise<void> {
  const pl = state.mine.find((p) => p.id === playlistId)
  if (!pl) return
  const idx = pl.tracks.findIndex((x) => x.videoId === videoId)
  if (idx < 0 || (pl.tracks[idx].image ?? '') === (image ?? '')) return
  await updatePlaylist({ ...pl, tracks: pl.tracks.map((x, i) => (i === idx ? { ...x, image } : x)) })
}

export async function removeFromPlaylist(playlistId: string, videoId: string): Promise<void> {
  const pl = state.mine.find((p) => p.id === playlistId)
  if (!pl) return
  await updatePlaylist({ ...pl, tracks: pl.tracks.filter((t) => t.videoId !== videoId) })
}

/** Creates a new, empty named playlist. Returns it. */
export async function createPlaylist(name: string): Promise<Playlist> {
  return savePlaylistAs(name, [])
}

/** Appends a track to a playlist (deduped by videoId). */
export async function addTrackToPlaylist(playlistId: string, track: QueueTrack): Promise<void> {
  const pl = state.mine.find((p) => p.id === playlistId)
  if (!pl || pl.tracks.some((t) => t.videoId === track.videoId)) return
  await updatePlaylist({ ...pl, tracks: [...pl.tracks, track] })
}

/** Appends several tracks to a playlist in ONE publish (deduped, order preserved) — e.g. a
 *  YouTube playlist import. Adding them one by one would publish the replaceable event N
 *  times. */
export async function addTracksToPlaylist(playlistId: string, tracks: QueueTrack[]): Promise<void> {
  const pl = state.mine.find((p) => p.id === playlistId)
  if (!pl || tracks.length === 0) return
  const have = new Set(pl.tracks.map((t) => t.videoId))
  const fresh = tracks.filter((t) => !have.has(t.videoId) && have.add(t.videoId))
  if (fresh.length === 0) return
  await updatePlaylist({ ...pl, tracks: [...pl.tracks, ...fresh] })
}

/** Copies a track into another playlist (source untouched; deduped). */
export async function copyTrackTo(toId: string, track: QueueTrack): Promise<void> {
  const to = state.mine.find((p) => p.id === toId)
  if (!to || to.tracks.some((t) => t.videoId === track.videoId)) return
  await updatePlaylist({ ...to, tracks: [...to.tracks, track] })
}

/** Moves a track from one playlist to another (copy to target, remove from source). */
export async function moveTrackBetween(fromId: string, toId: string, videoId: string): Promise<void> {
  if (fromId === toId) return
  const from = state.mine.find((p) => p.id === fromId)
  const to = state.mine.find((p) => p.id === toId)
  if (!from || !to) return
  const track = from.tracks.find((t) => t.videoId === videoId)
  if (!track) return
  if (!to.tracks.some((t) => t.videoId === videoId)) {
    await updatePlaylist({ ...to, tracks: [...to.tracks, track] })
  }
  await updatePlaylist({ ...from, tracks: from.tracks.filter((t) => t.videoId !== videoId) })
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

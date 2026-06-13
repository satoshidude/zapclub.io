import type { Event } from 'nostr-tools/pure'
import { KIND_AUTODJ, publishClub } from './groups'
import { CLUB_RELAY_PUBKEY } from './pool'
import type { Playlist } from './types'

// Auto DJ: an owner can arm one of their saved playlists as a virtual DJ participant.
// When real DJs are on stage, auto-DJ joins the round-robin as another slot.
// When no real DJ is on stage, the conductor plays the playlist in shuffled loop rotation.
// Arming publishes a kind-30105 snapshot to the club relay.
// The owner manually disarms via the Stop button (publishes 30105 with status=off).

interface AutoDJConfig {
  clubId: string
  armed: boolean
  srcId: string
  name: string
  updatedAt: number
}

interface AutoDJCtrl {
  clubId: string
  updatedAt: number
}

const state = $state<{
  config: Record<string, AutoDJConfig>
  ctrl: Record<string, AutoDJCtrl>
}>({ config: {}, ctrl: {} })

function now(): number {
  return Math.floor(Date.now() / 1000)
}

/** Effective-armed: latest 30105 has status=armed AND no newer 30111 disarm marker. */
function effectivelyArmed(clubId: string): boolean {
  const cfg = state.config[clubId]
  if (!cfg || !cfg.armed) return false
  const ctrl = state.ctrl[clubId]
  if (!ctrl) return true
  return ctrl.updatedAt < cfg.updatedAt
}

export const autodj = {
  isArmed(clubId: string): boolean {
    return effectivelyArmed(clubId)
  },
  name(clubId: string): string {
    return state.config[clubId]?.name ?? ''
  },
}

/** Ingest a kind-30105 Auto DJ config event (owner-authored). */
export function ingestAutoDJ(ev: Event): void {
  const club = ev.tags.find((t) => t[0] === 'h')?.[1]
  if (!club) return
  const prev = state.config[club]
  if (prev && ev.created_at < prev.updatedAt) return
  const armed = ev.tags.find((t) => t[0] === 'status')?.[1] === 'armed'
  const srcId = ev.tags.find((t) => t[0] === 'src')?.[1] ?? ''
  state.config = { ...state.config, [club]: { clubId: club, armed, srcId, name: ev.content, updatedAt: ev.created_at } }
}

/** Ingest a kind-30111 Auto DJ disarm marker (relay-signed). */
export function ingestAutoCtrl(ev: Event): void {
  if (ev.pubkey !== CLUB_RELAY_PUBKEY) return
  const club = ev.tags.find((t) => t[0] === 'd')?.[1]
  if (!club) return
  const prev = state.ctrl[club]
  if (prev && ev.created_at < prev.updatedAt) return
  state.ctrl = { ...state.ctrl, [club]: { clubId: club, updatedAt: ev.created_at } }
}

/** Arm Auto DJ: snapshot the playlist's tracks into a kind-30105 on the club relay. */
export async function armAutoDJ(clubId: string, playlist: Playlist): Promise<void> {
  await publishClub({
    kind: KIND_AUTODJ,
    created_at: now(),
    tags: [
      ['h', clubId],
      ['d', clubId],
      ['status', 'armed'],
      ['src', playlist.id],
      ...playlist.tracks.map((t): string[] => ['track', `yt:${t.videoId}`, t.title, String(t.duration)]),
    ],
    content: playlist.name,
  })
  // Optimistic update so the UI responds immediately.
  state.config = {
    ...state.config,
    [clubId]: { clubId, armed: true, srcId: playlist.id, name: playlist.name, updatedAt: now() },
  }
  // Clear the local disarm so the re-arm wins immediately.
  if (state.ctrl[clubId]) {
    state.ctrl = { ...state.ctrl, [clubId]: { clubId, updatedAt: 0 } }
  }
}

/** Disarm Auto DJ: owner publishes kind-30105 with status=off. */
export async function disarmAutoDJ(clubId: string): Promise<void> {
  const cfg = state.config[clubId]
  await publishClub({
    kind: KIND_AUTODJ,
    created_at: now(),
    tags: [
      ['h', clubId],
      ['d', clubId],
      ['status', 'off'],
      ...(cfg ? [['src', cfg.srcId] as string[]] : []),
    ],
    content: cfg?.name ?? '',
  })
  if (state.config[clubId]) {
    state.config = { ...state.config, [clubId]: { ...state.config[clubId], armed: false, updatedAt: now() } }
  }
}

export function resetAutoDJ(): void {
  state.config = {}
  state.ctrl = {}
}

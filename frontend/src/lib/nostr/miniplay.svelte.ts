import type { Event } from 'nostr-tools/pure'
import { pool, CLUB_RELAY } from './pool'

// A small, independent now_playing tracker for the GLOBAL mini-player — separate from the
// `sync` singleton that ClubView drives. It keeps the audio of the *active* club alive
// while the user browses other pages, until they enter a different club.
//
// Time units: Nostr created_at is seconds; started_at/sent_at tags are ms. We keep ms.
const KEY = 'zapclub:activeClub'

interface MiniNP {
  videoId: string
  startedAt: number // ms (conductor clock)
  duration: number // seconds
  title: string
  status: string
}

interface MiniState {
  clubId: string | null
  clubName: string
  np: MiniNP | null
  offsetMs: number // sent_at - Date.now() at ingest → drift calibration
}

const state = $state<MiniState>({ clubId: null, clubName: '', np: null, offsetMs: 0 })
let sub: { close(): void } | null = null
let newest = 0 // created_at (s) of the newest now_playing seen

export const miniplay = {
  get clubId() {
    return state.clubId
  },
  get clubName() {
    return state.clubName
  },
  get np() {
    return state.np
  },
  get active() {
    return !!state.clubId && !!state.np && state.np.status !== 'stopped'
  },
}

/** Current drift-corrected playback position in seconds. */
export function miniPosition(): number {
  if (!state.np) return 0
  return Math.max(0, (Date.now() + state.offsetMs - state.np.startedAt) / 1000)
}

function ingest(ev: Event): void {
  if (ev.created_at < newest) return
  newest = ev.created_at
  const tag = (k: string) => ev.tags.find((t) => t[0] === k)?.[1]
  const track = tag('track') ?? ''
  const videoId = track.startsWith('yt:') ? track.slice(3) : ''
  const startedAt = Number(tag('started_at')) || ev.created_at * 1000
  const sentAt = Number(tag('sent_at')) || Date.now()
  state.offsetMs = sentAt - Date.now()
  state.np = {
    videoId,
    startedAt,
    duration: Number(tag('duration')) || 0,
    title: ev.content || track,
    status: tag('status') || 'playing',
  }
}

/**
 * Marks a club as the active audio source and subscribes to its now_playing. Switching
 * to a different club tears down the old subscription (the new club's music takes over).
 */
export function registerActiveClub(clubId: string, clubName: string): void {
  if (clubName) state.clubName = clubName
  if (state.clubId === clubId) return
  state.clubId = clubId
  state.np = null
  newest = 0
  try {
    localStorage.setItem(KEY, clubId)
  } catch {
    /* ignore */
  }
  sub?.close()
  sub = pool.subscribe([CLUB_RELAY], { kinds: [30100], '#h': [clubId] }, { onevent: ingest })
}

export function persistedActiveClub(): string | null {
  try {
    return localStorage.getItem(KEY)
  } catch {
    return null
  }
}

export function stopMiniPlay(): void {
  sub?.close()
  sub = null
  state.clubId = null
  state.clubName = ''
  state.np = null
  newest = 0
  try {
    localStorage.removeItem(KEY)
  } catch {
    /* ignore */
  }
}

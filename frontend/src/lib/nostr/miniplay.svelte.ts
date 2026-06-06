import type { Event } from 'nostr-tools/pure'
import { pool, CLUB_RELAY } from './pool'
import { auth } from './auth.svelte'
import { markMyTrackPlayed } from './queue.svelte'

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
  dj: string // pubkey of the DJ whose track is playing (for reliable played-marking)
}

interface MiniState {
  clubId: string | null
  clubName: string
  np: MiniNP | null
  offsetMs: number // sent_at - Date.now() at ingest → drift calibration
  /** Reactive clock (ms) so `active` flips off when the conductor goes silent. */
  now: number
}

// Past this much silence the conductor is gone (navigated away while sticky-on-stage) — the
// mini-bar must not keep "playing" a dead track. Matches sync.LIVE_STALE_MS.
const LIVE_STALE_MS = 150_000

const state = $state<MiniState>({ clubId: null, clubName: '', np: null, offsetMs: 0, now: Date.now() })
let sub: { close(): void } | null = null
let newestSent = 0 // sent_at (ms) of the newest now_playing seen — ms so same-second heartbeats aren't dropped
let lastIngestMs = 0 // wall-clock of the last accepted heartbeat → staleness
let lastMarked = '' // videoId I last marked played → fire the mark once per track

if (typeof setInterval !== 'undefined') {
  setInterval(() => {
    state.now = Date.now()
  }, 5000)
}

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
    return (
      !!state.clubId &&
      !!state.np &&
      state.np.status !== 'stopped' &&
      state.now - lastIngestMs <= LIVE_STALE_MS // conductor still alive
    )
  },
}

/** Current drift-corrected playback position in seconds. */
export function miniPosition(): number {
  if (!state.np) return 0
  return Math.max(0, (Date.now() + state.offsetMs - state.np.startedAt) / 1000)
}

function ingest(ev: Event): void {
  const tag = (k: string) => ev.tags.find((t) => t[0] === k)?.[1]
  const track = tag('track') ?? ''
  const videoId = track.startsWith('yt:') ? track.slice(3) : ''
  const startedAt = Number(tag('started_at')) || ev.created_at * 1000
  const sentAt = Number(tag('sent_at')) || ev.created_at * 1000
  if (sentAt <= newestSent) return // older/duplicate heartbeat — keep the freshest (ms)
  newestSent = sentAt
  lastIngestMs = Date.now()
  state.offsetMs = sentAt - Date.now()
  const dj = tag('dj') ?? ev.pubkey
  state.np = {
    videoId,
    startedAt,
    duration: Number(tag('duration')) || 0,
    title: ev.content || track,
    status: tag('status') || 'playing',
    dj,
  }
  // Reliable played-marking: if the live track is MINE, mark it off in my queue — even when
  // I'm not in the club view (this runs from the persistent mini-player layer). The
  // round-robin scans from the top and skips `off`, so this is what keeps played tracks out
  // of the rotation regardless of where I've navigated. Guarded so it fires once per track.
  if (videoId && dj === auth.pubkey && state.clubId && videoId !== lastMarked) {
    lastMarked = videoId
    void markMyTrackPlayed(state.clubId, videoId)
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
  newestSent = 0
  lastIngestMs = 0
  lastMarked = ''
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
  newestSent = 0
  lastIngestMs = 0
  lastMarked = ''
  try {
    localStorage.removeItem(KEY)
  } catch {
    /* ignore */
  }
}

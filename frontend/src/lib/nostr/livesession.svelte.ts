import { auth } from './auth.svelte'
import { pool } from './pool'
import { CLUB_RELAY } from './pool'
import { signEvent } from './nostrLogin'

export const KIND_LIVE_SESSION = 30109
export const LIVE_SESSION_STALE_MS = 300_000 // 5 min — matches relay liveSessionStaleMS

export type LiveMode = 'takeover' | 'talkover'
export type LiveMedia = 'audio' | 'av'

export interface LiveSession {
  mode: LiveMode
  media: LiveMedia
  dj: string     // pubkey of the live DJ (zap target)
  startedAt: number // ms
}

// ── Reactive state ────────────────────────────────────────────────────────────

let _session = $state<LiveSession | null>(null)

/** The active live-session for the current club, or null. */
export const liveSession = {
  get current() {
    return _session
  },
}

// Per-author newest event (to select the freshest session across multiple DJs).
const _newest = new Map<string, { ev: { kind: number; pubkey: string; created_at: number; tags: string[][] }; parsed: LiveSession }>()

/** Called by groups.ts when a kind-30109 event arrives from the subscription. */
export function ingestLiveSession(ev: { kind: number; pubkey: string; created_at: number; tags: string[][] }): void {
  const existing = _newest.get(ev.pubkey)
  if (existing && ev.created_at <= existing.ev.created_at) return

  const mode = tag(ev, 'mode')
  const media = tag(ev, 'media')
  const status = tag(ev, 'status')
  const startedAt = Number(tag(ev, 'started_at')) || ev.created_at * 1000

  if (!mode || !media) {
    _newest.delete(ev.pubkey)
  } else {
    _newest.set(ev.pubkey, {
      ev,
      parsed: {
        mode: mode as LiveMode,
        media: media as LiveMedia,
        dj: ev.pubkey,
        startedAt,
      },
    })
  }

  // If status=ended, clear this author.
  if (status === 'ended') {
    _newest.delete(ev.pubkey)
  }

  _recompute()
}

function _recompute(): void {
  const now = Date.now()
  let best: LiveSession | null = null
  let bestTs = 0

  for (const [, entry] of _newest) {
    const { ev, parsed } = entry
    const ageMs = now - ev.created_at * 1000
    if (ageMs > LIVE_SESSION_STALE_MS) continue
    if (tag(ev, 'status') === 'ended') continue
    if (ev.created_at > bestTs) {
      bestTs = ev.created_at
      best = parsed
    }
  }

  _session = best
}

function tag(ev: { tags: string[][] }, name: string): string {
  return ev.tags.find((t) => t[0] === name)?.[1] ?? ''
}

/** Clears all session state — call on club change. */
export function resetLiveSession(): void {
  _newest.clear()
  _session = null
}

// ── Publish helpers ───────────────────────────────────────────────────────────

/** Publish a kind-30109 "live" event for the given club. */
export async function goLive(groupId: string, mode: LiveMode, media: LiveMedia): Promise<void> {
  const pk = auth.pubkey
  if (!pk) throw new Error('Not signed in')
  const now = Math.floor(Date.now() / 1000)
  const ev = {
    kind: KIND_LIVE_SESSION,
    created_at: now,
    pubkey: pk,
    tags: [
      ['h', groupId],
      ['d', groupId],
      ['mode', mode],
      ['media', media],
      ['status', 'live'],
      ['started_at', String(Date.now())],
    ],
    content: '',
  }
  const signed = await signEvent(ev)
  await pool.publish([CLUB_RELAY], signed)
}

/** Publish a kind-30109 "ended" event for the given club. */
export async function endLive(groupId: string): Promise<void> {
  const pk = auth.pubkey
  if (!pk) return
  const ev = {
    kind: KIND_LIVE_SESSION,
    created_at: Math.floor(Date.now() / 1000),
    pubkey: pk,
    tags: [
      ['h', groupId],
      ['d', groupId],
      ['mode', 'takeover'], // mode must be present for relay acceptance; value is irrelevant on end
      ['media', 'av'],
      ['status', 'ended'],
    ],
    content: '',
  }
  const signed = await signEvent(ev)
  await pool.publish([CLUB_RELAY], signed)
}

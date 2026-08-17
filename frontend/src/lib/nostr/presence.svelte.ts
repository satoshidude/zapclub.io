import type { Event } from 'nostr-tools/pure'
import { publishClub, KIND_PRESENCE } from './groups'
import { auth } from './auth.svelte'

// Live presence: each logged-in member of the club it's open in posts an ephemeral heartbeat
// (kind 20100, not stored) every BEAT_MS. Others mark a pubkey "online" while its last beat is
// within ONLINE_MS. This is what backs the green/purple "online" ring on avatars — and tells
// whether a DJ shown on stage is actually here right now (the stage event is sticky for 1h).
const ONLINE_MS = 50_000
const BEAT_MS = 25_000

const state = $state<{ seen: Record<string, number>; now: number }>({ seen: {}, now: Date.now() })

// Reactive clock so isOnline() re-evaluates (and expires) without new events.
if (typeof setInterval !== 'undefined') {
  setInterval(() => {
    state.now = Date.now()
  }, 5000)
}

export const presence = {
  /** Has this pubkey beat recently (is it here right now)? */
  isOnline(pubkey: string): boolean {
    return !!pubkey && state.now - (state.seen[pubkey] ?? 0) < ONLINE_MS
  },
  /** How many people are present in the club right now (recent heartbeats). These are the
   *  current listeners — everyone in the club hears the same synced stream. Note: only
   *  logged-in members beat presence (the relay rejects non-member writes), so anonymous
   *  guests aren't counted — there is no central listener tracking by design. */
  get count(): number {
    let n = 0
    for (const pk in state.seen) {
      if (state.now - state.seen[pk] < ONLINE_MS) n++
    }
    return n
  },
}

/** Handles an incoming presence heartbeat (kind 20100). */
export function ingestPresence(ev: Event): void {
  const ms = ev.created_at * 1000
  if (ms > (state.seen[ev.pubkey] ?? 0)) state.seen = { ...state.seen, [ev.pubkey]: ms }
}

let beat: ReturnType<typeof setInterval> | null = null
let myGroup: string | null = null

/** Start beating presence for a club (logged-in users only; the relay rejects non-members). */
export function startPresence(groupId: string): void {
  stopPresence()
  if (!auth.pubkey) return
  myGroup = groupId
  const post = () => {
    if (myGroup && auth.pubkey) {
      void publishClub({
        kind: KIND_PRESENCE,
        created_at: Math.floor(Date.now() / 1000),
        tags: [['h', myGroup]],
        content: '',
      })
    }
  }
  post()
  beat = setInterval(post, BEAT_MS)
  // Beat immediately when the tab returns so presence doesn't lag after backgrounding.
  if (typeof document !== 'undefined') document.addEventListener('visibilitychange', onVisible)
}

function onVisible(): void {
  if (document.visibilityState === 'visible' && myGroup && auth.pubkey) {
    void publishClub({
      kind: KIND_PRESENCE,
      created_at: Math.floor(Date.now() / 1000),
      tags: [['h', myGroup]],
      content: '',
    })
  }
}

export function stopPresence(): void {
  if (beat) {
    clearInterval(beat)
    beat = null
  }
  myGroup = null
  if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisible)
}

export function resetPresence(): void {
  state.seen = {}
  stopPresence()
}

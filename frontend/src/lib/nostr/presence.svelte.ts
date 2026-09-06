import type { Event } from 'nostr-tools/pure'
import { publishSessionClub, KIND_PRESENCE } from './groups'
import { auth } from './auth.svelte'
import { sessionEventPrincipal } from './sessionSigner'

// Live presence: each logged-in member of the club it's open in posts an ephemeral heartbeat
// (kind 20100, not stored) every BEAT_MS. Others mark a pubkey "online" while its last beat is
// within ONLINE_MS. This is what backs the green/purple "online" ring on avatars — and tells
// whether a DJ shown on stage is actually here right now (the stage lease is sticky for 5 min).
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
  /** How many signed-in members are socially present right now (recent heartbeats).
   *  Stream listeners are counted separately through anonymous browser sessions. */
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
  const principal = sessionEventPrincipal(ev)
  const ms = ev.created_at * 1000
  if (ms > (state.seen[principal] ?? 0)) state.seen = { ...state.seen, [principal]: ms }
}

export type PresenceClaim = 'view' | 'stage'

const claims = new Map<PresenceClaim, string>()
const IMPULSE_COOLDOWN_MS = 5000

interface Publisher {
  timer: ReturnType<typeof setInterval>
  lastSentAt: number
  posting: boolean
  pendingImmediate: boolean
}

const publishers = new Map<string, Publisher>()

function post(groupId: string, force = false): void {
  const publisher = publishers.get(groupId)
  if (!publisher || !auth.pubkey) return
  if (!force && Date.now() - publisher.lastSentAt < IMPULSE_COOLDOWN_MS) return
  if (publisher.posting) {
    if (!force) publisher.pendingImmediate = true
    return
  }

  publisher.posting = true
  publisher.lastSentAt = Date.now()
  void publishSessionClub({
    kind: KIND_PRESENCE,
    created_at: Math.floor(Date.now() / 1000),
    tags: [['h', groupId]],
    content: '',
  })
    .catch((error) => console.warn('[zc:presence] publish failed:', error))
    .finally(() => {
      publisher.posting = false
      if (publisher.pendingImmediate && publishers.get(groupId) === publisher) {
        publisher.pendingImmediate = false
        post(groupId)
      }
    })
}

function reconcilePublisher(): void {
  const desired = new Set(claims.values())
  for (const [groupId, publisher] of publishers) {
    if (!desired.has(groupId)) {
      clearInterval(publisher.timer)
      publishers.delete(groupId)
    }
  }
  for (const groupId of desired) {
    if (publishers.has(groupId)) continue
    const publisher: Publisher = {
      timer: setInterval(() => post(groupId, true), BEAT_MS),
      lastSentAt: 0,
      posting: false,
      pendingImmediate: false,
    }
    publishers.set(groupId, publisher)
    post(groupId)
  }
}

/** Claims live presence for one feature. Equal view/stage claims share one publisher. */
export function startPresence(groupId: string, claim: PresenceClaim = 'view'): void {
  if (!auth.pubkey) return
  claims.set(claim, groupId)
  reconcilePublisher()
}

function onVisible(): void {
  if (document.visibilityState === 'visible' && auth.pubkey) {
    for (const groupId of publishers.keys()) post(groupId)
  }
}

export function stopPresence(claim: PresenceClaim = 'view'): void {
  claims.delete(claim)
  reconcilePublisher()
}

export function resetPresence(): void {
  state.seen = {}
  claims.clear()
  reconcilePublisher()
}

// One listener for the module lifetime; post() itself is claim- and cooldown-aware.
if (typeof document !== 'undefined') document.addEventListener('visibilitychange', onVisible)

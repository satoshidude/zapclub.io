import { finalizeEvent, generateSecretKey, type Event } from 'nostr-tools/pure'
import { KIND_LISTENER_BEAT, KIND_LISTENER_COUNT } from './groups'
import { CLUB_RELAY, CLUB_RELAY_PUBKEY, pool } from './pool'

const BEAT_MS = 25_000
const COUNT_STALE_MS = 45_000
const SESSION_KEY_PREFIX = 'zapclub:listener-session-key:'

interface ListenerState {
  clubId: string | null
  count: number
  sentAt: number
  now: number
}

const state = $state<ListenerState>({ clubId: null, count: 0, sentAt: 0, now: Date.now() })

if (typeof setInterval !== 'undefined') {
  setInterval(() => {
    state.now = Date.now()
  }, 5000)
}

export const listeners = {
  /** Relay-authoritative active browser sessions for the current club. */
  get count(): number {
    if (!state.clubId || state.now-state.sentAt > COUNT_STALE_MS) return 0
    return state.count
  },
}

/** Parse only fresh-looking aggregate events signed by the configured relay key. */
export function parseListenerCount(event: Event, clubId: string): { count: number; sentAt: number } | null {
  if (event.kind !== KIND_LISTENER_COUNT || event.pubkey !== CLUB_RELAY_PUBKEY) return null
  const tag = (name: string) => event.tags.find((entry) => entry[0] === name)?.[1]
  if (tag('h') !== clubId) return null
  const count = Number(tag('count'))
  const sentAt = Number(tag('sent_at'))
  if (!Number.isSafeInteger(count) || count < 0 || !Number.isSafeInteger(sentAt) || sentAt <= 0) return null
  return { count, sentAt }
}

export function ingestListenerCount(event: Event, clubId: string): void {
  const next = parseListenerCount(event, clubId)
  if (!next || next.sentAt < state.sentAt) return
  state.clubId = clubId
  state.count = next.count
  state.sentAt = next.sentAt
}

const sessionKeys = new Map<string, Uint8Array>()
let activeClub: string | null = null
let beatTimer: ReturnType<typeof setInterval> | null = null

// One throwaway identity per browser tab. It survives a refresh via sessionStorage but is
// unrelated to the user's Nostr account and disappears when the tab is closed.
function listenerSessionKey(clubId: string): Uint8Array {
  const existing = sessionKeys.get(clubId)
  if (existing) return existing
  const storageKey = SESSION_KEY_PREFIX + clubId
  try {
    const raw = sessionStorage.getItem(storageKey)
    const bytes = raw ? (JSON.parse(raw) as number[]) : null
    if (Array.isArray(bytes) && bytes.length === 32) {
      const key = Uint8Array.from(bytes)
      sessionKeys.set(clubId, key)
      return key
    }
  } catch {
    // Storage can be disabled; the in-memory key still covers the current page lifetime.
  }
  const key = generateSecretKey()
  sessionKeys.set(clubId, key)
  try {
    sessionStorage.setItem(storageKey, JSON.stringify(Array.from(key)))
  } catch {
    // Best effort only.
  }
  return key
}

function publishState(clubId: string, value: 'on' | 'off'): void {
  const event = createListenerBeat(clubId, value, listenerSessionKey(clubId))
  void Promise.allSettled(pool.publish([CLUB_RELAY], event))
}

export function createListenerBeat(clubId: string, value: 'on' | 'off', key: Uint8Array): Event {
  return finalizeEvent(
    {
      kind: KIND_LISTENER_BEAT,
      created_at: Math.floor(Date.now() / 1000),
      tags: [['h', clubId], ['state', value]],
      content: '',
    },
    key,
  )
}

function onVisible(): void {
  if (document.visibilityState === 'visible' && activeClub) publishState(activeClub, 'on')
}

function onPageHide(): void {
  if (activeClub) publishState(activeClub, 'off')
}

function onPageShow(): void {
  if (activeClub) publishState(activeClub, 'on')
}

/** Start counting this tab once its ClubView is subscribed and allowed to hear the club. */
export function startListening(clubId: string): void {
  if (activeClub === clubId) {
    publishState(clubId, 'on')
    return
  }
  stopListening()
  activeClub = clubId
  state.clubId = clubId
  state.count = 0
  state.sentAt = 0
  publishState(clubId, 'on')
  beatTimer = setInterval(() => {
    if (activeClub) publishState(activeClub, 'on')
  }, BEAT_MS)
  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('pagehide', onPageHide)
    window.addEventListener('pageshow', onPageShow)
  }
}

export function stopListening(): void {
  const clubId = activeClub
  activeClub = null
  if (beatTimer) {
    clearInterval(beatTimer)
    beatTimer = null
  }
  if (typeof document !== 'undefined') {
    document.removeEventListener('visibilitychange', onVisible)
    window.removeEventListener('pagehide', onPageHide)
    window.removeEventListener('pageshow', onPageShow)
  }
  if (clubId) publishState(clubId, 'off')
  state.clubId = null
  state.count = 0
  state.sentAt = 0
}

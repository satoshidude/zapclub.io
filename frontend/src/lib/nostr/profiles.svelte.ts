import type { Event } from 'nostr-tools/pure'
import { npubEncode } from 'nostr-tools/nip19'
import { pool, PROFILE_RELAYS } from './pool'
import { safeImageUrl } from '../util'
import type { ProfileMetadata } from './types'

// Reactive profile cache for member/chat/DJ display. Profiles (kind 0) are loaded in
// BATCHES (one {kinds:[0], authors:[…]} query per ~250 ms window across all relays)
// instead of one query per pubkey — far fewer connections, far more reliable. A miss is
// NOT cached permanently: it's retried after a cooldown so a transient relay timeout
// doesn't pin a name/avatar to the npub fallback for the whole session.
const cache = $state<Record<string, ProfileMetadata>>({})
const tried = new Map<string, number>() // pubkey -> last attempt (ms)
const RETRY_MS = 30_000
const BATCH = 100

let queue = new Set<string>()
let flushTimer: ReturnType<typeof setTimeout> | null = null

function scheduleFlush(): void {
  if (flushTimer) return
  flushTimer = setTimeout(() => void flush(), 250)
}

async function flush(): Promise<void> {
  flushTimer = null
  if (queue.size === 0) return
  const all = [...queue]
  queue = new Set()
  const now = Date.now()
  for (const pk of all) tried.set(pk, now)

  // Chunk so no single filter carries too many authors.
  for (let i = 0; i < all.length; i += BATCH) {
    const authors = all.slice(i, i + BATCH)
    try {
      const events = await pool.querySync(PROFILE_RELAYS, { kinds: [0], authors }, { maxWait: 4000 })
      const newest = new Map<string, Event>()
      for (const ev of events) {
        const ex = newest.get(ev.pubkey)
        if (!ex || ev.created_at > ex.created_at) newest.set(ev.pubkey, ev)
      }
      for (const [pk, ev] of newest) {
        try {
          cache[pk] = JSON.parse(ev.content) as ProfileMetadata
        } catch {
          /* malformed kind 0 — leave uncached so it can retry later */
        }
      }
    } catch {
      /* transient — `tried` is set, so it retries after the cooldown */
    }
  }
}

/** Returns the profile (or null while loading); enqueues a batched fetch on demand. */
export function useProfile(pubkey: string): ProfileMetadata | null {
  if (cache[pubkey]) return cache[pubkey]
  const last = tried.get(pubkey) ?? 0
  if (Date.now() - last > RETRY_MS && !queue.has(pubkey)) {
    queue.add(pubkey)
    scheduleFlush()
  }
  return cache[pubkey] ?? null
}

/** Updates the cache so views re-render after the user edits their own profile. */
export function setProfileCache(pubkey: string, profile: ProfileMetadata | null): void {
  if (profile) cache[pubkey] = profile
  else delete cache[pubkey]
  tried.set(pubkey, Date.now())
}

export function displayName(pubkey: string, profile: ProfileMetadata | null): string {
  return profile?.display_name || profile?.name || npubEncode(pubkey).slice(0, 12) + '…'
}

export function avatarUrl(pubkey: string, profile: ProfileMetadata | null): string {
  // Foreign picture URL only if http/https — otherwise Robohash fallback.
  return safeImageUrl(
    profile?.picture,
    `https://robohash.org/${npubEncode(pubkey)}.png?set=set5&bgset=bg2`,
  )
}

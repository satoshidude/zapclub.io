import { npubEncode } from 'nostr-tools/nip19'
import { fetchProfile } from './pool'
import { safeImageUrl } from '../util'
import type { ProfileMetadata } from './types'

// Reactive, lazily filled profile cache for member/chat display.
const cache = $state<Record<string, ProfileMetadata | null>>({})
const pending = new Set<string>()

/** Returns the (possibly still empty) profile and lazy-loads it on demand. */
export function useProfile(pubkey: string): ProfileMetadata | null {
  if (!(pubkey in cache) && !pending.has(pubkey)) {
    pending.add(pubkey)
    fetchProfile(pubkey)
      .then((p) => {
        cache[pubkey] = p
      })
      .catch(() => {
        cache[pubkey] = null
      })
      .finally(() => pending.delete(pubkey))
  }
  return cache[pubkey] ?? null
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

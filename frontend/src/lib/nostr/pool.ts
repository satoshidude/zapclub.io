import { SimplePool } from 'nostr-tools/pool'
import type { Event } from 'nostr-tools/pure'
import type { ProfileMetadata } from './types'

/**
 * Public relays for user profiles (kind:0). Profiles are global, not club-local —
 * they live in the open Nostr network, never on the NIP-29 relay.
 */
export const PROFILE_RELAYS = [
  'wss://relay.damus.io',
  'wss://nos.lol',
  'wss://relay.nostr.band',
  'wss://relay.primal.net',
]

/**
 * Our own NIP-29 relay (khatru + relay29) for club/group data.
 * DNS relay.zapclub.io already points at the server.
 */
export const CLUB_RELAY = 'wss://relay.zapclub.io'

/** Shared pool for profile and club relays. */
export const pool = new SimplePool()

/** Reads the latest kind:0 profile of a pubkey from the public pool. */
export async function fetchProfile(pubkey: string): Promise<ProfileMetadata | null> {
  const event = await pool.get(PROFILE_RELAYS, { kinds: [0], authors: [pubkey] })
  if (!event) return null
  try {
    return JSON.parse(event.content) as ProfileMetadata
  } catch {
    return null
  }
}

/**
 * Publishes an already-signed kind:0 event to the profile relays.
 * Throws if not a single relay accepted it.
 */
export async function publishProfile(event: Event): Promise<void> {
  const results = await Promise.allSettled(pool.publish(PROFILE_RELAYS, event))
  const ok = results.some((r) => r.status === 'fulfilled')
  if (!ok) {
    const reason = results.find((r) => r.status === 'rejected') as PromiseRejectedResult | undefined
    throw new Error(reason?.reason?.toString() ?? 'No relay accepted the event')
  }
}

export function closePool(): void {
  pool.close([...PROFILE_RELAYS, CLUB_RELAY])
}

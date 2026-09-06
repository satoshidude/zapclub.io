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
  'wss://relay.nostr.band', // indexer — kept for broad READ/search coverage
  'wss://relay.primal.net',
  'wss://offchain.pub', // probed: accepts writes + reads → extra write-redundancy
]

/**
 * Our own NIP-29 relay (khatru + relay29) for club/group data.
 * DNS relay.zapclub.io already points at the server.
 */
export const CLUB_RELAY = 'wss://relay.zapclub.io'

/**
 * Public key of our NIP-29 relay (its NIP-11 `pubkey`, derived from RELAY_SECRET_KEY).
 * The relay IS the conductor: it authors now_playing (30100) + the play-log (1313) for every
 * club, so clients accept those events from this key (not from an on-stage DJ) and never write
 * now_playing themselves. Must match the live relay's key — verify via NIP-11 if it ever rotates.
 */
export const CLUB_RELAY_PUBKEY = 'b095f4347bab926917ccd36f371d1741e71d99079bb30562c2227dda29e0b8b1'

/**
 * Relays for NIP-57 zap receipts (kind 9735). The DJ's LNURL server publishes the
 * receipt to the relays named in the zap request; the client reads them from the same
 * list. Public relays — zap receipts are global, not club-scoped (the NIP-29 relay
 * rejects events without an h-tag).
 */
// Lean + write-friendly: the LNURL server publishes the 9735 to exactly these relays (the
// 9734 `relays` tag), and we read from the same list — so every entry must reliably ACCEPT
// the zapper's write and be fast to read. nostr.band dropped (indexer: didn't store receipts,
// connect errors). nsnip.io is whitelist-only for writes, but its own zapper is whitelisted
// there → it publishes reliably to its home relay; we only read from it.
export const ZAP_RELAYS = [
  'wss://relay.damus.io',
  'wss://nos.lol',
  'wss://relay.primal.net',
  'wss://relay.nsnip.io',
]

/**
 * YouTube video ID that loops in the player when no DJ has active tracks.
 * Set to '' to disable the lobby video (shows the static lobby overlay instead).
 */
export const LOBBY_VIDEO_ID = 'w8NRrAOS6s0'

/** Shared pool for profile and club relays. */
export const pool = new SimplePool()

type ClubRelay = Awaited<ReturnType<SimplePool['ensureRelay']>>
type ClubAuthSigner = Parameters<ClubRelay['auth']>[0]
type ClubAuthFailureHandler = (error: Error) => void

interface SafeAuthState {
  originalAuth: ClubRelay['auth']
  flight: Promise<string> | null
}

const CLUB_RELAY_URL = new URL(CLUB_RELAY).toString()
const safeAuthStates = new WeakMap<ClubRelay, SafeAuthState>()
let clubAuthFailureHandler: ClubAuthFailureHandler | null = null
let currentClubRelay: ClubRelay | null = null
let clubRelayGeneration = 0

function errorOf(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value || 'Club relay AUTH failed'))
}

function hardTimeout<T>(promise: Promise<T>, ms: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`club relay AUTH acknowledgement: timeout after ${ms}ms`)), ms)
    promise.then(
      (value) => {
        clearTimeout(timer)
        resolve(value)
      },
      (error) => {
        clearTimeout(timer)
        reject(error)
      },
    )
  })
}

function installSafeAuth(relay: ClubRelay): void {
  if (safeAuthStates.has(relay)) return
  const state: SafeAuthState = { originalAuth: relay.auth.bind(relay), flight: null }
  safeAuthStates.set(relay, state)

  relay.auth = (signer: ClubAuthSigner): Promise<string> => {
    if (currentClubRelay !== relay) {
      return Promise.reject(new Error('Club relay connection was replaced'))
    }
    if (state.flight) return state.flight
    let rejectSigner!: (error: unknown) => void
    const signerFailure = new Promise<string>((_resolve, reject) => {
      rejectSigner = reject
    })
    const wrappedSigner: ClubAuthSigner = async (template) => {
      try {
        return await signer(template)
      } catch (error) {
        rejectSigner(error)
        throw error
      }
    }

    // nostr-tools swallows signer rejection inside AbstractRelay.auth(). Racing the same
    // physical relay-auth flight against our wrapper makes every parallel pool caller settle.
    const raw = Promise.race([state.originalAuth(wrappedSigner), signerFailure])
    let flight!: Promise<string>
    flight = hardTimeout(raw, 20_000).then(
      (result) => {
        if (state.flight === flight) state.flight = null
        return result
      },
      (cause) => {
        if (state.flight === flight) state.flight = null
        const error = errorOf(cause)
        const isCurrentRelay = currentClubRelay === relay
        if (isCurrentRelay) currentClubRelay = null
        // close() synchronously invalidates the private rejected/hanging authPromise; the pool's
        // onclose hook removes this relay instance so an explicit retry receives a clean one.
        void relay.close()
        // A late result from a relay already replaced by an explicit retry must not re-pause
        // the fresh generation.
        if (isCurrentRelay) clubAuthFailureHandler?.(error)
        throw error
      },
    )
    state.flight = flight
    return flight
  }
}

const ensureRelay = pool.ensureRelay.bind(pool)
pool.ensureRelay = async (url, params) => {
  const isClubRelay = new URL(url).toString() === CLUB_RELAY_URL
  const generation = clubRelayGeneration
  const relay = await ensureRelay(url, params)
  if (isClubRelay) {
    if (generation !== clubRelayGeneration) {
      void relay.close()
      throw new Error('Club relay connection was replaced')
    }
    installSafeAuth(relay)
    currentClubRelay = relay
  }
  return relay
}

/** Registers the central lifecycle callback used by groups.ts for AUTH pause/cleanup. */
export function setClubAuthFailureHandler(handler: ClubAuthFailureHandler | null): void {
  clubAuthFailureHandler = handler
}

/** Reads the latest kind:0 profile of a pubkey from the public pool. */
export async function fetchProfile(pubkey: string): Promise<ProfileMetadata | null> {
  const event = await pool.get(PROFILE_RELAYS, { kinds: [0], authors: [pubkey] }, { maxWait: 4000 })
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

/** Closes only the account-bound club-relay connection. */
export function closeClubRelay(): void {
  clubRelayGeneration++
  currentClubRelay = null
  pool.close([CLUB_RELAY])
}

export function closePool(): void {
  closeClubRelay()
  pool.close(PROFILE_RELAYS)
}

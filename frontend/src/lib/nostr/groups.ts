import type { Event, EventTemplate, VerifiedEvent } from 'nostr-tools/pure'
import type { Filter } from 'nostr-tools/filter'
import { minePow } from 'nostr-tools/nip13'
import {
  pool,
  CLUB_RELAY,
  CLUB_RELAY_PUBKEY,
  PROFILE_RELAYS,
  closeClubRelay,
  setClubAuthFailureHandler,
} from './pool'
import { signEvent } from './nostrLogin'
import { auth, onAuthSigningStateChange } from './auth.svelte'
import { resolveZapper } from './zaps.svelte'
import type { Club, ClubMember, ClubConfig } from './types'
import { SESSION_LOOKBACK_MS } from './playlog'
import { sessionEventPrincipal, signSessionEvent } from './sessionSigner'

// NIP-29 group event kinds.
export const KIND_PUT_USER = 9000
export const KIND_REMOVE_USER = 9001 // moderation: remove user from group
export const KIND_EDIT_METADATA = 9002
export const KIND_CREATE_GROUP = 9007
export const KIND_JOIN_REQUEST = 9021
export const KIND_LEAVE_REQUEST = 9022
export const KIND_METADATA = 39000
export const KIND_ADMINS = 39001
export const KIND_MEMBERS = 39002

// Playback and club-state events (all carry the #h group tag).
export const KIND_NOW_PLAYING = 30100 // replaceable per club (d=club): the conductor's track
export const KIND_STAGE = 30102 // replaceable per DJ/club: "I'm a DJ here" heartbeat
export const KIND_QUEUE = 30103 // replaceable per DJ/club: that DJ's track queue
export const KIND_STAGE_KICK = 30106 // replaceable per DJ: owner/mod kicks a DJ off stage
export const KIND_SKIP = 30107 // replaceable per club: owner/mod asks the conductor to skip
export const KIND_PLAY = 1313 // non-replaceable play record (1 per real track start)
export const KIND_CLUB_CONFIG = 30101 // replaceable per club (d=club), OWNER-authored: access/price
export const KIND_PRESENCE = 20100 // ephemeral per-user heartbeat ("I'm here right now")
export const KIND_BROKEN = 20102 // ephemeral "I can't play this track" report (content = videoId)
export const KIND_ZAP_BROADCAST = 20101 // ephemeral, zapper-signed: "I zapped <p> N sats" (club-live zap signal when the DJ's LNURL doesn't publish a 9735 receipt)
export const KIND_FLOOR_REACTION = 20103 // ephemeral floor emote (content = emoji), member-only, h-tagged
export const KIND_MOOD           = 20104 // ephemeral vibe reaction: h=club, pos=track-pos, v=banger|skip
export const KIND_LISTENER_BEAT  = 20105 // anonymous, tab-scoped club-page heartbeat
export const KIND_LISTENER_COUNT = 20106 // relay-signed aggregate of active listener sessions
export const KIND_MEMBER_COUNT = 30112 // relay-signed public member total; no roster identities
export const KIND_AUTODJ = 30105      // replaceable per club (d=club): owner-armed auto-dj playlist
export const KIND_AUTODJ_CTRL = 30111 // replaceable per club (d=club): relay-signed disarm marker

const RELAYS = [CLUB_RELAY]

export type ClubOperationIntent = 'automatic' | 'explicit'

export class ClubAuthPausedError extends Error {
  constructor(message = 'Club relay authentication is paused until the next explicit action') {
    super(message)
    this.name = 'ClubAuthPausedError'
  }
}

type AuthFailureListener = (error: Error) => void
type AuthRecoveryListener = () => void
type AuthPauseListener = () => void
type ClubAuthBlock = { pubkey: string; phase: 'paused' | 'retrying' }

const AUTH_SIGN_TIMEOUT_MS = 15_000
const PUBLISH_TIMEOUT_MS = AUTH_SIGN_TIMEOUT_MS + 5_000
const authFailureListeners = new Set<AuthFailureListener>()
const authRecoveryListeners = new Set<AuthRecoveryListener>()
const authPauseListeners = new Set<AuthPauseListener>()
let authBlock: ClubAuthBlock | null = null
let authGeneration = 0

function errorOf(value: unknown, fallback: string): Error {
  if (value instanceof Error) return value
  const detail = String(value || fallback)
  return new Error(detail)
}

function isConnectionFailure(value: unknown): boolean {
  return typeof value === 'string' && value.toLowerCase().startsWith('connection failure:')
}

function requiresAuthPause(error: Error): boolean {
  return isConnectionFailure(error.message)
    || /\b(?:auth|authenticate|authentication)\b|timed? out|websocket|\bsocket\b|\bconnection\b|\bnetwork\b|\boffline\b/i.test(error.message)
}

function signalAuthFailure(error: Error): void {
  for (const listener of [...authFailureListeners]) listener(error)
}

function pauseClubAuth(pubkey: string | null, generation: number, cause: unknown): Error {
  const error = errorOf(cause, 'Club relay authentication failed')
  if (!pubkey || auth.pubkey !== pubkey || authGeneration !== generation) return error
  if (authBlock?.pubkey === pubkey && authBlock.phase === 'paused') return error
  authBlock = { pubkey, phase: 'paused' }
  signalAuthFailure(error)
  closeClubRelay()
  for (const listener of [...authPauseListeners]) listener()
  return error
}

setClubAuthFailureHandler((error) => {
  pauseClubAuth(auth.pubkey, authGeneration, error)
})

function withHardTimeout<T>(promise: Promise<T>, ms: number, label: string): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`${label}: timeout after ${ms}ms`)), ms)
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

/** True while background club operations must not reopen/authenticate the relay. */
export function isClubAuthPaused(pubkey = auth.pubkey): boolean {
  return !!pubkey && authBlock?.pubkey === pubkey
}

/** Account switch/logout boundary: reject old operations and forget their auth latch. */
export function resetClubAuthState(): void {
  authGeneration++
  authBlock = null
  signalAuthFailure(new Error('Club relay authentication reset'))
}

/** Live subscriptions use this to reopen after the one allowed explicit AUTH retry succeeds. */
export function onClubAuthRecovered(listener: AuthRecoveryListener): () => void {
  authRecoveryListeners.add(listener)
  return () => authRecoveryListeners.delete(listener)
}

/** Public club subscriptions refresh immediately into prompt-free mode after an AUTH pause. */
export function onClubAuthPaused(listener: AuthPauseListener): () => void {
  authPauseListeners.add(listener)
  return () => authPauseListeners.delete(listener)
}

function assertOperationAllowed(intent: ClubOperationIntent): void {
  if (intent === 'automatic' && isClubAuthPaused()) throw new ClubAuthPausedError()
}

function prepareExplicitRetry(intent: ClubOperationIntent): void {
  if (intent !== 'explicit' || !auth.pubkey || authBlock?.pubkey !== auth.pubkey) return
  if (authBlock.phase === 'retrying') return
  authBlock = { pubkey: auth.pubkey, phase: 'retrying' }
  authGeneration++
  closeClubRelay()
  for (const listener of [...authPauseListeners]) listener()
}

function markClubOperationSucceeded(pubkey: string | null): void {
  if (!pubkey || auth.pubkey !== pubkey || authBlock?.pubkey !== pubkey) return
  if (authBlock.phase !== 'retrying') return
  authBlock = null
  for (const listener of [...authRecoveryListeners]) listener()
}

function runClubOperation<T>(
  start: () => Promise<T>,
  timeoutMs: number,
  label: string,
  cleanup?: () => void,
): Promise<T> {
  const pubkey = auth.pubkey
  const generation = authGeneration
  return new Promise<T>((resolve, reject) => {
    let settled = false
    let timer: ReturnType<typeof setTimeout> | null = null
    const finish = (error?: unknown, value?: T) => {
      if (settled) return
      settled = true
      if (timer) clearTimeout(timer)
      authFailureListeners.delete(onAuthFailure)
      cleanup?.()
      if (error !== undefined) reject(errorOf(error, `${label} failed`))
      else resolve(value as T)
    }
    const onAuthFailure = (error: Error) => finish(error)
    authFailureListeners.add(onAuthFailure)
    timer = setTimeout(() => {
      const error = new Error(`${label}: timeout after ${timeoutMs}ms`)
      pauseClubAuth(pubkey, generation, error)
      finish(error)
    }, timeoutMs)

    void Promise.resolve().then(async () => {
      // resetSession/resetClubAuthState may settle this operation before the scheduled
      // start callback runs. Re-check the captured identity boundary immediately before
      // touching the pool so an old account cannot reopen or authenticate a new socket.
      if (settled) return
      if (generation !== authGeneration || pubkey !== auth.pubkey) {
        finish(new Error(`${label}: authentication context changed`))
        return
      }
      try {
        const value = await start()
        finish(undefined, value)
      } catch (cause) {
        const error = errorOf(cause, `${label} failed`)
        if (requiresAuthPause(error)) pauseClubAuth(pubkey, generation, error)
        else markClubOperationSucceeded(pubkey)
        finish(error)
      }
    })
  })
}

/** NIP-42 signer shared by every club query, publish and protected subscription. */
export const clubAuthHandler = async (evt: EventTemplate): Promise<VerifiedEvent> => {
  const pubkey = auth.pubkey
  const generation = authGeneration
  if (!pubkey) throw new ClubAuthPausedError('No authenticated identity for club relay AUTH')
  if (authBlock?.pubkey === pubkey && authBlock.phase === 'paused') throw new ClubAuthPausedError()
  try {
    return await withHardTimeout(
      signEvent(evt, { allowNip46Reconnect: false }) as Promise<VerifiedEvent>,
      AUTH_SIGN_TIMEOUT_MS,
      'club relay AUTH',
    )
  } catch (cause) {
    throw pauseClubAuth(pubkey, generation, cause)
  }
}

/** One-shot relay query that can complete the NIP-42 challenge and replay itself. */
export async function queryClubAuthed(filter: Filter, maxWait = 4000): Promise<Event[]> {
  assertOperationAllowed('automatic')
  const events: Event[] = []
  let sub: ReturnType<typeof pool.subscribeEose> | null = null
  const pubkey = auth.pubkey
  const operation = runClubOperation(
    () => new Promise<Event[]>((resolve, reject) => {
      let querySettled = false
      const finish = (error?: Error) => {
        if (querySettled) return
        querySettled = true
        if (error) reject(error)
        else resolve(events)
      }
      sub = pool.subscribeEose(RELAYS, filter, {
        onauth: clubAuthHandler,
        maxWait,
        onevent: (event) => events.push(event),
        onclose: (reasons) => {
          const reason = reasons.find((entry) =>
            entry && !entry.includes('closed automatically on eose') && !entry.includes('club query cleanup'))
          if (reason) finish(new Error(reason))
          else finish()
        },
      })
    }),
    maxWait + AUTH_SIGN_TIMEOUT_MS + 5_000,
    'club query',
    () => { void sub?.close('club query cleanup') },
  )
  return operation.then((result) => {
    markClubOperationSucceeded(pubkey)
    return result
  })
}

function tagValue(ev: Event, name: string): string | undefined {
  return ev.tags.find((t) => t[0] === name)?.[1]
}

/** NIP-01 replaceable ordering: newest timestamp, then lexicographically lowest event id. */
function preferredReplacement(candidate: Event, current: Event): boolean {
  return candidate.created_at > current.created_at
    || (candidate.created_at === current.created_at && candidate.id < current.id)
}

/** Generates a short, unique group id (NIP-29 d-tag). */
function generateGroupId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(8))
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

async function publishToClub(
  signed: Event,
  intent: ClubOperationIntent,
  failureMessage: string,
): Promise<void> {
  assertOperationAllowed(intent)
  prepareExplicitRetry(intent)
  const pubkey = auth.pubkey
  await runClubOperation(async () => {
    const results = await Promise.allSettled(pool.publish(RELAYS, signed, { onauth: clubAuthHandler }))
    const accepted = results.some((result) => result.status === 'fulfilled' && !isConnectionFailure(result.value))
    if (accepted) return
    const connectionFailure = results.find(
      (result): result is PromiseFulfilledResult<string> => result.status === 'fulfilled' && isConnectionFailure(result.value),
    )
    if (connectionFailure) throw new Error(connectionFailure.value)
    const rejected = results.find((result): result is PromiseRejectedResult => result.status === 'rejected')
    throw errorOf(rejected?.reason, failureMessage)
  }, PUBLISH_TIMEOUT_MS, 'club publish')
  markClubOperationSucceeded(pubkey)
}

/** Publishes an already-signed event to the club relay (with AUTH). Fire-and-forget-ok. */
export async function publishSignedClub(signed: Event, intent: ClubOperationIntent = 'automatic'): Promise<void> {
  await publishToClub(signed, intent, 'Relay rejected the signed event')
}

/**
 * Publishes a connection-bound runtime event signed by the page-session key. The relay accepts
 * this path only for Presence/Stage and only after NIP-42 authenticated the p-tagged main key.
 */
export async function publishSessionClub(
  template: EventTemplate,
  intent: ClubOperationIntent = 'automatic',
): Promise<Event> {
  assertOperationAllowed(intent)
  const signed = signSessionEvent(template)
  await publishToClub(signed, intent, 'Relay rejected the session event')
  return signed
}

/** Signs a template and publishes it to the club relay. Throws on total failure. */
async function publishClub(template: EventTemplate, intent: ClubOperationIntent = 'explicit'): Promise<Event> {
  assertOperationAllowed(intent)
  const signed = await signEvent(template, { allowNip46Reconnect: intent === 'explicit' })
  await publishToClub(signed, intent, 'Relay rejected the event')
  return signed
}

function now(): number {
  return Math.floor(Date.now() / 1000)
}

/**
 * Publishes a PUBLIC kind-1 note to the open relays (e.g. sharing a club link). Goes to the
 * public Nostr network, not the club relay — so the user's followers see it. Needs a signer.
 */
export async function shareNote(content: string, url: string): Promise<void> {
  const signed = await signEvent({
    kind: 1,
    created_at: now(),
    tags: [['t', 'zapclub'], ['r', url]],
    content,
  })
  const results = await Promise.allSettled(pool.publish(PROFILE_RELAYS, signed))
  if (!results.some((r) => r.status === 'fulfilled')) throw new Error('No relay accepted the note')
}

// ── Club lifecycle ──────────────────────────────────────────────────────────

/** Creates a club (NIP-29 create-group + edit-metadata). Returns the group id. */
export async function createClub(
  meta: { name: string; about?: string; picture?: string; link?: string },
  opts?: { private?: boolean },
): Promise<string> {
  const id = generateGroupId()
  await publishClub({ kind: KIND_CREATE_GROUP, created_at: now(), tags: [['h', id]], content: '' })

  const metaTags: string[][] = [['h', id], ['name', meta.name]]
  if (opts?.private) {
    // Private clubs: invite-only join (closed) + content hidden from non-members (private).
    // Single-element tags (relay29 convention) — ['closed',''] is NOT recognized.
    metaTags.push(['closed'], ['private'])
  } else {
    // Default: open + public so anyone can join/listen.
    metaTags.push(['open'], ['public'])
  }
  if (meta.about) metaTags.push(['about', meta.about])
  if (meta.picture) metaTags.push(['picture', meta.picture])
  if (meta.link) metaTags.push(['link', meta.link])
  await publishClub({ kind: KIND_EDIT_METADATA, created_at: now(), tags: metaTags, content: '' })

  return id
}

/**
 * Edits club metadata (name/about/picture/visibility). Only the host/admin may do this —
 * the relay enforces the role. Pass `isPrivate:true` to make a club invite-only and hidden
 * from non-members.
 */
export async function editClub(
  groupId: string,
  meta: { name: string; about?: string; picture?: string; link?: string },
  opts?: { isPrivate?: boolean },
): Promise<void> {
  const metaTags: string[][] = [['h', groupId], ['name', meta.name]]
  if (opts?.isPrivate) {
    metaTags.push(['closed'], ['private'])
  } else {
    metaTags.push(['open'], ['public'])
  }
  if (meta.about) metaTags.push(['about', meta.about])
  if (meta.picture) metaTags.push(['picture', meta.picture])
  if (meta.link) metaTags.push(['link', meta.link])
  await publishClub({ kind: KIND_EDIT_METADATA, created_at: now(), tags: metaTags, content: '' })
}

/**
 * Sets the club access config (kind 30101, OWNER only). For paid clubs the entry lightning
 * address is resolved to its NIP-57 zapper pubkey and stored too, so the relay can verify
 * entry receipts. Open clubs store access=open (price 0).
 */
export async function setClubConfig(
  groupId: string,
  cfg: { access: 'open' | 'paid'; price: number; lud16: string; featured?: boolean },
): Promise<void> {
  const tags: string[][] = [
    ['h', groupId],
    ['d', groupId],
    ['access', cfg.access],
  ]
  if (cfg.access === 'paid') {
    const zapper = cfg.lud16 ? await resolveZapper(cfg.lud16) : ''
    tags.push(['price', String(Math.max(0, Math.floor(cfg.price)))], ['lud16', cfg.lud16], ['zapper', zapper])
  }
  if (cfg.featured) tags.push(['featured', '1'])
  await publishClub({ kind: KIND_CLUB_CONFIG, created_at: now(), tags, content: '' })
}

/** Parses a club-config event (kind 30101). Caller must verify the author is the owner. */
export function parseClubConfig(ev: Event): ClubConfig {
  const tag = (n: string) => ev.tags.find((t) => t[0] === n)?.[1] ?? ''
  return {
    access: tag('access') === 'paid' ? 'paid' : 'open',
    price: Number(tag('price')) || 0,
    lud16: tag('lud16'),
    zapper: tag('zapper'),
    featured: tag('featured') === '1',
  }
}

const JOIN_POW_DIFFICULTY = 15

/**
 * Join request (NIP-29 kind 9021). Open clubs auto-add the member on the relay. For a PAID
 * club, pass the 9735 entry receipt as `proof` — the relay verifies it before admitting.
 * Mines NIP-13 PoW (difficulty 15, ~100–500 ms) before signing so the relay accepts the event.
 */
export async function joinClub(groupId: string, proof?: Event): Promise<void> {
  const tags: string[][] = [['h', groupId]]
  if (proof) tags.push(['proof', JSON.stringify(proof)])
  const template: EventTemplate = { kind: KIND_JOIN_REQUEST, created_at: now(), tags, content: '' }
  const pk = auth.pubkey
  if (!pk) {
    await publishClub(template)
    return
  }
  // Mine PoW synchronously — yield first so any loading indicator the caller set can render.
  await new Promise<void>((r) => setTimeout(r, 0))
  const mined = minePow({ ...template, pubkey: pk }, JOIN_POW_DIFFICULTY)
  // Strip pubkey+id (signEvent adds pubkey back and recomputes id from the nonce-bearing tags).
  const { pubkey: _pk, id: _id, ...minedTemplate } = mined
  await publishClub(minedTemplate as EventTemplate)
}

export async function leaveClub(groupId: string): Promise<void> {
  await publishClub({ kind: KIND_LEAVE_REQUEST, created_at: now(), tags: [['h', groupId]], content: '' })
}

/**
 * Club-live zap broadcast (kind 20101, ephemeral, h-tagged). A NIP-57 9735 receipt is the
 * hard proof of a zap — but some LNURL providers (e.g. nsnip.io) never publish one, so other
 * clients would never see the zap. After the zapper confirms payment we also emit this
 * lightweight club event so everyone in the room gets the animation + session score
 * immediately, regardless of the DJ's provider. Trust: self-reported by the zapper (an
 * ephemeral social signal, like applause); `bolt11` dedup in ingestZapBroadcast prevents
 * the zapper's local credit and broadcast echo from counting twice.
 */
export async function publishZapBroadcast(
  club: string,
  dj: string,
  sats: number,
  invoice?: string,
): Promise<void> {
  if (!club || !dj || sats <= 0) return
  const tags: string[][] = [
    ['h', club],
    ['p', dj],
    ['amount', String(sats)],
  ]
  if (invoice) tags.push(['bolt11', invoice])
  await publishClub({ kind: KIND_ZAP_BROADCAST, created_at: now(), tags, content: '' })
}

/**
 * Reports the running track as unplayable (kind 20102, ephemeral): deleted/region-locked/
 * embedding-off — something the relay can't detect itself. The relay (the conductor) skips the
 * track when an authorized reporter (owner/mod/playing-DJ) OR a quorum of distinct members
 * reports it. Members only (relay write-protection); not a moderation action.
 */
export async function reportBrokenTrack(groupId: string, videoId: string): Promise<void> {
  if (!videoId) return
  await publishClub({
    kind: KIND_BROKEN,
    created_at: now(),
    tags: [['h', groupId]],
    content: videoId,
  }, 'automatic')
}

// ── Moderation (host/moderator only — the relay enforces the role) ────────────

/** Removes a user from the club (NIP-29 kind 9001). */
export async function removeUser(groupId: string, pubkey: string): Promise<void> {
  await publishClub({
    kind: KIND_REMOVE_USER,
    created_at: now(),
    tags: [
      ['h', groupId],
      ['p', pubkey],
    ],
    content: '',
  })
}

/** Appoints a moderator (NIP-29 kind 9000 put-user with the "moderator" role). */
export async function addModerator(groupId: string, pubkey: string): Promise<void> {
  await publishClub({
    kind: KIND_PUT_USER,
    created_at: now(),
    tags: [
      ['h', groupId],
      ['p', pubkey, 'moderator'],
    ],
    content: '',
  })
}

/**
 * Adds a plain member (NIP-29 kind 9000 put-user, no role). Used by owners to approve
 * join-requests or to invite users directly in invite-only clubs.
 */
export async function addMember(groupId: string, pubkey: string): Promise<void> {
  await publishClub({
    kind: KIND_PUT_USER,
    created_at: now(),
    tags: [
      ['h', groupId],
      ['p', pubkey],
    ],
    content: '',
  })
}

/**
 * Fetches pending join-requests (kind 9021) for a club — for the owner's approval panel in
 * invite-only clubs. Deduplicates to the newest request per pubkey and removes anyone already
 * in the provided members list so approved requests don't linger.
 */
export async function fetchJoinRequests(
  groupId: string,
  existingMembers: string[] = [],
): Promise<{ pubkey: string; createdAt: number }[]> {
  const evs = await queryClubAuthed(
    { kinds: [KIND_JOIN_REQUEST], '#h': [groupId] },
    4000,
  )
  // Keep newest request per pubkey.
  const map = new Map<string, number>()
  for (const ev of evs) {
    const prev = map.get(ev.pubkey) ?? 0
    if (ev.created_at > prev) map.set(ev.pubkey, ev.created_at)
  }
  const memberSet = new Set(existingMembers)
  return [...map.entries()]
    .filter(([pk]) => !memberSet.has(pk))
    .map(([pubkey, createdAt]) => ({ pubkey, createdAt }))
    .sort((a, b) => a.createdAt - b.createdAt)
}

/** Kicks a DJ off the stage (owner/moderator). Clients only honor kicks from admins. */
export async function kickFromStage(groupId: string, djPubkey: string): Promise<void> {
  await publishClub({
    kind: KIND_STAGE_KICK,
    created_at: now(),
    tags: [
      ['h', groupId],
      ['d', djPubkey],
      ['p', djPubkey],
    ],
    content: '',
  })
}

/**
 * Publishes a play record (one real track start), the SHARED, conductor-independent source
 * of round-robin progress (see playlog.ts). `pos` = the round-robin position; `loop` = the
 * rotation epoch (bumped by advance() on exhaustion so every client agrees on a replay).
 */
export async function publishPlay(
  groupId: string,
  djPubkey: string,
  videoId: string,
  startedAt: number,
  pos: number,
  loop: number,
): Promise<void> {
  await publishClub({
    kind: KIND_PLAY,
    created_at: now(),
    tags: [
      ['h', groupId],
      ['p', djPubkey],
      ['started_at', String(startedAt)],
      ['pos', String(pos)],
      ['loop', String(loop)],
    ],
    content: videoId,
  })
}

// ── Read / parse ──────────────────────────────────────────────────────────────

export function parseClubMetadata(ev: Event): Club {
  const has = (name: string) => ev.tags.some((t) => t[0] === name)
  return {
    id: tagValue(ev, 'd') ?? '',
    name: tagValue(ev, 'name') ?? tagValue(ev, 'd') ?? 'Untitled',
    about: tagValue(ev, 'about'),
    picture: tagValue(ev, 'picture'),
    open: has('open'),
    isPublic: has('public'),
    closed: has('closed'),
    isPrivate: has('private'),
    link: tagValue(ev, 'link'),
  }
}

export function parseMembers(ev: Event): ClubMember[] {
  return ev.tags
    .filter((t) => t[0] === 'p' && t[1])
    .map((t) => ({ pubkey: t[1], roles: t.slice(2) }))
}

/** Admin pubkeys from kind:39001 (first = host/creator). */
export function parseAdmins(ev: Event): string[] {
  return ev.tags.filter((t) => t[0] === 'p' && t[1]).map((t) => t[1])
}

/**
 * The club OWNER (creator). Identified by the `owner` role in the 39001 admins event —
 * NOT by tag position: relay29 does not guarantee the owner is the first p-tag (a
 * moderator can be listed first), and the order can differ between clients. Picking by
 * position made different clients disagree on the owner → wrong conductor (owner-override)
 * → duplicate now_playing writers. Falls back to the first admin if no role is tagged.
 */
export function parseOwner(ev: Event): string {
  const ownerTag = ev.tags.find((t) => t[0] === 'p' && t[1] && t.slice(2).includes('owner'))
  return ownerTag?.[1] ?? ev.tags.find((t) => t[0] === 'p' && t[1])?.[1] ?? ''
}

/** List all clubs from public metadata, owner and access configuration events. */
export async function listClubs(): Promise<Club[]> {
  const [metaEvents, adminEvents, configEvents] = await Promise.all([
    pool.querySync(RELAYS, { kinds: [KIND_METADATA] }, { maxWait: 4000 }),
    pool.querySync(RELAYS, { kinds: [KIND_ADMINS] }, { maxWait: 4000 }),
    pool.querySync(RELAYS, { kinds: [KIND_CLUB_CONFIG] }, { maxWait: 4000 }),
  ])
  // Owner = first admin (creator) per club (kind 39001).
  const owners = new Map<string, string>()
  for (const ev of adminEvents) {
    const id = tagValue(ev, 'd')
    if (id) owners.set(id, parseOwner(ev))
  }
  // Access config (30101) — newest OWNER-authored per club only (others ignored).
  const configs = new Map<string, ClubConfig>()
  const configAt = new Map<string, number>()
  for (const ev of configEvents) {
    const id = tagValue(ev, 'd')
    if (!id || ev.pubkey !== owners.get(id)) continue
    if (ev.created_at < (configAt.get(id) ?? 0)) continue
    configAt.set(id, ev.created_at)
    configs.set(id, parseClubConfig(ev))
  }
  const clubs = metaEvents
    .map(parseClubMetadata)
    .filter((c) => c.id)
    .map((c) => ({
      ...c,
      owner: owners.get(c.id) || undefined,
      access: configs.get(c.id)?.access ?? 'open',
      price: configs.get(c.id)?.price ?? 0,
      featured: !!configs.get(c.id)?.featured,
    }))

  // The private 39002 roster stays out of this query. Public aggregate counts
  // come from the separate relay-authored KIND_MEMBER_COUNT event.
  clubs.sort((a, b) => {
    if (a.featured !== b.featured) return a.featured ? -1 : 1
    return a.name.localeCompare(b.name)
  })
  return clubs
}

/** Club ids that are LIVE right now: a fresh relay-authored now_playing (kind 30100) means DJs
 *  are on stage and streaming. The window matches the client's lobby-fallback (150s) — the relay
 *  republishes now_playing every ~15s while playing, then it goes stale.
 *  Also checks fresh kind 30102 stage heartbeats (60 s window) so that a bot-only stage
 *  (or any stage where the conductor hasn't published a fresh now_playing yet) still counts. */
export async function fetchLiveClubIds(clubIds: string[]): Promise<Set<string>> {
  const live = new Set<string>()
  if (clubIds.length === 0) return live
  const now = Date.now()
  const [npEvs, stageEvs] = await Promise.all([
    pool.querySync(RELAYS, { kinds: [30100], '#h': clubIds }, { maxWait: 4000 }),
    pool.querySync(RELAYS, { kinds: [30102], '#h': clubIds }, { maxWait: 3000 }),
  ])
  for (const ev of npEvs) {
    const h = ev.tags.find((t) => t[0] === 'h')?.[1]
    const sent = Number(ev.tags.find((t) => t[0] === 'sent_at')?.[1]) || 0
    if (h && now - sent < 150_000) live.add(h)
  }
  for (const ev of stageEvs) {
    const h = ev.tags.find((t) => t[0] === 'h')?.[1]
    if (h && now - ev.created_at * 1000 < 60_000) live.add(h)
  }
  return live
}

export interface OnAirClubDj {
  dj: string
  sentAt: number
}

export interface OnAirClubTrack extends OnAirClubDj {
  title: string
}

/**
 * Select the current DJ for every club with a fresh, actively playing
 * now_playing event. Unlike `fetchLiveClubIds`, stage heartbeats alone do not
 * count: the public directory only calls a club "on air" while it is actually
 * broadcasting a track.
 */
export function selectOnAirClubDjs(
  events: Event[],
  clubIds: string[],
  nowMs = Date.now(),
): Map<string, string> {
  return new Map(
    [...selectOnAirClubTracks(events, clubIds, nowMs)].map(([clubId, live]) => [clubId, live.dj]),
  )
}

/** Select the current track and DJ for every club that is actively on air. */
export function selectOnAirClubTracks(
  events: Event[],
  clubIds: string[],
  nowMs = Date.now(),
): Map<string, OnAirClubTrack> {
  const allowed = new Set(clubIds)
  const latest = new Map<string, OnAirClubTrack>()

  for (const ev of events) {
    const clubId = tagValue(ev, 'h')
    const sentAt = Number(tagValue(ev, 'sent_at')) || 0
    const dj = tagValue(ev, 'dj')
    if (!clubId || !allowed.has(clubId) || !dj) continue
    if (tagValue(ev, 'status') === 'paused' || sentAt <= 0 || nowMs - sentAt >= 150_000) continue
    if (sentAt <= (latest.get(clubId)?.sentAt ?? 0)) continue
    latest.set(clubId, { dj, sentAt, title: ev.content.trim() })
  }

  return latest
}

/** Current on-air DJ by club id. */
export async function fetchOnAirClubDjs(clubIds: string[]): Promise<Map<string, string>> {
  const tracks = await fetchOnAirClubTracks(clubIds)
  return new Map([...tracks].map(([clubId, live]) => [clubId, live.dj]))
}

/** Current on-air track and DJ by club id. */
export async function fetchOnAirClubTracks(clubIds: string[]): Promise<Map<string, OnAirClubTrack>> {
  if (clubIds.length === 0) return new Map()
  const events = await pool.querySync(
    RELAYS,
    { kinds: [KIND_NOW_PLAYING], '#h': clubIds },
    { maxWait: 4000 },
  )
  return selectOnAirClubTracks(events, clubIds)
}

export interface ClubMemberCount {
  count: number
  sentAt: number
}

/** Validate and select the newest relay-authored public member total per club. */
export function selectClubMemberCounts(
  events: Event[],
  clubIds: string[],
): Map<string, ClubMemberCount> {
  const allowed = new Set(clubIds)
  const latest = new Map<string, ClubMemberCount>()
  for (const event of events) {
    if (event.kind !== KIND_MEMBER_COUNT || event.pubkey !== CLUB_RELAY_PUBKEY) continue
    const clubId = tagValue(event, 'h')
    const count = Number(tagValue(event, 'count'))
    const sentAt = Number(tagValue(event, 'sent_at'))
    if (!clubId || !allowed.has(clubId)) continue
    if (!Number.isSafeInteger(count) || count < 0 || !Number.isSafeInteger(sentAt) || sentAt <= 0) continue
    if (sentAt < (latest.get(clubId)?.sentAt ?? 0)) continue
    latest.set(clubId, { count, sentAt })
  }
  return latest
}

export async function fetchClubMemberCounts(clubIds: string[]): Promise<Map<string, ClubMemberCount>> {
  if (clubIds.length === 0) return new Map()
  const events = await pool.querySync(
    RELAYS,
    { kinds: [KIND_MEMBER_COUNT], '#h': clubIds },
    { maxWait: 4000 },
  )
  return selectClubMemberCounts(events, clubIds)
}

export function subscribeClubMemberCounts(
  clubIds: string[],
  onCount: (clubId: string, count: number, sentAt: number) => void,
): () => void {
  if (clubIds.length === 0) return () => {}
  const sub = pool.subscribeMany(
    RELAYS,
    { kinds: [KIND_MEMBER_COUNT], '#h': clubIds },
    {
      onevent(event) {
        const next = selectClubMemberCounts([event], clubIds)
        for (const [clubId, value] of next) onCount(clubId, value.count, value.sentAt)
      },
    },
  )
  return () => sub.close()
}

const DIRECTORY_STAGE_STALE_MS = 300_000

interface StageClubDj {
  pubkey: string
  since: number
}

/**
 * Select the first active stage DJ per club from replaceable stage events.
 * The newest event per relay-bound principal wins, so a later `off` event removes that DJ.
 */
export function selectOnStageClubDjs(
  events: Event[],
  clubIds: string[],
  nowMs = Date.now(),
): Map<string, string> {
  const allowed = new Set(clubIds)
  const newest = new Map<string, Event>()

  for (const ev of events) {
    const clubId = tagValue(ev, 'h')
    if (!clubId || !allowed.has(clubId)) continue
    const principal = sessionEventPrincipal(ev)
    const key = `${clubId}:${principal}`
    const previous = newest.get(key)
    if (!previous || preferredReplacement(ev, previous)) newest.set(key, ev)
  }

  const byClub = new Map<string, StageClubDj[]>()
  for (const ev of newest.values()) {
    const clubId = tagValue(ev, 'h')!
    if (ev.content === 'off' || nowMs - ev.created_at * 1000 >= DIRECTORY_STAGE_STALE_MS) continue
    const since = Number(tagValue(ev, 'since')) || ev.created_at
    const djs = byClub.get(clubId) ?? []
    djs.push({ pubkey: sessionEventPrincipal(ev), since })
    byClub.set(clubId, djs)
  }

  return new Map(
    [...byClub].map(([clubId, djs]) => {
      djs.sort((a, b) => a.since - b.since || a.pubkey.localeCompare(b.pubkey))
      return [clubId, djs[0].pubkey]
    }),
  )
}

/** First active stage DJ by club id, including DJs waiting between tracks. */
export async function fetchOnStageClubDjs(clubIds: string[]): Promise<Map<string, string>> {
  if (clubIds.length === 0) return new Map()
  const events = await pool.querySync(
    RELAYS,
    { kinds: [KIND_STAGE], '#h': clubIds },
    { maxWait: 4000 },
  )
  return selectOnStageClubDjs(events, clubIds)
}

/**
 * Opens a live subscription for kind 20100 (presence beats) across all given clubs.
 * Calls `onBeat(clubId, pubkey, ms)` on every incoming beat.
 * Returns a cleanup function — call it when the component unmounts.
 */
export function subscribeClubPresence(
  clubIds: string[],
  onBeat: (clubId: string, pubkey: string, ms: number) => void,
): () => void {
  if (clubIds.length === 0) return () => {}
  const sub = pool.subscribeMany(
    RELAYS,
    { kinds: [KIND_PRESENCE], '#h': clubIds },
    {
      onevent(ev) {
        const h = ev.tags.find((t) => t[0] === 'h')?.[1]
        if (h) onBeat(h, sessionEventPrincipal(ev), ev.created_at * 1000)
      },
    },
  )
  return () => sub.close()
}

/** Every distinct pubkey visible as a member or admin/owner of any queryable club. */
export async function fetchClubPeople(): Promise<string[]> {
  const [members, admins] = await Promise.all([
    pool.querySync(RELAYS, { kinds: [KIND_MEMBERS] }, { maxWait: 4000 }),
    pool.querySync(RELAYS, { kinds: [KIND_ADMINS] }, { maxWait: 4000 }),
  ])
  const set = new Set<string>()
  for (const ev of [...members, ...admins]) {
    for (const t of ev.tags) if (t[0] === 'p' && t[1]) set.add(t[1])
  }
  return [...set]
}

export interface MyClub {
  id: string
  name: string
  picture?: string
  roles: string[]
}

/**
 * Clubs the user is a member of: members events (39002) carrying them as `p` → group
 * ids, then the metadata (name/picture). The relay allows the `#p` query on 39002.
 */
export async function fetchMyClubs(pubkey: string): Promise<MyClub[]> {
  const memberEvents = await queryClubAuthed({ kinds: [KIND_MEMBERS], '#p': [pubkey] })
  const roleById = new Map<string, string[]>()
  for (const ev of memberEvents) {
    const id = tagValue(ev, 'd')
    if (!id) continue
    const mine = ev.tags.find((t) => t[0] === 'p' && t[1] === pubkey)
    roleById.set(id, mine ? mine.slice(2) : [])
  }
  const ids = [...roleById.keys()]
  if (ids.length === 0) return []
  const metaEvents = await pool.querySync(
    RELAYS,
    { kinds: [KIND_METADATA], '#d': ids },
    { maxWait: 4000 },
  )
  const metaById = new Map<string, Club>()
  for (const ev of metaEvents) {
    const c = parseClubMetadata(ev)
    if (c.id) metaById.set(c.id, c)
  }
  return ids.map((id) => {
    const m = metaById.get(id)
    return { id, name: m?.name ?? id, picture: m?.picture, roles: roleById.get(id) ?? [] }
  })
}

// A DJ's stage event (30102) is considered "live" within this window of its last
// heartbeat — matches the stage's own sticky STALE_MS.
const STAGE_STALE_MS = 300_000

export interface UserClubActivity {
  /** Every club the user is a member of (incl. ones they host), as full cards, ordered:
   *  current/last-DJ'd first, then live, then hosted, then by member count. */
  memberOf: Club[]
  /** Clubs the user is live on stage in right now (fresh, non-"off" stage event). */
  djingIn: Club[]
  /** Clubs the user owns (hosts). */
  hosting: Club[]
  /** The club to pin at the very top: where they're DJing now, else where they last DJ'd
   *  (newest 30102 by this author) — provided they're still a member. Null if neither. */
  topClubId: string | null
  /** club id → the user's NIP-29 roles on it (from 39002), e.g. ['dj'] / ['moderator']. */
  rolesById: Record<string, string[]>
}

/**
 * A user's club activity for their profile: every club they're a MEMBER of (public or private),
 * with the club they're currently DJing in — or last DJ'd in — pinned to the top. Also surfaces
 * which they host and their per-club roles, so the profile can badge each row.
 */
export async function fetchUserClubActivity(pubkey: string): Promise<UserClubActivity> {
  const [clubs, memberEvents] = await Promise.all([
    listClubs(),
    queryClubAuthed({ kinds: [KIND_MEMBERS], '#p': [pubkey] }),
  ])
  const byId = new Map(clubs.map((c) => [c.id, c]))

  // Membership (39002 carrying this pubkey) → ids + roles. The owner is implicitly a member.
  const rolesById: Record<string, string[]> = {}
  const memberIds = new Set<string>()
  for (const ev of memberEvents) {
    const id = tagValue(ev, 'd')
    if (!id) continue
    memberIds.add(id)
    const mine = ev.tags.find((t) => t[0] === 'p' && t[1] === pubkey)
    rolesById[id] = mine ? mine.slice(2) : []
  }
  const hosting = clubs.filter((c) => c.owner === pubkey)
  for (const c of hosting) memberIds.add(c.id)

  // Stage events (30102) are GROUP-SCOPED on the relay — a query by `authors` alone returns
  // nothing (relay29 only serves content reads filtered by the group `#h`). So query the user's
  // own club h-tags, then keep their own events. (A DJ is always a member, so memberIds covers it.)
  const ids = [...memberIds]
  const stageEvents = ids.length
    ? (await pool.querySync(RELAYS, { kinds: [KIND_STAGE], '#h': ids }, { maxWait: 4000 })).filter(
        (ev) => sessionEventPrincipal(ev) === pubkey,
      )
    : []

  // Newest stage event per club (live if fresh + not "off") + the single newest overall (the
  // club they currently / most recently DJ'd in, regardless of off/stale).
  const newestByGroup = new Map<string, Event>()
  let lastStage: Event | null = null
  for (const ev of stageEvents) {
    const h = tagValue(ev, 'h')
    if (!h) continue
    const ex = newestByGroup.get(h)
    if (!ex || preferredReplacement(ev, ex)) newestByGroup.set(h, ev)
    if (!lastStage || preferredReplacement(ev, lastStage)) lastStage = ev
  }
  const nowMs = Date.now()
  const liveIds = new Set<string>()
  const djingIn: Club[] = []
  for (const [h, ev] of newestByGroup) {
    if (ev.content === 'off' || nowMs - ev.created_at * 1000 >= STAGE_STALE_MS) continue
    const c = byId.get(h)
    if (c) {
      djingIn.push(c)
      liveIds.add(h)
    }
  }

  const lastH = lastStage ? tagValue(lastStage, 'h') : null
  const topClubId = lastH && byId.has(lastH) && memberIds.has(lastH) ? lastH : null

  const ownerIds = new Set(hosting.map((c) => c.id))
  const rank = (c: Club): number =>
    c.id === topClubId ? 0 : liveIds.has(c.id) ? 1 : ownerIds.has(c.id) ? 2 : 3
  const memberOf = [...memberIds]
    .map((id) => byId.get(id))
    .filter((c): c is Club => !!c)
    .sort((a, b) => rank(a) - rank(b) || (b.memberCount ?? 0) - (a.memberCount ?? 0))

  return { memberOf, djingIn, hosting, topClubId, rolesById }
}

/**
 * Snapshot of all DJ queues (kind 30103) for a club — one replaceable event per DJ. Used by
 * the periodic queue re-sync (queue.svelte) as a reliability net against missed live
 * subscription events (reconnects, relay restarts), so the round-robin always sees the
 * current playlists. Read-only; ingestion stays idempotent (newest created_at wins).
 */
export async function fetchClubQueues(groupId: string): Promise<Event[]> {
  return pool.querySync(RELAYS, { kinds: [KIND_QUEUE], '#h': [groupId] }, { maxWait: 4000 })
}

/** Fetch the club's play-log (kind 1313) since `sinceMs` — the shared round-robin progress
 *  (playlog.ts reconstructs the played-set from it). Bounded by `since` to keep reads small. */
export async function fetchClubPlayLog(groupId: string, sinceMs: number): Promise<Event[]> {
  return pool.querySync(
    RELAYS,
    { kinds: [KIND_PLAY], '#h': [groupId], since: Math.floor(sinceMs / 1000) },
    { maxWait: 4000 },
  )
}

/** Fetch single club metadata (by d-tag). */
export async function fetchClub(groupId: string): Promise<Club | null> {
  const ev = await pool.get(RELAYS, { kinds: [KIND_METADATA], '#d': [groupId] }, { maxWait: 4000 })
  return ev ? parseClubMetadata(ev) : null
}

// ── Subscriptions (relay29 rule: metadata separate from content) ──────────────

export interface ClubSubHandlers {
  onMeta?: (ev: Event) => void
  onMembers?: (ev: Event) => void
  onMembershipChange?: (ev: Event) => void
  onAdmins?: (ev: Event) => void
  onNowPlaying?: (ev: Event) => void
  onStage?: (ev: Event) => void
  onStageKick?: (ev: Event) => void
  onQueue?: (ev: Event) => void
  onSkip?: (ev: Event) => void
  onConfig?: (ev: Event) => void
  onPresence?: (ev: Event) => void
  onZapBroadcast?: (ev: Event) => void
  onPlay?: (ev: Event) => void
  onEmote?: (ev: Event) => void
  onAutoDJ?: (ev: Event) => void
  onAutoDJCtrl?: (ev: Event) => void
  onMood?: (ev: Event) => void
  onListenerCount?: (ev: Event) => void
  onMemberCount?: (ev: Event) => void
  /** Called once after all stored events have been delivered (EOSE). */
  onEose?: () => void
}

/**
 * Subscribes to a club. Two subscriptions, because relay29 does not allow mixing
 * metadata kinds with others: metadata by #d, content by #h.
 * Returns a cleanup function.
 */
export function subscribeClub(groupId: string, h: ClubSubHandlers): () => void {
  const metaFilter: Filter = { kinds: [KIND_METADATA, KIND_ADMINS], '#d': [groupId] }
  const membersFilter: Filter = { kinds: [KIND_MEMBERS], '#d': [groupId] }
  const contentFilter: Filter = {
    kinds: [
      KIND_NOW_PLAYING,
      KIND_STAGE,
      KIND_STAGE_KICK,
      KIND_QUEUE,
      KIND_SKIP,
      KIND_CLUB_CONFIG,
      KIND_ZAP_BROADCAST,
      KIND_FLOOR_REACTION,
      KIND_MOOD,
      KIND_LISTENER_COUNT,
      KIND_MEMBER_COUNT,
      KIND_AUTODJ,
      KIND_AUTODJ_CTRL,
    ],
    '#h': [groupId],
  }
  // The play-log (kind 1313, one event per track ever started) grows without bound. In the
  // shared content filter its newest records filled the relay's result window and crowded
  // out everything older, including long-lived replaceable state (e.g. Auto DJ 30105).
  // Own subscription, bounded to the same session lookback
  // ingestPlay keeps anyway.
  const playFilter: Filter = {
    kinds: [KIND_PLAY],
    '#h': [groupId],
    since: Math.floor((Date.now() - SESSION_LOOKBACK_MS) / 1000),
  }

  let active = true
  let observedPubkey = auth.pubkey
  let observedCanSign = auth.canSign
  let subscriptions: Array<{ close: (reason?: string) => unknown }> = []
  const closeSubscriptions = () => {
    const current = subscriptions
    subscriptions = []
    for (const subscription of current) void subscription.close('club subscription refresh')
  }
  const openSubscriptions = () => {
    if (!active) return
    closeSubscriptions()
    const automaticAuth = auth.canSign && !isClubAuthPaused() ? clubAuthHandler : undefined
    const authOptions = automaticAuth ? { onauth: automaticAuth } : {}

    subscriptions.push(pool.subscribe(RELAYS, metaFilter, {
      ...authOptions,
      onevent(ev) {
        if (ev.kind === KIND_METADATA) h.onMeta?.(ev)
        else if (ev.kind === KIND_ADMINS) h.onAdmins?.(ev)
      },
    }))

    // Member roster + presence are a separate authenticated social layer. During an AUTH
    // pause they stay closed, so a timer or component remount cannot prompt the signer.
    if (auth.canSign && !isClubAuthPaused()) {
      subscriptions.push(pool.subscribe(RELAYS, membersFilter, {
        onauth: clubAuthHandler,
        onevent(ev) {
          if (ev.kind === KIND_MEMBERS) h.onMembers?.(ev)
        },
      }))
      subscriptions.push(pool.subscribe(RELAYS, { kinds: [KIND_PRESENCE], '#h': [groupId] }, {
        onauth: clubAuthHandler,
        onevent(ev) {
          if (ev.kind === KIND_PRESENCE) h.onPresence?.(ev)
        },
      }))
      // A short live moderation tail lets the affected browser react immediately when this
      // account joins, leaves or is kicked.
      subscriptions.push(pool.subscribe(RELAYS, {
        kinds: [KIND_PUT_USER, KIND_REMOVE_USER],
        '#h': [groupId],
        '#p': [auth.pubkey!],
        since: Math.floor(Date.now() / 1000) - 60,
      }, {
        onauth: clubAuthHandler,
        onevent: (ev) => h.onMembershipChange?.(ev),
      }))
    }

    subscriptions.push(pool.subscribe(RELAYS, contentFilter, {
      ...authOptions,
      oneose: () => h.onEose?.(),
      onevent(ev) {
        if (ev.kind === KIND_NOW_PLAYING) h.onNowPlaying?.(ev)
        else if (ev.kind === KIND_STAGE) h.onStage?.(ev)
        else if (ev.kind === KIND_STAGE_KICK) h.onStageKick?.(ev)
        else if (ev.kind === KIND_QUEUE) h.onQueue?.(ev)
        else if (ev.kind === KIND_SKIP) h.onSkip?.(ev)
        else if (ev.kind === KIND_CLUB_CONFIG) h.onConfig?.(ev)
        else if (ev.kind === KIND_ZAP_BROADCAST) h.onZapBroadcast?.(ev)
        else if (ev.kind === KIND_FLOOR_REACTION) h.onEmote?.(ev)
        else if (ev.kind === KIND_AUTODJ) h.onAutoDJ?.(ev)
        else if (ev.kind === KIND_AUTODJ_CTRL) h.onAutoDJCtrl?.(ev)
        else if (ev.kind === KIND_MOOD) h.onMood?.(ev)
        else if (ev.kind === KIND_LISTENER_COUNT) h.onListenerCount?.(ev)
        else if (ev.kind === KIND_MEMBER_COUNT) h.onMemberCount?.(ev)
      },
    }))

    subscriptions.push(pool.subscribe(RELAYS, playFilter, {
      ...authOptions,
      onevent(ev) {
        if (ev.kind === KIND_PLAY) h.onPlay?.(ev)
      },
    }))
  }

  openSubscriptions()
  const stopPause = onClubAuthPaused(openSubscriptions)
  const stopRecovery = onClubAuthRecovered(openSubscriptions)
  const stopSigningState = onAuthSigningStateChange((next) => {
    if (next.pubkey === observedPubkey && next.canSign === observedCanSign) return
    const switchedAccounts = observedPubkey !== null
      && next.pubkey !== null
      && next.pubkey !== observedPubkey
    observedPubkey = next.pubkey
    observedCanSign = next.canSign
    // goHome() has already scheduled this old club view for unmount. Reopening its
    // subscriptions under the new account could otherwise trigger an unnecessary AUTH.
    if (switchedAccounts) return
    openSubscriptions()
  })
  return () => {
    active = false
    stopPause()
    stopRecovery()
    stopSigningState()
    closeSubscriptions()
  }
}

export { publishClub }

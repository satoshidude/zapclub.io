import type { Event, EventTemplate, VerifiedEvent } from 'nostr-tools/pure'
import type { Filter } from 'nostr-tools/filter'
import { pool, CLUB_RELAY, PROFILE_RELAYS } from './pool'
import { signEvent } from './nostrLogin'
import { resolveZapper } from './zaps.svelte'
import type { Club, ClubMember, ClubConfig } from './types'

// NIP-29 event kinds (Phase 2 subset — DJ/stage/queue kinds come in Phase 3).
export const KIND_CHAT = 9
export const KIND_PUT_USER = 9000
export const KIND_REMOVE_USER = 9001 // moderation: remove user from group
export const KIND_EDIT_METADATA = 9002
export const KIND_DELETE_EVENT = 9005 // moderation: delete an event (e.g. chat)
export const KIND_CREATE_GROUP = 9007
export const KIND_JOIN_REQUEST = 9021
export const KIND_LEAVE_REQUEST = 9022
export const KIND_METADATA = 39000
export const KIND_ADMINS = 39001
export const KIND_MEMBERS = 39002

// Phase 3 — playback content events (all carry the #h group tag).
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

const RELAYS = [CLUB_RELAY]

/** NIP-42 AUTH handler: signs the relay challenge via the active signer. */
const onauth = (evt: EventTemplate): Promise<VerifiedEvent> =>
  signEvent(evt) as Promise<VerifiedEvent>

function tagValue(ev: Event, name: string): string | undefined {
  return ev.tags.find((t) => t[0] === name)?.[1]
}

/** Generates a short, unique group id (NIP-29 d-tag). */
function generateGroupId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(8))
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

/** Publishes an already-signed event to the club relay (with AUTH). Fire-and-forget-ok. */
export async function publishSignedClub(signed: Event): Promise<void> {
  await Promise.allSettled(pool.publish(RELAYS, signed, { onauth }))
}

/** Signs a template and publishes it to the club relay. Throws on total failure. */
async function publishClub(template: EventTemplate): Promise<Event> {
  const signed = await signEvent(template)
  const results = await Promise.allSettled(pool.publish(RELAYS, signed, { onauth }))
  if (!results.some((r) => r.status === 'fulfilled')) {
    const reason = results.find((r) => r.status === 'rejected') as PromiseRejectedResult | undefined
    throw new Error(reason?.reason?.toString() ?? 'Relay rejected the event')
  }
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
export async function createClub(meta: {
  name: string
  about?: string
  picture?: string
}): Promise<string> {
  const id = generateGroupId()
  await publishClub({ kind: KIND_CREATE_GROUP, created_at: now(), tags: [['h', id]], content: '' })

  const metaTags: string[][] = [
    ['h', id],
    ['name', meta.name],
    // open + public so anyone can join/listen (MVP).
    // Single-element tags (relay29 convention) — ['open',''] is NOT recognized.
    ['open'],
    ['public'],
  ]
  if (meta.about) metaTags.push(['about', meta.about])
  if (meta.picture) metaTags.push(['picture', meta.picture])
  await publishClub({ kind: KIND_EDIT_METADATA, created_at: now(), tags: metaTags, content: '' })

  return id
}

/**
 * Edits club metadata (name/about/picture). Only the host/admin may do this — the relay
 * enforces the role. open/public are re-sent so the club stays open/public.
 */
export async function editClub(
  groupId: string,
  meta: { name: string; about?: string; picture?: string },
): Promise<void> {
  const metaTags: string[][] = [
    ['h', groupId],
    ['name', meta.name],
    ['open'],
    ['public'],
  ]
  if (meta.about) metaTags.push(['about', meta.about])
  if (meta.picture) metaTags.push(['picture', meta.picture])
  await publishClub({ kind: KIND_EDIT_METADATA, created_at: now(), tags: metaTags, content: '' })
}

/**
 * Sets the club access config (kind 30101, OWNER only). For paid clubs the entry lightning
 * address is resolved to its NIP-57 zapper pubkey and stored too, so the relay can verify
 * entry receipts. Open clubs store access=open (price 0).
 */
export async function setClubConfig(
  groupId: string,
  cfg: { access: 'open' | 'paid'; price: number; lud16: string },
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
  }
}

/**
 * Join request (NIP-29 kind 9021). Open clubs auto-add the member on the relay. For a PAID
 * club, pass the 9735 entry receipt as `proof` — the relay verifies it before admitting.
 */
export async function joinClub(groupId: string, proof?: Event): Promise<void> {
  const tags: string[][] = [['h', groupId]]
  if (proof) tags.push(['proof', JSON.stringify(proof)])
  await publishClub({ kind: KIND_JOIN_REQUEST, created_at: now(), tags, content: '' })
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
 * ephemeral social signal, like applause); the 9735 — when it exists — stays authoritative,
 * and `bolt11` dedup (in ingestZapBroadcast/ingestZapReceipt) prevents double-counting.
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
  })
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

/** Deletes an event (e.g. a chat message) in the club (NIP-29 kind 9005). */
export async function deleteEvent(groupId: string, eventId: string): Promise<void> {
  await publishClub({
    kind: KIND_DELETE_EVENT,
    created_at: now(),
    tags: [
      ['h', groupId],
      ['e', eventId],
    ],
    content: '',
  })
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

/**
 * List of all clubs (kind:39000), enriched with member counts (kind:39002) and owner
 * (kind:39001). Active clubs (more members) first; empty (0 members) clubs are hidden
 * so orphaned/test clubs don't clutter the home page.
 */
export async function listClubs(): Promise<Club[]> {
  const [metaEvents, memberEvents, adminEvents, configEvents] = await Promise.all([
    pool.querySync(RELAYS, { kinds: [KIND_METADATA] }, { maxWait: 4000 }),
    pool.querySync(RELAYS, { kinds: [KIND_MEMBERS] }, { maxWait: 4000 }),
    pool.querySync(RELAYS, { kinds: [KIND_ADMINS] }, { maxWait: 4000 }),
    pool.querySync(RELAYS, { kinds: [KIND_CLUB_CONFIG] }, { maxWait: 4000 }),
  ])
  const counts = new Map<string, number>()
  for (const ev of memberEvents) {
    const id = tagValue(ev, 'd')
    if (id) counts.set(id, ev.tags.filter((t) => t[0] === 'p' && t[1]).length)
  }
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
  return metaEvents
    .map(parseClubMetadata)
    .filter((c) => c.id)
    .map((c) => ({
      ...c,
      memberCount: counts.get(c.id) ?? 0,
      owner: owners.get(c.id) || undefined,
      access: configs.get(c.id)?.access ?? 'open',
      price: configs.get(c.id)?.price ?? 0,
    }))
    .filter((c) => (c.memberCount ?? 0) > 0)
    .sort((a, b) => (b.memberCount ?? 0) - (a.memberCount ?? 0))
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
  const memberEvents = await pool.querySync(
    RELAYS,
    { kinds: [KIND_MEMBERS], '#p': [pubkey] },
    { maxWait: 4000 },
  )
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
const STAGE_STALE_MS = 3_600_000

/**
 * A user's club activity for their profile: clubs they own (host) and the club they're
 * currently DJing in (a fresh, non-"off" stage event). Both resolved to full Club cards.
 */
export async function fetchUserClubActivity(
  pubkey: string,
): Promise<{ hosting: Club[]; djingIn: Club[] }> {
  const [clubs, stageEvents] = await Promise.all([
    listClubs(),
    pool.querySync(RELAYS, { kinds: [KIND_STAGE], authors: [pubkey] }, { maxWait: 4000 }),
  ])
  const byId = new Map(clubs.map((c) => [c.id, c]))
  const hosting = clubs.filter((c) => c.owner === pubkey)

  // Newest stage event per club; live if not stepped "off" and within the sticky window.
  const newestByGroup = new Map<string, Event>()
  for (const ev of stageEvents) {
    const h = tagValue(ev, 'h')
    if (!h) continue
    const ex = newestByGroup.get(h)
    if (!ex || ev.created_at > ex.created_at) newestByGroup.set(h, ev)
  }
  const nowMs = Date.now()
  const djingIn: Club[] = []
  for (const [h, ev] of newestByGroup) {
    if (ev.content === 'off' || nowMs - ev.created_at * 1000 >= STAGE_STALE_MS) continue
    const c = byId.get(h)
    if (c) djingIn.push(c)
  }
  return { hosting, djingIn }
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
  onAdmins?: (ev: Event) => void
  onChat?: (ev: Event) => void
  onDeleteEvent?: (ev: Event) => void
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
}

/**
 * Subscribes to a club. Two subscriptions, because relay29 does not allow mixing
 * metadata kinds with others: metadata by #d, content by #h.
 * Returns a cleanup function.
 */
export function subscribeClub(groupId: string, h: ClubSubHandlers): () => void {
  const metaFilter: Filter = { kinds: [KIND_METADATA, KIND_MEMBERS, KIND_ADMINS], '#d': [groupId] }
  const contentFilter: Filter = {
    kinds: [
      KIND_CHAT,
      KIND_DELETE_EVENT,
      KIND_NOW_PLAYING,
      KIND_STAGE,
      KIND_STAGE_KICK,
      KIND_QUEUE,
      KIND_SKIP,
      KIND_CLUB_CONFIG,
      KIND_PRESENCE,
      KIND_ZAP_BROADCAST,
      KIND_FLOOR_REACTION,
      KIND_PLAY,
    ],
    '#h': [groupId],
  }

  const metaSub = pool.subscribe(RELAYS, metaFilter, {
    onauth,
    onevent(ev) {
      if (ev.kind === KIND_METADATA) h.onMeta?.(ev)
      else if (ev.kind === KIND_MEMBERS) h.onMembers?.(ev)
      else if (ev.kind === KIND_ADMINS) h.onAdmins?.(ev)
    },
  })

  const contentSub = pool.subscribe(RELAYS, contentFilter, {
    onauth,
    onevent(ev) {
      if (ev.kind === KIND_CHAT) h.onChat?.(ev)
      else if (ev.kind === KIND_DELETE_EVENT) h.onDeleteEvent?.(ev)
      else if (ev.kind === KIND_NOW_PLAYING) h.onNowPlaying?.(ev)
      else if (ev.kind === KIND_STAGE) h.onStage?.(ev)
      else if (ev.kind === KIND_STAGE_KICK) h.onStageKick?.(ev)
      else if (ev.kind === KIND_QUEUE) h.onQueue?.(ev)
      else if (ev.kind === KIND_SKIP) h.onSkip?.(ev)
      else if (ev.kind === KIND_CLUB_CONFIG) h.onConfig?.(ev)
      else if (ev.kind === KIND_PRESENCE) h.onPresence?.(ev)
      else if (ev.kind === KIND_ZAP_BROADCAST) h.onZapBroadcast?.(ev)
      else if (ev.kind === KIND_FLOOR_REACTION) h.onEmote?.(ev)
      else if (ev.kind === KIND_PLAY) h.onPlay?.(ev)
    },
  })

  return () => {
    metaSub.close()
    contentSub.close()
  }
}

export { publishClub }

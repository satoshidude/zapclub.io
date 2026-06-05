import type { Event, EventTemplate, VerifiedEvent } from 'nostr-tools/pure'
import type { Filter } from 'nostr-tools/filter'
import { pool, CLUB_RELAY } from './pool'
import { signEvent } from './nostrLogin'
import type { Club, ClubMember } from './types'

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

/** Join request (NIP-29 kind 9021). Open clubs auto-add the member on the relay. */
export async function joinClub(groupId: string): Promise<void> {
  await publishClub({ kind: KIND_JOIN_REQUEST, created_at: now(), tags: [['h', groupId]], content: '' })
}

export async function leaveClub(groupId: string): Promise<void> {
  await publishClub({ kind: KIND_LEAVE_REQUEST, created_at: now(), tags: [['h', groupId]], content: '' })
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
 * List of all clubs (kind:39000), enriched with member counts (kind:39002) and owner
 * (kind:39001). Active clubs (more members) first; empty (0 members) clubs are hidden
 * so orphaned/test clubs don't clutter the home page.
 */
export async function listClubs(): Promise<Club[]> {
  const [metaEvents, memberEvents, adminEvents] = await Promise.all([
    pool.querySync(RELAYS, { kinds: [KIND_METADATA] }, { maxWait: 4000 }),
    pool.querySync(RELAYS, { kinds: [KIND_MEMBERS] }, { maxWait: 4000 }),
    pool.querySync(RELAYS, { kinds: [KIND_ADMINS] }, { maxWait: 4000 }),
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
    if (id) owners.set(id, parseAdmins(ev)[0] ?? '')
  }
  return metaEvents
    .map(parseClubMetadata)
    .filter((c) => c.id)
    .map((c) => ({
      ...c,
      memberCount: counts.get(c.id) ?? 0,
      owner: owners.get(c.id) || undefined,
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

/** Fetch single club metadata (by d-tag). */
export async function fetchClub(groupId: string): Promise<Club | null> {
  const ev = await pool.get(RELAYS, { kinds: [KIND_METADATA], '#d': [groupId] })
  return ev ? parseClubMetadata(ev) : null
}

// ── Subscriptions (relay29 rule: metadata separate from content) ──────────────

export interface ClubSubHandlers {
  onMeta?: (ev: Event) => void
  onMembers?: (ev: Event) => void
  onAdmins?: (ev: Event) => void
  onChat?: (ev: Event) => void
  onDeleteEvent?: (ev: Event) => void
}

/**
 * Subscribes to a club. Two subscriptions, because relay29 does not allow mixing
 * metadata kinds with others: metadata by #d, content by #h.
 * Returns a cleanup function.
 */
export function subscribeClub(groupId: string, h: ClubSubHandlers): () => void {
  const metaFilter: Filter = { kinds: [KIND_METADATA, KIND_MEMBERS, KIND_ADMINS], '#d': [groupId] }
  const contentFilter: Filter = { kinds: [KIND_CHAT, KIND_DELETE_EVENT], '#h': [groupId] }

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
    },
  })

  return () => {
    metaSub.close()
    contentSub.close()
  }
}

export { publishClub }

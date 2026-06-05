import { pool, CLUB_RELAY } from './pool'
import { signEvent } from './nostrLogin'
import { auth } from './auth.svelte'
import { parseClubMetadata, parseMembers, parseAdmins, parseOwner } from './groups'
import type { Club, ClubMember } from './types'

// The superadmin (satoshidude). Only this pubkey may open the dashboard and the relay
// only honors /admin calls signed by it (NIP-98). Client gate is convenience; the relay
// is the real boundary.
export const SUPERADMIN = '661419f8f48b1b496e2249aee97a6ad9d5bea907149dc7bf3eb7479f2bce555e'

export function isSuperadmin(): boolean {
  return auth.pubkey === SUPERADMIN
}

const ADMIN_BASE = 'https://relay.zapclub.io'

// Builds a NIP-98 (kind 27235) Authorization header for an admin call.
async function nip98Header(url: string, method: string): Promise<string> {
  const ev = await signEvent({
    kind: 27235,
    created_at: Math.floor(Date.now() / 1000),
    tags: [
      ['u', url],
      ['method', method],
    ],
    content: '',
  })
  return 'Nostr ' + btoa(JSON.stringify(ev))
}

async function adminFetch(path: string, method: 'GET' | 'POST', body?: unknown): Promise<unknown> {
  const url = ADMIN_BASE + path
  const headers: Record<string, string> = { Authorization: await nip98Header(url, method) }
  if (body) headers['Content-Type'] = 'application/json'
  const res = await fetch(url, { method, headers, body: body ? JSON.stringify(body) : undefined })
  if (!res.ok) throw new Error(`${res.status}: ${(await res.text()) || res.statusText}`)
  return res.json()
}

export function banPubkey(pubkey: string, reason: string): Promise<{ ok: boolean; purged: number }> {
  return adminFetch('/admin/ban', 'POST', { pubkey, reason }) as Promise<{ ok: boolean; purged: number }>
}
export function unbanPubkey(pubkey: string): Promise<{ ok: boolean }> {
  return adminFetch('/admin/unban', 'POST', { pubkey }) as Promise<{ ok: boolean }>
}
export function listBans(): Promise<Record<string, string>> {
  return adminFetch('/admin/bans', 'GET') as Promise<Record<string, string>>
}
export function deleteClubAdmin(groupId: string): Promise<{ ok: boolean; purged: number }> {
  return adminFetch('/admin/delete-club', 'POST', { groupId }) as Promise<{ ok: boolean; purged: number }>
}

/** A fully-detailed club view for the superadmin dashboard. */
export interface AdminClub extends Club {
  admins: string[]
  members: ClubMember[]
  nowPlaying: { title: string; videoId: string; dj: string } | null
  djs: string[] // active stage DJs (kind 30102, not stepped off)
}

const STAGE_STALE_MS = 3_600_000

/** Loads every club with full details straight from the relay (the relay IS the index). */
export async function loadAdminData(): Promise<AdminClub[]> {
  const [meta, members, admins, np, stage] = await Promise.all([
    pool.querySync([CLUB_RELAY], { kinds: [39000] }, { maxWait: 5000 }),
    pool.querySync([CLUB_RELAY], { kinds: [39002] }, { maxWait: 5000 }),
    pool.querySync([CLUB_RELAY], { kinds: [39001] }, { maxWait: 5000 }),
    pool.querySync([CLUB_RELAY], { kinds: [30100] }, { maxWait: 5000 }),
    pool.querySync([CLUB_RELAY], { kinds: [30102] }, { maxWait: 5000 }),
  ])
  const dTag = (ev: { tags: string[][] }) => ev.tags.find((t) => t[0] === 'd')?.[1] ?? ''
  const hTag = (ev: { tags: string[][] }) => ev.tags.find((t) => t[0] === 'h')?.[1] ?? ''

  const membersById = new Map<string, ClubMember[]>()
  for (const ev of members) membersById.set(dTag(ev), parseMembers(ev))
  const adminsById = new Map<string, string[]>()
  const ownerById = new Map<string, string>()
  for (const ev of admins) {
    adminsById.set(dTag(ev), parseAdmins(ev))
    ownerById.set(dTag(ev), parseOwner(ev))
  }

  const npById = new Map<string, { title: string; videoId: string; dj: string }>()
  for (const ev of np) {
    const id = dTag(ev)
    const track = ev.tags.find((t) => t[0] === 'track')?.[1] ?? ''
    npById.set(id, {
      title: ev.content || track,
      videoId: track.startsWith('yt:') ? track.slice(3) : '',
      dj: ev.tags.find((t) => t[0] === 'p')?.[1] ?? '',
    })
  }

  // Active DJs per club: newest 30102 per (club, author), not stepped "off", fresh.
  const nowMs = Date.now()
  const djNewest = new Map<string, Map<string, { at: number; on: boolean }>>()
  for (const ev of stage) {
    const id = hTag(ev)
    if (!id) continue
    let m = djNewest.get(id)
    if (!m) djNewest.set(id, (m = new Map()))
    const prev = m.get(ev.pubkey)
    if (!prev || ev.created_at > prev.at) m.set(ev.pubkey, { at: ev.created_at, on: ev.content !== 'off' })
  }
  const djsById = new Map<string, string[]>()
  for (const [id, m] of djNewest) {
    const live: string[] = []
    for (const [pk, d] of m) if (d.on && nowMs - d.at * 1000 < STAGE_STALE_MS) live.push(pk)
    djsById.set(id, live)
  }

  return meta
    .map(parseClubMetadata)
    .filter((c) => c.id)
    .map((c): AdminClub => {
      const ms = membersById.get(c.id) ?? []
      const ad = adminsById.get(c.id) ?? []
      return {
        ...c,
        memberCount: ms.length,
        owner: ownerById.get(c.id) || undefined,
        admins: ad,
        members: ms,
        nowPlaying: npById.get(c.id) ?? null,
        djs: djsById.get(c.id) ?? [],
      }
    })
    .sort((a, b) => (b.memberCount ?? 0) - (a.memberCount ?? 0))
}

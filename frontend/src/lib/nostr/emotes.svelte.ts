import type { Event } from 'nostr-tools/pure'
import { publishClub, KIND_FLOOR_REACTION } from './groups'

// Ephemeral floor reactions (kind 20103): a member taps an emoji, it flies up over the dancefloor
// and fades. Not stored (the relay broadcasts the ephemeral event to everyone in the club).
export interface FloorEmote {
  id: string
  pubkey: string
  emoji: string
  at: number // ms received
}

const TTL_MS = 4000
const MAX = 40

const state = $state<{ items: FloorEmote[] }>({ items: [] })

export const emotes = {
  get items() {
    return state.items
  },
}

/** Handle an incoming floor reaction (kind 20103). Caps content length + count. */
export function ingestEmote(ev: Event): void {
  const emoji = (ev.content || '').trim().slice(0, 12)
  if (!emoji || state.items.some((e) => e.id === ev.id)) return
  state.items = [...state.items, { id: ev.id, pubkey: ev.pubkey, emoji, at: Date.now() }].slice(-MAX)
}

/** Send a floor reaction (members only — the relay rejects non-member writes). */
export async function sendEmote(groupId: string, emoji: string): Promise<void> {
  const e = emoji.trim().slice(0, 12)
  if (!e) return
  await publishClub({
    kind: KIND_FLOOR_REACTION,
    created_at: Math.floor(Date.now() / 1000),
    tags: [['h', groupId]],
    content: e,
  })
}

export function resetEmotes(): void {
  state.items = []
}

// Sweep expired emotes out of the DOM.
if (typeof setInterval !== 'undefined') {
  setInterval(() => {
    const cut = Date.now() - TTL_MS
    if (state.items.some((e) => e.at < cut)) state.items = state.items.filter((e) => e.at >= cut)
  }, 1000)
}

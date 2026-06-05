import type { Event } from 'nostr-tools/pure'
import { KIND_CHAT, publishClub } from './groups'
import type { ChatMessage } from './types'

const MAX_MESSAGES = 200

const state = $state<{ messages: ChatMessage[] }>({ messages: [] })

export const chat = {
  get messages() {
    return state.messages
  },
}

/** Handles an incoming chat event (kind:9), deduplicates, keeps sorting. */
export function ingestChat(ev: Event): void {
  if (state.messages.some((m) => m.id === ev.id)) return
  const msg: ChatMessage = {
    id: ev.id,
    pubkey: ev.pubkey,
    content: ev.content,
    createdAt: ev.created_at,
  }
  // Insert sorted by created_at (subscriptions don't guarantee order).
  const idx = state.messages.findIndex((m) => m.createdAt > msg.createdAt)
  if (idx === -1) state.messages.push(msg)
  else state.messages.splice(idx, 0, msg)
  if (state.messages.length > MAX_MESSAGES) {
    state.messages.splice(0, state.messages.length - MAX_MESSAGES)
  }
}

export async function sendChat(groupId: string, content: string): Promise<void> {
  const text = content.trim()
  if (!text) return
  await publishClub({
    kind: KIND_CHAT,
    created_at: Math.floor(Date.now() / 1000),
    tags: [['h', groupId]],
    content: text,
  })
}

/** Removes a message (moderation/deletion) locally. */
export function removeMessage(eventId: string): void {
  state.messages = state.messages.filter((m) => m.id !== eventId)
}

export function resetChat(): void {
  state.messages = []
}

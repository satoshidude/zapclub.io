import type { Event, EventTemplate, VerifiedEvent } from 'nostr-tools/pure'
import type { Filter } from 'nostr-tools/filter'
import { minePow } from 'nostr-tools/nip13'
import { auth } from './auth.svelte'
import { publishClub } from './groups'
import { signEvent } from './nostrLogin'
import { CLUB_RELAY, pool } from './pool'

export const KIND_CHAT = 9
export const CHAT_MAX_LENGTH = 1000
export const CHAT_HISTORY_LIMIT = 100
const CHAT_POW_DIFFICULTY = 10

const onauth = (event: EventTemplate): Promise<VerifiedEvent> =>
  signEvent(event) as Promise<VerifiedEvent>

function now(): number {
  return Math.floor(Date.now() / 1000)
}
export function normalizeChatContent(content: string): string {
  return content.trim().slice(0, CHAT_MAX_LENGTH)
}

/** Publish a NIP-29 kind-9 message with the relay's required NIP-13 proof of work. */
export async function publishChat(groupId: string, content: string): Promise<Event> {
  if (!auth.canSign || !auth.pubkey) throw new Error('Sign in to chat')
  const normalized = normalizeChatContent(content)
  if (!normalized) throw new Error('Write a message first')

  // Mine before signing. signEvent restores pubkey and computes the final id/signature
  // from the nonce-bearing template.
  const template = minePow(
    {
      kind: KIND_CHAT,
      created_at: now(),
      tags: [['h', groupId]],
      content: normalized,
      pubkey: auth.pubkey,
    },
    CHAT_POW_DIFFICULTY,
  )
  const { pubkey: _pubkey, id: _id, ...unsigned } = template
  return publishClub(unsigned as EventTemplate)
}

/** Stable chronological merge for history, reconnects and optimistic own events. */
export function mergeChatMessages(current: Event[], incoming: Event, limit = CHAT_HISTORY_LIMIT): Event[] {
  if (incoming.kind !== KIND_CHAT || current.some((event) => event.id === incoming.id)) return current
  return [...current, incoming]
    .sort((a, b) => a.created_at - b.created_at || a.id.localeCompare(b.id))
    .slice(-limit)
}

export interface ChatSubscriptionHandlers {
  onMessage: (event: Event) => void
  onEose?: () => void
  onClose?: (reason: string) => void
}

/** Member-authenticated history + live subscription for one club. */
export function subscribeChat(groupId: string, handlers: ChatSubscriptionHandlers): () => void {
  const filter: Filter = {
    kinds: [KIND_CHAT],
    '#h': [groupId],
    limit: CHAT_HISTORY_LIMIT,
  }
  const sub = pool.subscribe([CLUB_RELAY], filter, {
    onauth,
    onevent: handlers.onMessage,
    oneose: handlers.onEose,
    onclose: (reasons) => {
      const reason = reasons.find((entry) => entry && !entry.includes('closed by caller'))
      if (reason) handlers.onClose?.(reason)
    },
  })
  return () => sub.close('closed by caller')
}

import { finalizeEvent, generateSecretKey, type Event, type EventTemplate } from 'nostr-tools/pure'
import { auth } from './auth.svelte'

/** Relay-recognized marker for connection-bound, locally signed session events. */
export const SESSION_EVENT_MARKER = 'zapclub-session-v1'
export const SESSION_EVENT_MAX_FUTURE_SECONDS = 30

let keyOwner: string | null = null
let key: Uint8Array | null = null
const lastTimestamp = new Map<string, number>()

function sessionKey(pubkey: string): Uint8Array {
  if (!key || keyOwner !== pubkey) {
    key = generateSecretKey()
    keyOwner = pubkey
    lastTimestamp.clear()
  }
  return key
}

/**
 * Signs a narrowly scoped runtime event with a throwaway page-session key. The relay accepts
 * these events only on a NIP-42 connection authenticated as the p-tagged main identity and
 * re-checks membership, bans, rate limits and stage capacity. The key never leaves memory.
 */
export function signSessionEvent(template: EventTemplate): Event {
  const principal = auth.pubkey
  if (!principal) throw new Error('No authenticated identity for session event')
  const signingKey = sessionKey(principal)

  const group = template.tags.find((tag) => tag[0] === 'h')?.[1] ?? ''
  const address = `${principal}:${template.kind}:${group}`
  const createdAt = Math.max(template.created_at, (lastTimestamp.get(address) ?? 0) + 1)
  const maxCreatedAt = Math.floor(Date.now() / 1000) + SESSION_EVENT_MAX_FUTURE_SECONDS
  if (createdAt > maxCreatedAt) {
    throw new Error('Too many session events in one second; wait before sending another')
  }
  lastTimestamp.set(address, createdAt)

  return finalizeEvent(
    {
      ...template,
      created_at: createdAt,
      tags: [
        ...template.tags.filter((tag) => tag[0] !== 'p' && tag[0] !== 'client'),
        ['p', principal],
        ['client', SESSION_EVENT_MARKER],
      ],
    },
    signingKey,
  )
}

/**
 * Returns the relay-bound main identity represented by a received session event.
 * Only the two relay-supported session kinds may delegate identity, and only through
 * one unambiguous, exactly-shaped marker + principal tag pair. Anything else remains
 * attributable to the event's actual signing key.
 */
export function sessionEventPrincipal(event: Pick<Event, 'kind' | 'pubkey' | 'tags'>): string {
  if (event.kind !== 20100 && event.kind !== 30102) return event.pubkey
  const clientTags = event.tags.filter((tag) => tag[0] === 'client')
  const principalTags = event.tags.filter((tag) => tag[0] === 'p')
  if (clientTags.length !== 1 || principalTags.length !== 1) return event.pubkey

  const client = clientTags[0]
  const principal = principalTags[0]
  if (client.length !== 2 || client[1] !== SESSION_EVENT_MARKER) return event.pubkey
  if (principal.length !== 2 || !/^[0-9a-f]{64}$/i.test(principal[1])) return event.pubkey
  return principal[1]
}

export function resetSessionSigner(): void {
  key = null
  keyOwner = null
  lastTimestamp.clear()
}

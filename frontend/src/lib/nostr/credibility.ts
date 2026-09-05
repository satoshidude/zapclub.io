import type { Event } from 'nostr-tools/pure'
import { CLUB_RELAY, CLUB_RELAY_PUBKEY, pool } from './pool'

export const KIND_CREDIBILITY = 30078
export const CREDIBILITY_NAMESPACE = 'zapclub-credibility'

export interface Credibility {
  score: number
  tracks: number
  bangers: number
  skipped: number
}

function intTag(event: Event, name: string): number | null {
  const raw = event.tags.find((tag) => tag[0] === name)?.[1]
  if (raw == null || !/^-?\d+$/.test(raw)) return null
  const value = Number(raw)
  return Number.isSafeInteger(value) ? value : null
}

export function parseCredibility(event: Event, pubkey: string): Credibility | null {
  if (event.kind !== KIND_CREDIBILITY || event.pubkey !== CLUB_RELAY_PUBKEY) return null
  const tag = (name: string) => event.tags.find((item) => item[0] === name)?.[1]
  if (tag('h') !== CREDIBILITY_NAMESPACE || tag('p') !== pubkey || tag('d') !== `zapclub:credibility:${pubkey}`) return null
  const score = intTag(event, 'score')
  const tracks = intTag(event, 'tracks')
  const bangers = intTag(event, 'bangers')
  const skipped = intTag(event, 'skipped')
  if (score == null || tracks == null || bangers == null || skipped == null) return null
  if (tracks < 0 || bangers < 0 || skipped < 0) return null
  return { score, tracks, bangers, skipped }
}

/** Reads the relay-attested, replaceable NIP-78 score snapshot for one DJ. */
export async function fetchCredibility(pubkey: string): Promise<Credibility | null> {
  try {
    const event = await pool.get(
      [CLUB_RELAY],
      {
        kinds: [KIND_CREDIBILITY],
        authors: [CLUB_RELAY_PUBKEY],
        '#h': [CREDIBILITY_NAMESPACE],
        '#d': [`zapclub:credibility:${pubkey}`],
        '#p': [pubkey],
      },
      { maxWait: 4000 },
    )
    return event ? parseCredibility(event, pubkey) : null
  } catch {
    return null
  }
}

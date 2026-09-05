// Global public ranking from the relay's aggregate endpoint. Building the candidate
// set from kind 39002 would expose the protected club membership roster.

import type { Event } from 'nostr-tools/pure'
import { nip98Header } from './nip98'

export interface LeaderboardEntry {
  pubkey: string
  rank: number
  score: number // relay-calculated DJ SCORE in tenths (0…1000)
  tracks: number
  bangers: number
  skipped: number
  vibeScore: number
}

export interface DJRank extends LeaderboardEntry {
  total: number // ranked DJs
}

export interface TrackLeaderboardEntry {
  rank: number
  club: string
  videoId: string
  title: string
  dj: string
  bangers: number
  skipped: boolean
  startedAt: number
}

export interface LeaderboardPayload {
  total: number
  top: LeaderboardEntry[]
  topTracks: TrackLeaderboardEntry[]
}

type UnknownRecord = Record<string, unknown>

const PUBKEY_PATTERN = /^[0-9a-f]{64}$/

function isRecord(value: unknown): value is UnknownRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isSafeInteger(value: unknown, min: number, max = Number.MAX_SAFE_INTEGER): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= min && value <= max
}

function parseEntry(value: unknown): LeaderboardEntry | null {
  if (!isRecord(value)) return null
  const { pubkey, rank, score, tracks, bangers, skipped, vibeScore } = value
  if (typeof pubkey !== 'string' || !PUBKEY_PATTERN.test(pubkey)) return null
  if (!isSafeInteger(rank, 1) || !isSafeInteger(score, 0, 1000) || !isSafeInteger(tracks, 1)) return null
  if (!isSafeInteger(bangers, 0, tracks * 5) || !isSafeInteger(skipped, 0, tracks)) return null
  if (!isSafeInteger(vibeScore, -tracks, tracks * 5)) return null
  return { pubkey, rank, score, tracks, bangers, skipped, vibeScore }
}

function parseTrackEntry(value: unknown): TrackLeaderboardEntry | null {
  if (!isRecord(value)) return null
  const { rank, club, videoId, title, dj, bangers, skipped, startedAt } = value
  if (!isSafeInteger(rank, 1, 10) || typeof club !== 'string' || club.length === 0 || club.length > 256) return null
  if (typeof videoId !== 'string' || videoId.length > 64 || typeof title !== 'string' || title.length > 500) return null
  if (typeof dj !== 'string' || !PUBKEY_PATTERN.test(dj)) return null
  if (!isSafeInteger(bangers, 1, 5) || typeof skipped !== 'boolean' || !isSafeInteger(startedAt, 1)) return null
  return { rank, club, videoId, title, dj, bangers, skipped, startedAt }
}

function parseLeaderboard(value: unknown): LeaderboardPayload {
  if (!isRecord(value) || !Array.isArray(value.top) || !isSafeInteger(value.total, 0)) {
    return { total: 0, top: [], topTracks: [] }
  }

  const seenPubkeys = new Set<string>()
  const seenRanks = new Set<number>()
  const top: LeaderboardEntry[] = []
  for (const candidate of value.top) {
    const entry = parseEntry(candidate)
    if (!entry || seenPubkeys.has(entry.pubkey) || seenRanks.has(entry.rank)) continue
    seenPubkeys.add(entry.pubkey)
    seenRanks.add(entry.rank)
    top.push(entry)
  }
  const seenTrackRanks = new Set<number>()
  const seenPerformances = new Set<string>()
  const topTracks: TrackLeaderboardEntry[] = []
  if (Array.isArray(value.topTracks)) {
    for (const candidate of value.topTracks) {
      const entry = parseTrackEntry(candidate)
      const performanceKey = entry ? `${entry.club}:${entry.startedAt}:${entry.dj}` : ''
      if (!entry || seenTrackRanks.has(entry.rank) || seenPerformances.has(performanceKey)) continue
      seenTrackRanks.add(entry.rank)
      seenPerformances.add(performanceKey)
      topTracks.push(entry)
    }
  }
  if (top.length === 0) return { total: 0, top: [], topTracks }
  return { total: Math.max(value.total, top.length), top, topTracks }
}

let cache: { at: number; promise: Promise<LeaderboardPayload> } | null = null
const TTL_MS = 60_000
const LEADERBOARD_URL = 'https://relay.zapclub.io/leaderboard'
const ZAPS_URL = 'https://relay.zapclub.io/zaps'

export interface ReceivedZaps {
  total: number
  count: number
  bySender: { sender: string; sats: number; count: number; exact: boolean; anon: boolean }[]
}

/** Relay-authoritative DJ performance ranking. Cached for roughly one minute. */
export function fetchLeaderboard(): Promise<LeaderboardPayload> {
  if (cache && Date.now() - cache.at < TTL_MS) return cache.promise
  const promise = build()
  cache = { at: Date.now(), promise }
  return promise
}

async function build(): Promise<LeaderboardPayload> {
  try {
    const response = await fetch(LEADERBOARD_URL)
    if (!response.ok) throw new Error(`leaderboard: ${response.status}`)
    return parseLeaderboard(await response.json())
  } catch {
    return { total: 0, top: [], topTracks: [] }
  }
}

/** A DJ's global performance placement, or null if they have no settled songs. */
export async function fetchDJRank(pubkey: string): Promise<DJRank | null> {
  if (!PUBKEY_PATTERN.test(pubkey)) return null
  try {
    const response = await fetch(`${LEADERBOARD_URL}?pubkey=${encodeURIComponent(pubkey)}`)
    if (!response.ok) return null
    const result: unknown = await response.json()
    if (!isRecord(result) || result.ranked !== true || !isSafeInteger(result.total, 1)) return null
    const entry = parseEntry(result)
    if (!entry || entry.pubkey !== pubkey || entry.rank > result.total) return null
    return { ...entry, total: result.total }
  } catch {
    return null
  }
}

/** Records a payment confirmed inside Zapclub using its already-signed NIP-57 request. */
export async function recordZap(request: Event, invoice: string): Promise<void> {
  const response = await fetch(ZAPS_URL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ request, invoice }),
  })
  if (!response.ok) throw new Error(`record zap: ${response.status}`)
}

/** Returns only Zapclub-recorded zaps received by the NIP-98 signer. */
export async function fetchReceivedZaps(): Promise<ReceivedZaps> {
  const url = `${ZAPS_URL}/received`
  const response = await fetch(url, {
    headers: { Authorization: await nip98Header(url, 'GET') },
  })
  if (!response.ok) throw new Error(`received zaps: ${response.status}`)
  return response.json() as Promise<ReceivedZaps>
}

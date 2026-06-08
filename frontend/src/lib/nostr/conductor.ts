import type { StageDj } from './types'

// Pure stage-selection logic — no Nostr, no $state, no clock. Decides WHO is on stage (and in
// what round-robin order), unit-testable in isolation. (Conductor election lived here too but is
// gone: the RELAY is the sole conductor now, so clients never elect one.)

export const MAX_DJS = 5
// Sticky stage: a DJ stays up to 1h after their last heartbeat (survives backgrounded tabs).
export const STALE_MS = 3_600_000

export interface DjState {
  since: number // stage-join time (s) → round-robin order
  lastSeen: number // last heartbeat (ms)
  on: boolean
}

/**
 * Active stage DJs: on + fresh (< STALE_MS) + not kicked (last heartbeat newer than the
 * last kick), sorted by stage join (oldest first; pubkey tiebreak for determinism across
 * clients), capped at maxDjs.
 */
export function selectActiveDjs(
  djs: Record<string, DjState>,
  kicks: Record<string, number>,
  nowMs: number,
  maxDjs = MAX_DJS,
): StageDj[] {
  return Object.entries(djs)
    .filter(([pk, d]) => d.on && nowMs - d.lastSeen < STALE_MS && d.lastSeen > (kicks[pk] ?? 0))
    .map(([pubkey, d]) => ({ pubkey, since: d.since }))
    .sort((a, b) => a.since - b.since || a.pubkey.localeCompare(b.pubkey))
    .slice(0, maxDjs)
}

import type { StageDj } from './types'

// Pure conductor/stage-selection logic — no Nostr, no $state, no clock. Extracted from
// stage.svelte.ts so the rules that decide WHO drives playback (the most bug-prone part)
// are unit-testable in isolation.

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

/**
 * Conductor (time authority) selection — pure and DETERMINISTIC across clients:
 *  1. Owner on stage → always the conductor (resolves multi-candidate conflicts).
 *  2. Otherwise the current conductor stays as long as they're active (sticky — a newly
 *     joined DJ never steals it, even on clock skew).
 *  3. Otherwise the oldest active DJ.
 * Returns the conductor pubkey, or null if the stage is empty.
 */
export function pickConductor(
  active: StageDj[],
  hostPk: string | null,
  current: string | null,
): string | null {
  if (active.length === 0) return null
  if (hostPk && active.some((d) => d.pubkey === hostPk)) return hostPk
  if (current && active.some((d) => d.pubkey === current)) return current
  return active[0].pubkey
}

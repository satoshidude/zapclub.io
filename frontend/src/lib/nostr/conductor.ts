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

/**
 * Whether `me` should DRIVE playback (write now_playing) right now — pure & deterministic.
 *
 * The elected conductor (pickConductor) is the time authority, BUT conducting only happens
 * while the club view is open, whereas stage presence is sticky across navigation. So an
 * elected conductor can navigate away (mini-player keeps their audio, the 5-min stage
 * heartbeat keeps them "on stage") and silently stop driving now_playing — freezing the room
 * for everyone, since other clients defer to them. This bridges that gap:
 *
 *  1. I'm the elected conductor → I always act (present-and-elected reclaims; owner-override).
 *  2. now_playing is fresh (writer still heartbeating) → only that writer keeps going. A
 *     rescuer that took over stays the writer until the elected conductor returns and writes.
 *  3. now_playing went silent (writer gone > rescueAfterMs) → the oldest-since active DJ
 *     OTHER than the silent writer takes over. Deterministic, so exactly one DJ rescues.
 *
 * @param me            local pubkey (or null)
 * @param elected       pickConductor result (owner-override / sticky-oldest)
 * @param activeDjs     active stage DJ pubkeys, oldest-since first (selectActiveDjs order)
 * @param npWriter      writer of the current now_playing, or null if no track yet
 * @param npStaleMs     ms since now_playing was last refreshed (Infinity if none)
 * @param rescueAfterMs staleness beyond which a rescuer may take over the silent conductor
 */
export function shouldConduct(
  me: string | null,
  elected: string | null,
  activeDjs: string[],
  npWriter: string | null,
  npStaleMs: number,
  rescueAfterMs: number,
): boolean {
  if (!me) return false
  if (me === elected) return true
  if (npWriter == null) return false // no track yet & I'm not elected → defer to the elected conductor
  if (npStaleMs <= rescueAfterMs) return npWriter === me // fresh → only the current writer drives
  // Silent conductor → oldest active DJ that is neither the silent writer nor the (absent)
  // elected conductor rescues the room.
  const rescuer = activeDjs.find((pk) => pk !== npWriter && pk !== elected)
  return rescuer === me
}

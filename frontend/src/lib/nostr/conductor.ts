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
 *  3. now_playing went silent (writer gone > rescueAfterMs) → the oldest-since active DJ that
 *     is ONLINE (live presence) and isn't the silent writer takes over. Using presence is
 *     what makes the rescue CASCADE: if the elected conductor AND the next-oldest DJ are both
 *     phantoms (on stage but away), it still hands off to whoever is actually here — instead
 *     of deadlocking on an absent designated rescuer. Falls back to the oldest non-writer when
 *     no one is reporting presence (so a single present-but-silent DJ still tries).
 *
 * @param me            local pubkey (or null)
 * @param elected       pickConductor result (owner-override / sticky-oldest)
 * @param activeDjs     active stage DJ pubkeys, oldest-since first (selectActiveDjs order)
 * @param npWriter      writer of the current now_playing, or null if no track yet
 * @param npStaleMs     ms since now_playing was last refreshed (Infinity if none)
 * @param rescueAfterMs staleness beyond which a rescuer may take over the silent conductor
 * @param isOnline      reports whether a pubkey is reporting live presence (default: all)
 */
export function shouldConduct(
  me: string | null,
  elected: string | null,
  activeDjs: string[],
  npWriter: string | null,
  npStaleMs: number,
  rescueAfterMs: number,
  isOnline: (pubkey: string) => boolean = () => true,
): boolean {
  if (!me) return false
  if (me === elected) return true
  if (npWriter == null) {
    // No now_playing yet → normally the elected conductor bootstraps it. But if the elected
    // conductor is OFFLINE (the oldest on-stage DJ has a dead/closed client, or an empty queue
    // and navigated away — stage is sticky for 1h), nobody would EVER start and the room stays
    // silent even though other present DJs have full queues. So when the elected conductor
    // isn't online, the oldest ONLINE active DJ bootstraps playback (same cascade as the
    // stale-conductor rescue). When presence is unknown (default all-online) we still defer to
    // the elected conductor, preserving the deterministic single-starter behaviour.
    if (!elected || isOnline(elected)) return false
    const online = activeDjs.filter(isOnline)
    const starter = online.find((pk) => pk !== elected) ?? online[0]
    return starter === me
  }
  if (npStaleMs <= rescueAfterMs) return npWriter === me // fresh → only the current writer drives
  // Silent conductor → pick a rescuer among ONLINE active DJs (so we don't hand off to another
  // phantom), oldest-since first, excluding the silent writer. No one online → fall back to any
  // non-writer (a single present DJ that hasn't been seen via presence still tries).
  const notWriter = activeDjs.filter((pk) => pk !== npWriter)
  const online = notWriter.filter(isOnline)
  const pool = online.length ? online : notWriter
  const rescuer = pool.find((pk) => pk !== elected) ?? pool[0]
  return rescuer === me
}

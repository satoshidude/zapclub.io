import type { Event } from 'nostr-tools/pure'
import { KIND_STAGE, publishClub } from './groups'
import { auth } from './auth.svelte'
import type { StageDj } from './types'

// zapclub: open stage — any member may take a free slot, up to 5 DJs. The stage is a
// content event (30102), NOT a relay role; there is no dj-policy gate.
export const MAX_DJS = 5

// Remembers (pubkey-bound) "I'm on stage in club X" → after a reload the client re-joins
// itself (the heartbeat would otherwise stop). Cleared on explicit leave/logout, NOT on
// reset (otherwise no reload-resume).
const ONSTAGE_KEY = 'zapclub:onstage'
function rememberStage(groupId: string, since: number): void {
  try {
    localStorage.setItem(
      ONSTAGE_KEY,
      JSON.stringify({ group: groupId, pubkey: auth.pubkey, since }),
    )
  } catch {
    /* ignore */
  }
}
/** Persisted `since` for this stage (or null) — keeps the DJ order stable across reloads;
 *  otherwise the DJ would get a new `since` on each resume and the round-robin order would
 *  drift between clients. */
function persistedSince(groupId: string): number | null {
  try {
    const raw = localStorage.getItem(ONSTAGE_KEY)
    if (!raw) return null
    const o = JSON.parse(raw)
    return o && o.pubkey === auth.pubkey && o.group === groupId && typeof o.since === 'number'
      ? o.since
      : null
  } catch {
    return null
  }
}
function forgetStage(): void {
  try {
    localStorage.removeItem(ONSTAGE_KEY)
  } catch {
    /* ignore */
  }
}
/** Club id if the current user was on its stage per the marker (else null). */
export function persistedStageGroup(): string | null {
  try {
    const raw = localStorage.getItem(ONSTAGE_KEY)
    if (!raw) return null
    const o = JSON.parse(raw)
    return o && o.pubkey === auth.pubkey ? (o.group ?? null) : null
  } catch {
    return null
  }
}

// Heartbeat every 5 min — the 1h window (STALE_MS) has huge headroom, so an infrequent
// keep-alive suffices (join/leave are posted immediately, plus an instant refresh when the
// tab returns). Keeps relay load/DB small.
const HEARTBEAT_MS = 300_000
// "Sticky stage": a DJ stays up to 1 hour after their last heartbeat — even if the app is
// backgrounded/suspended and no timers run. You only come down via: leave stage, logout,
// owner kick, or 1h without a sign of life.
const STALE_MS = 3_600_000

interface StageState {
  /** pubkey → last state. */
  djs: Record<string, { since: number; lastSeen: number; on: boolean }>
  /** pubkey → last kick time (ms). Whoever posts "on" after a kick is back up. */
  kicks: Record<string, number>
  /** Reactive time tick so freshness filters re-evaluate. */
  tick: number
  /** Current conductor (sticky — stays as long as active). */
  conductorPk: string | null
}

const state = $state<StageState>({ djs: {}, kicks: {}, tick: 0, conductorPk: null })

// Club owner (first admin). If on stage, they are ALWAYS the conductor (time authority) —
// deterministic across all clients (everyone knows the same owner from 39001), which also
// resolves the now_playing conflict between multiple "conductor" candidates.
let hostPk: string | null = null
export function setStageHost(pk: string | null): void {
  if (pk === hostPk) return
  hostPk = pk
  resolveConductor()
}

/**
 * Determines the conductor STICKILY: the current conductor stays as long as they are
 * active — a newly joined DJ does NOT take over (not even on clock skew that could sort
 * their `since` falsely to the front). Only when the conductor leaves does the next
 * (oldest `since`) take over.
 */
function resolveConductor(): void {
  const active = activeDjs()
  if (active.length === 0) {
    state.conductorPk = null
    return
  }
  // Owner on stage → they lead, regardless of join order.
  if (hostPk && active.some((d) => d.pubkey === hostPk)) {
    state.conductorPk = hostPk
    return
  }
  if (state.conductorPk && active.some((d) => d.pubkey === state.conductorPk)) return // sticky
  state.conductorPk = active[0].pubkey
}

let tickTimer: ReturnType<typeof setInterval> | null = null
function ensureTicking(): void {
  if (tickTimer) return
  tickTimer = setInterval(() => {
    state.tick = Date.now()
    resolveConductor()
  }, 5000)
}

/**
 * Active stage DJs: on + fresh (<1h) + not kicked (last on-heartbeat newer than the last
 * kick), sorted by stage join, max MAX_DJS.
 */
function activeDjs(): StageDj[] {
  const now = state.tick || Date.now()
  return Object.entries(state.djs)
    .filter(
      ([pk, d]) =>
        d.on &&
        now - d.lastSeen < STALE_MS &&
        d.lastSeen > (state.kicks[pk] ?? 0),
    )
    .map(([pubkey, d]) => ({ pubkey, since: d.since }))
    .sort((a, b) => a.since - b.since || a.pubkey.localeCompare(b.pubkey))
    .slice(0, MAX_DJS)
}

export const stage = {
  get djs(): StageDj[] {
    return activeDjs()
  },
  get full(): boolean {
    return activeDjs().length >= MAX_DJS
  },
  /** Conductor (time authority) — sticky: first DJ leads, next on leave. */
  get conductor(): string | null {
    if (state.conductorPk && activeDjs().some((d) => d.pubkey === state.conductorPk)) {
      return state.conductorPk
    }
    return activeDjs()[0]?.pubkey ?? null
  },
  isOnStage(pubkey: string | null): boolean {
    return !!pubkey && activeDjs().some((d) => d.pubkey === pubkey)
  },
}

/** Handles an incoming on-stage event (kind 30102). */
export function ingestStage(ev: Event): void {
  // created_at is in seconds — keep lastSeen in ms (compared against Date.now()).
  const lastSeen = ev.created_at * 1000
  const since = Number(ev.tags.find((t) => t[0] === 'since')?.[1]) || ev.created_at
  const on = ev.content !== 'off'
  const prev = state.djs[ev.pubkey]
  // Only consider the newest event per DJ.
  if (prev && lastSeen < prev.lastSeen) return
  // Read `since` DIRECTLY from the (newest) event — don't freeze the first one seen.
  // Otherwise long-watching clients keep a stale `since` on rejoin and the DJ order
  // (round-robin mapping pos%n) drifts between clients.
  state.djs[ev.pubkey] = { since, lastSeen, on }
  resolveConductor()
}

/**
 * Handles a stage kick (kind 30106). The kicked DJ (p-tag) counts as off-stage until they
 * post "on" again after the kick time. The CALLER (ClubView) ensures the kick comes from
 * an admin/moderator. Returns the kicked pubkey (for self-kick handling).
 */
export function ingestStageKick(ev: Event): string | null {
  const target = ev.tags.find((t) => t[0] === 'p')?.[1] ?? ev.tags.find((t) => t[0] === 'd')?.[1]
  if (!target) return null
  const kickMs = ev.created_at * 1000
  if (kickMs > (state.kicks[target] ?? 0)) {
    state.kicks[target] = kickMs
    resolveConductor()
  }
  return target
}

// ── my own stage presence ─────────────────────────────────────────────────

let hbTimer: ReturnType<typeof setInterval> | null = null
let myGroupId: string | null = null
let mySince = 0

function nowSec(): number {
  return Math.floor(Date.now() / 1000)
}

async function postStage(groupId: string, on: boolean): Promise<void> {
  await publishClub({
    kind: KIND_STAGE,
    created_at: nowSec(),
    tags: [
      ['h', groupId],
      ['d', groupId],
      ['since', String(mySince)],
    ],
    content: on ? 'on' : 'off',
  })
}

/** Go on stage: post on-stage + start the heartbeat. */
export async function joinStage(groupId: string): Promise<void> {
  myGroupId = groupId
  // On reload-resume reuse the SAME `since` (stable DJ order across all clients), else
  // fresh on a real stage join.
  mySince = persistedSince(groupId) ?? nowSec()
  ensureTicking()
  rememberStage(groupId, mySince) // for reload-resume (incl. stable since)
  await postStage(groupId, true)
  if (hbTimer) clearInterval(hbTimer)
  hbTimer = setInterval(() => {
    if (myGroupId) void postStage(myGroupId, true)
  }, HEARTBEAT_MS)
}

/**
 * Come down from the stage: post off + stop the heartbeat. `groupId` as fallback in case
 * `myGroupId` was lost after a reload (sticky stage → the relay still has you up, but the
 * module state is gone → otherwise you couldn't leave).
 */
export async function leaveStage(groupId?: string): Promise<void> {
  if (hbTimer) {
    clearInterval(hbTimer)
    hbTimer = null
  }
  forgetStage() // explicit leave → no more reload-resume
  const gid = myGroupId ?? groupId ?? null
  myGroupId = null
  // Optimistically come down locally (don't wait for the off round-trip).
  const me = auth.pubkey
  if (me && state.djs[me]) {
    state.djs[me] = { ...state.djs[me], on: false, lastSeen: Date.now() }
    resolveConductor()
  }
  if (gid) await postStage(gid, false)
}

export function resetStage(): void {
  if (hbTimer) {
    clearInterval(hbTimer)
    hbTimer = null
  }
  if (tickTimer) {
    clearInterval(tickTimer)
    tickTimer = null
  }
  // On logout/switch you might still be on stage (resetStage is called without a prior
  // leaveStage) → best-effort post `off` so others don't see us as an orphaned DJ. Fails
  // if the signer is already gone.
  if (myGroupId) void postStage(myGroupId, false)
  myGroupId = null
  state.djs = {}
  state.kicks = {}
  state.conductorPk = null
  hostPk = null
}

ensureTicking()

// Tab throttling/suspend stops the setInterval heartbeats in the background. When the user
// returns, immediately post a fresh heartbeat + re-evaluate freshness so the stage doesn't
// only reappear after the next 5s tick.
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') {
      state.tick = Date.now()
      resolveConductor()
      if (myGroupId) void postStage(myGroupId, true)
    }
  })
}

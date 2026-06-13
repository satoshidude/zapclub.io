import type { Event } from 'nostr-tools/pure'
import { KIND_STAGE, KIND_PRESENCE, publishClub } from './groups'
import { auth } from './auth.svelte'
import { selectActiveDjs, MAX_DJS } from './conductor'
import type { StageDj } from './types'

// zapclub: open stage — any member may take a free slot, up to MAX_DJS. The stage is a
// content event (30102), NOT a relay role; there is no dj-policy gate.
export { MAX_DJS }

const pk8 = (pk: string) => pk.slice(0, 8)

// Remembers (pubkey-bound) "I'm on stage in club X" → after a reload the client re-joins
// itself (the heartbeat would otherwise stop). Cleared on explicit leave/logout, NOT on
// reset (otherwise no reload-resume).
const ONSTAGE_KEY = 'zapclub:onstage'
// Per-club snapshot of all known on-stage DJs → pre-populates the stage display before the
// 30102 subscription returns. Prevents other DJs from flashing off on reload/navigate.
const STAGE_CACHE_PREFIX = 'zapclub:stage_djs:'
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
function cacheStage(groupId: string): void {
  try {
    const snapshot: Record<string, { since: number; lastSeen: number; on: boolean }> = {}
    for (const [pk, entry] of Object.entries(state.djs)) {
      if (entry.on) snapshot[pk] = entry
    }
    localStorage.setItem(STAGE_CACHE_PREFIX + groupId, JSON.stringify(snapshot))
  } catch {
    /* ignore */
  }
}
/** Pre-populate the stage display from the last-known cache for a club.
 *  Called before the 30102 subscription fires so other DJs don't flash off on reload. */
export function seedStageFromCache(groupId: string): void {
  try {
    const raw = localStorage.getItem(STAGE_CACHE_PREFIX + groupId)
    if (!raw) {
      console.log(`[zc:stage] seedFromCache: no cache ${groupId.slice(0, 8)}`)
      return
    }
    const snapshot = JSON.parse(raw) as Record<string, { since: number; lastSeen: number; on: boolean }>
    let seeded = 0
    for (const [pk, entry] of Object.entries(snapshot)) {
      if (!state.djs[pk]) {
        state.djs[pk] = { since: Number(entry.since), lastSeen: Number(entry.lastSeen), on: Boolean(entry.on) }
        seeded++
      }
    }
    console.log(`[zc:stage] seedFromCache: ${seeded}/${Object.keys(snapshot).length} djs seeded`)
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

// Heartbeat every 5 min — the 1h window (STALE_MS in conductor.ts) has huge headroom, so
// an infrequent keep-alive suffices (join/leave are posted immediately, plus an instant
// refresh when the tab returns). Keeps relay load/DB small.
const HEARTBEAT_MS = 300_000

interface StageState {
  /** pubkey → last state. */
  djs: Record<string, { since: number; lastSeen: number; on: boolean }>
  /** pubkey → last kick time (ms). Whoever posts "on" after a kick is back up. */
  kicks: Record<string, number>
  /** Reactive time tick so freshness filters re-evaluate. */
  tick: number
}

const state = $state<StageState>({ djs: {}, kicks: {}, tick: 0 })

// Pre-populate the current user's stage entry from localStorage before auth loads.
// This prevents a flash of "off-stage" during page reload while the signer is still loading
// and the 30102 subscription is refetching. The subscription will confirm/overwrite shortly.
;(function seedMyStage() {
  try {
    const raw = localStorage.getItem(ONSTAGE_KEY)
    if (!raw) {
      console.log('[zc:stage] seedMyStage: no marker')
      return
    }
    const o = JSON.parse(raw)
    if (!o?.pubkey || !o?.group || typeof o.since !== 'number') {
      console.log('[zc:stage] seedMyStage: invalid marker', o)
      return
    }
    state.djs[o.pubkey] = { since: o.since, lastSeen: Date.now(), on: true }
    console.log(`[zc:stage] seedMyStage: ${pk8(o.pubkey)} group=${o.group.slice(0, 8)} since=${o.since}`)
  } catch {
    /* ignore */
  }
})()

let tickTimer: ReturnType<typeof setInterval> | null = null
function ensureTicking(): void {
  if (tickTimer) return
  tickTimer = setInterval(() => {
    state.tick = Date.now()
  }, 5000)
}

/**
 * Active stage DJs: on + fresh (<1h) + not kicked (last on-heartbeat newer than the last
 * kick), sorted by stage join, max MAX_DJS.
 */
function activeDjs(): StageDj[] {
  return selectActiveDjs(state.djs, state.kicks, state.tick || Date.now())
}

export const stage = {
  get djs(): StageDj[] {
    return activeDjs()
  },
  get full(): boolean {
    return activeDjs().length >= MAX_DJS
  },
  isOnStage(pubkey: string | null): boolean {
    return !!pubkey && activeDjs().some((d) => d.pubkey === pubkey)
  },
  /** Raw lastSeen timestamp (ms) for a pubkey — bypasses the active-DJ filter. Used to
   *  distinguish a genuine live kick from a stale replayed kick event on page reload. */
  rawLastSeen(pubkey: string): number {
    return state.djs[pubkey]?.lastSeen ?? 0
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
  if (prev && lastSeen < prev.lastSeen) {
    console.log(`[zc:stage] ingestStage: skip old ${pk8(ev.pubkey)} on=${on}`)
    return
  }
  // Read `since` DIRECTLY from the (newest) event — don't freeze the first one seen.
  // Otherwise long-watching clients keep a stale `since` on rejoin and the DJ order
  // (round-robin mapping pos%n) drifts between clients.
  state.djs[ev.pubkey] = { since, lastSeen, on }
  const onCount = Object.values(state.djs).filter((d) => d.on).length
  console.log(`[zc:stage] ingestStage: ${pk8(ev.pubkey)} on=${on} since=${since} total_on=${onCount}`)
  const groupId = ev.tags.find((t) => t[0] === 'h')?.[1]
  if (groupId) cacheStage(groupId)
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
  }
  return target
}

// ── my own stage presence ─────────────────────────────────────────────────

let hbTimer: ReturnType<typeof setInterval> | null = null
let presTimer: ReturnType<typeof setInterval> | null = null
let myGroupId: string | null = null
let mySince = 0

function nowSec(): number {
  return Math.floor(Date.now() / 1000)
}

function postPresence(groupId: string): void {
  if (!auth.pubkey) return
  void publishClub({
    kind: KIND_PRESENCE,
    created_at: nowSec(),
    tags: [['h', groupId]],
    content: '',
  })
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
  // Switching to a different stage → step off the old one first (you can only DJ in one
  // club at a time). Same club = idempotent re-join (e.g. reload-resume), no off needed.
  console.log(`[zc:stage] joinStage: ${groupId.slice(0, 8)} prev=${myGroupId?.slice(0, 8) ?? 'none'}`)
  if (myGroupId && myGroupId !== groupId) void postStage(myGroupId, false)
  myGroupId = groupId
  // On reload-resume reuse the SAME `since` (stable DJ order across all clients), else
  // fresh on a real stage join.
  mySince = persistedSince(groupId) ?? nowSec()
  console.log(`[zc:stage] joinStage: since=${mySince} persisted=${persistedSince(groupId) !== null}`)
  ensureTicking()
  rememberStage(groupId, mySince) // for reload-resume (incl. stable since)
  await postStage(groupId, true)
  if (hbTimer) clearInterval(hbTimer)
  hbTimer = setInterval(() => {
    if (myGroupId) {
      console.log(`[zc:stage] heartbeat: ${myGroupId.slice(0, 8)}`)
      void postStage(myGroupId, true)
    }
  }, HEARTBEAT_MS)
  // Keep presence (20100) alive while on stage even when the club view isn't mounted.
  // ClubView.svelte starts/stops its own presence; this ensures a navigated-away DJ
  // remains "online" so the conductor's offline played-set guard doesn't apply to them.
  if (presTimer) clearInterval(presTimer)
  postPresence(groupId) // immediate beat
  presTimer = setInterval(() => {
    if (myGroupId) {
      console.log(`[zc:stage] presbeat: ${myGroupId.slice(0, 8)}`)
      postPresence(myGroupId)
    }
  }, 25_000)
}

/**
 * Come down from the stage: post off + stop the heartbeat. `groupId` as fallback in case
 * `myGroupId` was lost after a reload (sticky stage → the relay still has you up, but the
 * module state is gone → otherwise you couldn't leave).
 */
export async function leaveStage(groupId?: string): Promise<void> {
  console.log(`[zc:stage] leaveStage: myGroupId=${myGroupId?.slice(0, 8) ?? 'none'} fallback=${groupId?.slice(0, 8) ?? 'none'}`)
  if (hbTimer) {
    clearInterval(hbTimer)
    hbTimer = null
  }
  if (presTimer) {
    clearInterval(presTimer)
    presTimer = null
  }
  forgetStage() // explicit leave → no more reload-resume
  const gid = myGroupId ?? groupId ?? null
  myGroupId = null
  // Optimistically come down locally (don't wait for the off round-trip).
  const me = auth.pubkey
  if (me && state.djs[me]) {
    state.djs[me] = { ...state.djs[me], on: false, lastSeen: Date.now() }
  }
  if (gid) await postStage(gid, false)
}

/**
 * Clears only the stage DISPLAY (other DJs, kicks, conductor) — used when navigating
 * between clubs. Crucially does NOT post 'off' and does NOT stop the heartbeat, so you
 * stay on your current stage while browsing around (sticky until you switch stages or log
 * out). Without this, leaving the club view dropped you off the stage (visible on WebKit).
 *
 * Pass `leavingGroupId` (the club you're navigating away from). If the user is actively
 * on stage in that club (hbTimer running), their own entry is preserved so re-entering
 * the same club shows them on stage immediately (no flash while subscription refetches).
 */
export function clearStageView(leavingGroupId?: string): void {
  const me = auth.pubkey
  // Preserve own entry in two cases:
  // 1. SPA navigation: actively on stage (hbTimer running) in this club
  // 2. Reload-resume: localStorage says we're on stage here but hbTimer not yet running
  //    (happens when the subscription $effect re-runs after auth loads, before joinStage fires)
  const keepOwn =
    me &&
    leavingGroupId &&
    state.djs[me] &&
    ((leavingGroupId === myGroupId && !!hbTimer) || persistedSince(leavingGroupId) !== null)
  if (keepOwn) {
    const myEntry = state.djs[me]
    console.log(`[zc:stage] clearView: KEEP me=${pk8(me!)} hb=${!!hbTimer} persisted=${persistedSince(leavingGroupId!) !== null}`)
    state.djs = { [me]: myEntry }
  } else {
    console.log(`[zc:stage] clearView: WIPE me=${me ? pk8(me) : 'none'} leaving=${leavingGroupId?.slice(0, 8)} myGroup=${myGroupId?.slice(0, 8)} hb=${!!hbTimer} hadEntry=${me ? !!state.djs[me] : false}`)
    state.djs = {}
  }
  state.kicks = {}
}

export function resetStage(): void {
  if (hbTimer) {
    clearInterval(hbTimer)
    hbTimer = null
  }
  if (presTimer) {
    clearInterval(presTimer)
    presTimer = null
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
}

ensureTicking()

// Tab throttling/suspend stops the setInterval heartbeats in the background. When the user
// returns, immediately post a fresh heartbeat + re-evaluate freshness so the stage doesn't
// only reappear after the next 5s tick.
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') {
      state.tick = Date.now()
      if (myGroupId) void postStage(myGroupId, true)
    }
  })
}

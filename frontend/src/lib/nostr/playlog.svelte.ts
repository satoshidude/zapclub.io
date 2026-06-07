import type { Event } from 'nostr-tools/pure'
import { fetchClubPlayLog } from './groups'
import { LIVE_STALE_MS } from './sync.svelte'
import { parsePlayRecord, reconstructPlayed, type PlayRecord } from './playlog'

// Shared, conductor-independent round-robin progress. The conductor emits a kind-1313 play
// record on every track start; every client ingests them (live subscription + poll backstop,
// exactly like the DJ queues) and reconstructs the played-set with the PURE logic in
// playlog.ts. So a rescuer/bootstrapper/fresh-mount continues where the room left off instead
// of replaying away DJs' tracks. See playlog.ts for the session/loop semantics.

// A gap larger than this between consecutive plays means the room fell to the lobby → a new
// session. Tied to the lobby-fallback threshold (single source) so they can't drift.
const SESSION_GAP_MS = LIVE_STALE_MS
// Rolling query/memory bound (DoS guard) — NOT the session definition. 6h.
export const SESSION_LOOKBACK_MS = 21_600_000

// eventId → record. Map dedupes re-ingestion from the poll vs the subscription.
const state = $state<{ byId: Record<string, PlayRecord> }>({ byId: {} })

/** Handles an incoming play record (kind 1313). Idempotent; prunes anything older than the
 *  lookback window so the map stays bounded. */
export function ingestPlay(ev: Event): void {
  const rec = parsePlayRecord(ev)
  if (!rec) return
  if (state.byId[ev.id]) return // already have it
  const cutoff = Date.now() - SESSION_LOOKBACK_MS
  const next: Record<string, PlayRecord> = { [ev.id]: rec }
  for (const [id, r] of Object.entries(state.byId)) {
    if (r.startedAt >= cutoff) next[id] = r
  }
  state.byId = next
}

/** Reconstructed played-set + loop epoch for the current session, given the live track. */
export function sessionPlayed(currentVideoId: string | null): { played: Set<string>; loop: number } {
  return reconstructPlayed(Object.values(state.byId), SESSION_GAP_MS, currentVideoId)
}

// ── poll backstop (mirrors queue.svelte.ts) ─────────────────────────────────
const PLAYLOG_SYNC_MS = 15_000
let syncTimer: ReturnType<typeof setInterval> | null = null
let syncGroup: string | null = null
let syncing = false

/** Re-query the club's play-log and ingest it (one-shot, overlap-guarded). */
export async function refreshPlayLog(groupId: string): Promise<void> {
  if (syncing) return
  syncing = true
  try {
    const events = await fetchClubPlayLog(groupId, Date.now() - SESSION_LOOKBACK_MS)
    for (const ev of events) ingestPlay(ev)
  } catch {
    /* transient — next tick retries */
  } finally {
    syncing = false
  }
}

export function startPlayLogSync(groupId: string): void {
  if (syncGroup === groupId && syncTimer) return
  stopPlayLogSync()
  syncGroup = groupId
  void refreshPlayLog(groupId)
  syncTimer = setInterval(() => {
    if (syncGroup) void refreshPlayLog(syncGroup)
  }, PLAYLOG_SYNC_MS)
}

export function stopPlayLogSync(): void {
  if (syncTimer) {
    clearInterval(syncTimer)
    syncTimer = null
  }
  syncGroup = null
}

export function resetPlayLog(): void {
  state.byId = {}
}

// Refresh on tab return (background throttles the interval) so a takeover has a current set.
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && syncGroup) void refreshPlayLog(syncGroup)
  })
}

<script lang="ts">
  // Persistent, UI-less conductor engine. The club's conducting (now_playing heartbeat +
  // round-robin advance) normally only runs inside ClubView; navigating away would freeze the
  // room. This keeps the conductor driving MY stage club while I'm NOT on a club page — so my
  // playlist keeps running even when I'm the conductor and browsing elsewhere.
  //
  // Route-gated to be mutually exclusive with ClubView (which owns the same singletons on a
  // club page): active iff signed-in + on a stage + route !== 'club'. Reuses the exact same
  // conductor machinery (subscribeClub / conductorTick / queue + play-log sync) — no duplication.
  import { auth } from '../nostr/auth.svelte'
  import { router } from '../router.svelte'
  import {
    persistedStageGroup,
    ingestStage,
    ingestStageKick,
    leaveStage,
    joinStage,
    setStageHost,
    clearStageView,
    stage,
  } from '../nostr/stage.svelte'
  import { subscribeClub, parseOwner, parseMembers } from '../nostr/groups'
  import {
    ingestNowPlaying,
    conductorTick,
    resetSync,
    ingestSkipIntent,
    clearSkipIntent,
    isActingConductor,
    skipTrack,
    sync,
  } from '../nostr/sync.svelte'
  import { ingestQueue, resetQueues, startQueueSync, stopQueueSync } from '../nostr/queue.svelte'
  import { ingestPlay, startPlayLogSync, stopPlayLogSync, resetPlayLog } from '../nostr/playlog.svelte'
  import { startPresence, stopPresence } from '../nostr/presence.svelte'
  import type { ClubMember } from '../nostr/types'

  let ownerPk = $state<string | null>(null)
  let members = $state<ClubMember[]>([])
  const isModerator = (pk: string) =>
    !!pk && members.some((m) => m.pubkey === pk && m.roles.includes('moderator'))

  // Boolean (not the route object) so navigating between NON-club pages (home↔profile↔howto)
  // doesn't churn the engine — it only flips when entering/leaving a club page.
  const offClub = $derived(router.route.name !== 'club')

  // Engine lifecycle — re-runs only when activation actually flips (offClub / auth).
  $effect(() => {
    if (!auth.canSign) return
    if (!offClub) return // ClubView owns the engine on a club page
    const gid = persistedStageGroup()
    if (!gid) return
    const me = auth.pubkey

    // Clean start (like a ClubView mount), then take over the club's content stream.
    resetSync()
    clearStageView()
    resetQueues()
    resetPlayLog()
    ownerPk = null
    members = []

    // Stay on stage (post + keep the 5-min heartbeat) so I remain the conductor off-page —
    // mirrors ClubView's reload-resume. Idempotent.
    void joinStage(gid)
    startPresence(gid)

    const stop = subscribeClub(gid, {
      onMembers: (ev) => (members = parseMembers(ev)),
      onAdmins: (ev) => (ownerPk = parseOwner(ev)),
      // Hijack-guard like ClubView: only accept now_playing from a DJ on stage (or bootstrap).
      onNowPlaying: (ev) => {
        if (stage.djs.length === 0 || stage.isOnStage(ev.pubkey)) ingestNowPlaying(ev)
      },
      onStage: ingestStage,
      onStageKick: (ev) => {
        if (!isModerator(ev.pubkey)) return
        const kicked = ingestStageKick(ev)
        if (kicked && kicked === me && stage.isOnStage(me)) void leaveStage(gid)
      },
      onQueue: ingestQueue,
      onSkip: ingestSkipIntent,
      onPlay: ingestPlay,
    })
    startQueueSync(gid)
    startPlayLogSync(gid)
    // Self-driving tick (advance/heartbeat). First fire after 6s → the subscription has
    // delivered the current now_playing/stage/queues by then, so no bootstrap jump.
    // NOTE: ownerPk is read ONLY here inside the interval (untracked) — never synchronously
    // in the effect body, or onAdmins writing it would re-trigger this effect in a loop
    // (resubscribe storm).
    const tick = setInterval(() => {
      setStageHost(ownerPk) // keep owner-override correct as 39001 arrives
      conductorTick(gid)
    }, 6000)

    return () => {
      stop()
      clearInterval(tick)
      stopQueueSync()
      stopPlayLogSync()
      stopPresence()
      // Do NOT resetSync here — entering a club lets ClubView reset+own; the next activation
      // resets at its start.
    }
  })

  // Enact a skip requested by an owner/moderator while I'm the (off-page) conductor.
  $effect(() => {
    const intent = sync.skipIntent
    if (!intent) return
    if (!offClub) return
    if (!isActingConductor()) return
    const authorized =
      intent.author === ownerPk || isModerator(intent.author) || intent.author === stage.conductor
    const fresh = Date.now() - intent.at < 60_000
    if (authorized && fresh && sync.live && intent.pos === sync.live.pos) {
      clearSkipIntent()
      skipTrack(persistedStageGroup() ?? '')
    } else if (!sync.live || (sync.live && intent.pos !== sync.live.pos)) {
      clearSkipIntent()
    }
  })
</script>

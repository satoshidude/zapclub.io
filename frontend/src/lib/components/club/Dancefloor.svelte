<script lang="ts">
  import { presence } from '../../nostr/presence.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import { npubEncode } from 'nostr-tools/nip19'
  import { goUser } from '../../router.svelte'
  import type { ClubMember } from '../../nostr/types'
  import { zaps } from '../../nostr/zaps.svelte'
  import { stage, joinStage, leaveStage, MAX_DJS } from '../../nostr/stage.svelte'
  import { autodj } from '../../nostr/autodj.svelte'
  import { kickFromStage } from '../../nostr/groups'
  import { reactivateMyQueue } from '../../nostr/queue.svelte'
  import ComingNext from './ComingNext.svelte'

  let {
    groupId,
    members,
    canModerate = false,
    isOwner = false,
    isMember = false,
    owner = '',
    currentDj = '',
    autoPlaying = false,
    onkick,
    onpromote,
  }: {
    groupId: string
    members: ClubMember[]
    canModerate?: boolean
    isOwner?: boolean
    isMember?: boolean
    owner?: string
    currentDj?: string
    autoPlaying?: boolean
    onkick?: (pubkey: string) => void
    onpromote?: (pubkey: string) => void
  } = $props()

  // DJs currently on stage — the floor's front row (even if their presence beat is a little
  // stale; being on stage means they're here). They are NOT repeated in the crowd below.
  const stageDjs = $derived(stage.djs)
  const autoDJ = $derived(autodj.getConfig(groupId))
  const stageSet = $derived(new Set(stageDjs.map((d) => d.pubkey)))
  const onStage = $derived(stage.isOnStage(auth.pubkey))
  const occupiedSlots = $derived(stageDjs.length + (autoDJ ? 1 : 0))
  const emptySlots = $derived(Math.max(0, MAX_DJS - occupiedSlots))
  // A free slot can be taken directly by a signed-in member who isn't on stage yet.
  const canJoin = $derived(auth.canSign && isMember && !onStage && occupiedSlots < MAX_DJS)
  let stageBusy = $state(false)
  let stageError = $state('')

  async function goStage() {
    stageBusy = true
    stageError = ''
    try {
      await joinStage(groupId) // just join the rotation — the round-robin interleaves my set
      void reactivateMyQueue(groupId) // bring my FULL set (clear stale played-flags from before)
    } catch (e) {
      stageError = String((e as Error)?.message ?? e)
    } finally {
      stageBusy = false
    }
  }
  async function offStage() {
    stageBusy = true
    stageError = ''
    try {
      await leaveStage(groupId)
    } catch (e) {
      stageError = String((e as Error)?.message ?? e)
    } finally {
      stageBusy = false
    }
  }
  async function unstage(pubkey: string) {
    stageError = ''
    try {
      await kickFromStage(groupId, pubkey)
    } catch (e) {
      stageError = String((e as Error)?.message ?? e)
    }
  }

  // A DJ is actually playing → the floor dances; otherwise it just idles (no one's on).
  const playing = $derived(!!currentDj)

  // Zap bounce: when a fresh zap lands, the zapped DJ's avatar jumps briefly.
  let zapped = $state<string | null>(null)
  let lastZapAt = 0
  $effect(() => {
    const lz = zaps.lastZap
    if (lz && lz.at !== lastZapAt) {
      lastZapAt = lz.at
      zapped = lz.dj
      const t = setTimeout(() => (zapped = null), 1600)
      return () => clearTimeout(t)
    }
  })

  // Deterministic per-pubkey dance — stable across renders (no Math.random), so the crowd looks
  // varied but doesn't reshuffle. Each avatar gets 89–110 BPM, a phase, a motion variant and a
  // small scatter offset. One animation cycle equals one beat.
  function hash(pk: string): number {
    let h = 2166136261
    for (let i = 0; i < pk.length; i++) h = (Math.imul(h ^ pk.charCodeAt(i), 16777619)) >>> 0
    return h
  }
  function danceVars(pk: string): string {
    const h = hash(pk)
    const bpm = 89 + ((h >>> 0) % 22)
    const dur = 60 / bpm
    const phase = ((h >>> 5) % 100) / 100
    const delay = -(dur * phase)
    const dx = (((h >>> 17) % 9) - 4).toFixed(0) // -4..4 px scatter
    const dy = (((h >>> 21) % 7) - 3).toFixed(0) // -3..3 px scatter
    // Only time/offset vars here — NO CSS var inside the keyframe transforms or animation-name
    // (iOS Safari resolves those unreliably → no animation). Amplitude is baked into the keyframes.
    return `--dur:${dur.toFixed(4)}s;--delay:${delay.toFixed(4)}s;--dx:${dx}px;--dy:${dy}px`
  }
  const variantOf = (pk: string) => (hash(pk) >>> 9) % 6

  // Click an avatar → a small card (profile link + moderation).
  let selected = $state<string | null>(null)
  const sel = $derived(selected ? members.find((m) => m.pubkey === selected) ?? null : null)
  function roleLabel(m: ClubMember): string {
    if (m.pubkey === owner) return 'host'
    if (m.roles.includes('moderator')) return 'mod'
    return ''
  }
  function openProfile(pk: string) {
    selected = null
    goUser(npubEncode(pk))
  }

</script>

<section class="floor stage-card card led-zone" class:playing>
  <div class="led-scanlines" aria-hidden="true"></div>
  <div class="head lcd-card-heading">
    <span class="head-title"><h3 class="lcd-card-title">On stage</h3><span class="count">{occupiedSlots}/{MAX_DJS}</span></span>
    {#if auth.canSign && isMember && onStage}
      <button class="leave-stage lcd-card-title" onclick={offStage} disabled={stageBusy}>Leave stage</button>
    {/if}
  </div>

  {#if occupiedSlots === 0}
    <p class="dim">No one is on stage yet.</p>
  {/if}

  <!-- Stage row: the on-stage DJs dance up front, right against the crowd. Open slots are
       joinable in place; the people live ONLY here (not repeated in the crowd below). -->
  <div class="stagerow">
    <span class="stage-tag" aria-hidden="true">{occupiedSlots}/{MAX_DJS} ON STAGE</span>
    {#each stageDjs as dj (dj.pubkey)}
      {@const profile = useProfile(dj.pubkey)}
      <button
        class="dancer up-front"
        class:dj={dj.pubkey === currentDj}
        class:no-ring={profile?.bot}
        class:zapped={zapped === dj.pubkey}
        style={danceVars(dj.pubkey)}
        title={displayName(dj.pubkey, profile)}
        onclick={() => (selected = selected === dj.pubkey ? null : dj.pubkey)}
      >
        <span class="bob v{variantOf(dj.pubkey)}">
          <img class="av" src={avatarUrl(dj.pubkey, profile)} alt="" width="64" height="64" loading="lazy" />
        </span>
        <span class="nm"><span class="mq-inner">{displayName(dj.pubkey, profile)}</span></span>
      </button>
    {/each}
    {#if autoDJ}
      <div
        class="dancer up-front auto-dj"
        class:dj={autoPlaying}
        style={danceVars(`autodj:${groupId}`)}
        role="img"
        aria-label={`Auto DJ on stage — ${autoDJ.name}`}
        title={`Auto DJ — ${autoDJ.name}`}
      >
        <span class="bob v{variantOf(`autodj:${groupId}`)}">
          <span class="auto-avatar" aria-hidden="true">
            <svg viewBox="0 0 24 24"><path d="M13.2 2 5.5 13h5.7L10.8 22l7.7-11h-5.7L13.2 2z"></path></svg>
          </span>
        </span>
        <span class="nm"><span class="mq-inner">Auto DJ</span></span>
      </div>
    {/if}
    {#each Array(emptySlots) as _, i (i)}
      <button
        class="dancer open"
        class:joinable={canJoin}
        onclick={goStage}
        disabled={!canJoin || stageBusy}
        title={canJoin ? 'Take this spot' : ''}
      >
        <span class="ring">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M4 14v-2a8 8 0 0 1 16 0v2"></path>
            <path d="M4 14h2.5v6H5.75A1.75 1.75 0 0 1 4 18.25V14ZM20 14h-2.5v6h.75A1.75 1.75 0 0 0 20 18.25V14Z"></path>
          </svg>
        </span>
        <span class="nm">{canJoin ? 'Join stage' : 'open'}</span>
      </button>
    {/each}
  </div>
  <ComingNext clubId={groupId} />
  {#if stageError}<p class="dim err">⚠ {stageError}</p>{/if}

  {#if sel && stageSet.has(sel.pubkey)}
    {@const profile = useProfile(sel.pubkey)}
    <div class="card-pop">
      <img class="av" src={avatarUrl(sel.pubkey, profile)} alt="" width="36" height="36" />
      <div class="who">
        <span class="nm2"><span class="mq-inner">{displayName(sel.pubkey, profile)}</span></span>
        {#if roleLabel(sel)}<span class="role">{roleLabel(sel)}</span>{/if}
        {#if presence.isOnline(sel.pubkey)}<span class="here">● here</span>{/if}
      </div>
      <button class="link" onclick={() => openProfile(sel.pubkey)}>Profile ↗</button>
      {#if canModerate && sel.pubkey !== auth.pubkey}
        <button class="mini" onclick={() => { void unstage(sel.pubkey); selected = null }}>off stage</button>
      {/if}
      {#if canModerate && sel.pubkey !== owner && sel.pubkey !== auth.pubkey}
        {#if isOwner && !sel.roles.includes('moderator')}
          <button class="mini" onclick={() => { onpromote?.(sel.pubkey); selected = null }}>+mod</button>
        {/if}
        <button class="mini danger" onclick={() => { onkick?.(sel.pubkey); selected = null }}>kick</button>
      {/if}
      <button class="x" aria-label="Close" onclick={() => (selected = null)}>✕</button>
    </div>
  {/if}
</section>

<style>
  .stage-card {
    grid-area: stage;
  }
  .floor {
    position: relative;
    overflow: hidden;
    background: transparent;
    border: 0;
    border-radius: 0;
    padding: 0.8rem 1rem 1rem;
    min-width: 0;
    height: 100%;
    color: var(--lcd-text);
    box-shadow: none;
    font-family: 'DotGothic16', ui-monospace, monospace;
    text-shadow: none;
  }
  .floor > :not(.led-scanlines) {
    position: relative;
    z-index: 1;
  }
  .led-scanlines {
    position: absolute;
    inset: 0;
    z-index: 0;
    display: none;
    pointer-events: none;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  .head-title {
    display: flex;
    align-items: baseline;
    gap: 0.45rem;
  }
  h3 {
    margin: 0;
  }
  .count {
    color: var(--lcd-text);
    font-weight: 700;
    font-size: 0.82rem;
    font-variant-numeric: tabular-nums;
  }
  .leave-stage {
    border: 0;
    background: transparent;
  }
  .dim {
    color: var(--lcd-text-dim);
    font-size: 0.85rem;
    margin: 0.3rem 0;
  }

  /* Stage row: the front of the floor. Slightly bigger dancers, a soft platform glow, and a
     dashed edge towards the crowd right below. */
  .stagerow {
    --stage-avatar-size: 88px;
    position: relative;
    display: grid;
    grid-template-columns: repeat(3, minmax(88px, 1fr));
    gap: 0.75rem;
    align-items: flex-end;
    padding: 1rem 0 0.85rem;
    margin-top: 0;
    border: 0;
    background: transparent;
  }
  .stage-tag {
    display: none;
  }
  .stagerow .dancer {
    width: 100%;
  }
  .stagerow .dancer .av {
    width: var(--stage-avatar-size);
    height: var(--stage-avatar-size);
  }
  .stagerow .nm {
    max-width: 92px;
    --nm-w: 92px;
    font-size: 13px;
  }
  /* Open slot / leave control as a dancer-shaped column so it lines up with the row. */
  .dancer.open .ring {
    width: var(--stage-avatar-size, 58px);
    height: var(--stage-avatar-size, 58px);
    border-radius: 50%;
    border: 2px dashed rgba(201, 206, 209, 0.42);
    display: grid;
    place-items: center;
    font-size: 1.25rem;
    color: var(--lcd-text-dim);
  }
  .dancer.open .ring svg {
    width: 30px;
    height: 30px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.7;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .dancer.open {
    opacity: 0.58;
    cursor: default;
  }
  .dancer.open.joinable,
  .dancer.open.leave {
    opacity: 1;
    cursor: pointer;
  }
  .dancer.open.joinable .ring {
    border-color: var(--lcd-text);
    color: var(--lcd-text);
  }
  .dancer.open.joinable:hover:not(:disabled) .ring {
    background: rgba(241, 243, 244, 0.08);
  }
  .dancer.open:disabled {
    cursor: default;
  }
  .err {
    color: var(--danger);
  }

  @keyframes floatUp {
    0% { opacity: 0; transform: translateY(0) scale(0.6); }
    15% { opacity: 1; transform: translateY(-10px) scale(1.1); }
    100% { opacity: 0; transform: translateY(-150px) scale(1); }
  }

  .dancer {
    position: relative;
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 3px;
    width: 66px;
    transform: translate(var(--dx, 0), var(--dy, 0));
  }
  .dancer:focus-visible {
    outline: 1px dashed #cfe9ff;
    outline-offset: 3px;
  }
  .bob {
    position: relative;
    display: block;
    border-radius: 50%;
    -webkit-mask-image: none;
    mask-image: none;
    will-change: transform;
    transform-origin: center bottom;
  }
  .bob::after {
    content: '';
    position: absolute;
    inset: 0;
    background-image: radial-gradient(circle, rgba(244, 246, 247, 0.12) 0 0.55px, transparent 0.72px);
    background-size: 4px 4px;
    mix-blend-mode: screen;
    pointer-events: none;
  }
  .av {
    border-radius: 50%;
    object-fit: cover;
    background: #1a1d20;
    display: block;
  }
  .dancer .av {
    border: 0;
    opacity: 0.94;
    filter: grayscale(0.25) saturate(0.85) brightness(1.03) contrast(1.08);
  }
  /* The playing DJ is brighter, not glowing. */
  .dancer.up-front .av {
    opacity: 0.97;
  }
  .dancer.up-front.no-ring .av {
    opacity: 0.92;
  }
  .dancer.auto-dj {
    cursor: default;
  }
  .auto-avatar {
    display: grid;
    place-items: center;
    width: var(--stage-avatar-size);
    height: var(--stage-avatar-size);
    border: 1px solid rgba(241, 243, 244, 0.34);
    border-radius: 50%;
    color: var(--lcd-text-soft);
    background:
      radial-gradient(circle, rgba(241, 243, 244, 0.08) 0 1px, transparent 1.2px) 0 0 / 5px 5px,
      rgba(241, 243, 244, 0.025);
  }
  .auto-avatar svg {
    width: 43%;
    height: 43%;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .dancer.auto-dj.dj .auto-avatar {
    border-color: rgba(241, 243, 244, 0.62);
    color: var(--lcd-text);
  }
  .dancer.dj .av {
    opacity: 1;
    filter: grayscale(0.18) saturate(0.9) brightness(1.1) contrast(1.12);
  }
  .nm {
    max-width: 66px;
    --nm-w: 66px;
    overflow: hidden;
    white-space: nowrap;
    color: var(--lcd-text);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 13px;
    font-style: normal;
    font-weight: 400;
    letter-spacing: 0.07em;
    text-shadow: 0 0 3px rgba(179, 222, 255, 0.72), 0 0 8px rgba(90, 160, 255, 0.2);
    text-transform: uppercase;
  }
  .nm .mq-inner {
    display: inline-block;
  }
  /* Scroll on hover (desktop); always on touch */
  .dancer:hover .nm .mq-inner,
  .dancer:focus-visible .nm .mq-inner {
    animation: nm-scroll 3s ease-in-out infinite;
  }
  @media (hover: none) {
    .nm .mq-inner {
      animation: nm-scroll 4s ease-in-out 0.8s infinite;
    }
  }
  @keyframes nm-scroll {
    0%, 25%  { transform: translateX(0); }
    75%, 100% { transform: translateX(min(0px, calc(var(--nm-w) - 100%))); }
  }
  /* The dance: 6 deterministic variants, only while a DJ is playing. Per-pubkey duration/delay
     (vars, iOS-safe) give varied phases → no lockstep. animation-name comes from the concrete
     variant class (NOT a CSS var) so iOS Safari resolves the keyframes. Every variant has exactly
     one vertical peak per cycle, keeping its visible bounce inside the 89–110 BPM range. */
  .floor.playing .bob {
    animation-duration: var(--dur, 0.9s);
    animation-delay: var(--delay, 0s);
    animation-iteration-count: infinite;
    animation-timing-function: ease-in-out;
  }
  .floor.playing .v0 { animation-name: dance0; }
  .floor.playing .v1 { animation-name: dance1; }
  .floor.playing .v2 { animation-name: dance2; }
  .floor.playing .v3 { animation-name: dance3; }
  .floor.playing .v4 { animation-name: dance4; }
  .floor.playing .v5 { animation-name: dance5; }

  @keyframes dance0 { /* bounce */
    0%, 100% { transform: translateY(0) scaleY(1); }
    50% { transform: translateY(-10px) scaleY(1.05); }
  }
  @keyframes dance1 { /* sway + bob */
    0%, 100% { transform: translateY(-1px) rotate(-7deg); }
    50% { transform: translateY(-5px) rotate(7deg); }
  }
  @keyframes dance2 { /* headbob */
    0%, 100% { transform: translateY(0) rotate(-2deg); }
    50% { transform: translateY(-7px) rotate(2deg); }
  }
  @keyframes dance3 { /* two-step bounce */
    0%, 100% { transform: translateX(-4px) translateY(0); }
    50% { transform: translateX(4px) translateY(-8px); }
  }
  @keyframes dance4 { /* soft hop with a small tilt */
    0%, 100% { transform: translateY(0) rotate(3deg) scale(1); }
    50% { transform: translateY(-9px) rotate(-4deg) scale(1.04); }
  }
  @keyframes dance5 { /* diagonal club step */
    0%, 100% { transform: translateX(3px) translateY(0) rotate(4deg); }
    50% { transform: translateX(-3px) translateY(-6px) rotate(-5deg); }
  }

  /* Zap landed on this DJ → a brief brighter LCD pulse, without glow. */
  .dancer.zapped .av {
    animation: zapPulse 0.4s ease-out 3;
    opacity: 1;
    filter: grayscale(0.15) saturate(0.95) brightness(1.24) contrast(1.16);
  }
  @keyframes zapPulse {
    0%, 100% { transform: scale(1); }
    50% { transform: scale(1.25); }
  }
  .dancer.zapped::after {
    content: '⚡';
    position: absolute;
    top: -4px;
    right: 0;
    font-size: 1.1rem;
    z-index: 4;
    pointer-events: none;
    animation: floatUp 1.4s ease-out;
    color: #eef9ff;
  }

  .card-pop {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-top: 0.6rem;
    padding: 0.5rem 0.6rem;
    background: rgba(9, 28, 60, 0.9);
    border: 1px solid rgba(207, 233, 255, 0.3);
    border-radius: var(--radius-sm);
  }
  .card-pop .av {
    width: 36px;
    height: 36px;
    flex: 0 0 auto;
  }
  .who {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .nm2 {
    font-weight: 600;
    font-size: 0.88rem;
    overflow: hidden;
    white-space: nowrap;
  }
  .nm2 .mq-inner {
    display: inline-block;
    animation: nm2-scroll 4s ease-in-out 0.5s infinite;
  }
  @keyframes nm2-scroll {
    0%, 25%  { transform: translateX(0); }
    75%, 100% { transform: translateX(min(0px, calc(160px - 100%))); }
  }
  .role {
    font-size: 0.68rem;
    color: #b8dcfa;
  }
  .here {
    font-size: 0.66rem;
    color: #d2ecff;
  }
  .link {
    background: none;
    border: 1px solid rgba(207, 233, 255, 0.3);
    color: #cfe9ff;
    border-radius: 7px;
    padding: 0.25rem 0.5rem;
    font-size: 0.76rem;
    cursor: pointer;
    flex: 0 0 auto;
  }
  .mini {
    background: rgba(13, 31, 66, 0.7);
    border: 1px solid rgba(207, 233, 255, 0.25);
    color: #9fc8ed;
    border-radius: 7px;
    padding: 0.25rem 0.45rem;
    font-size: 0.72rem;
    cursor: pointer;
    flex: 0 0 auto;
  }
  .mini.danger:hover {
    color: var(--danger);
    border-color: var(--danger);
  }
  .x {
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    flex: 0 0 auto;
  }


  @media (prefers-reduced-motion: reduce) {
    .floor.playing .bob,
    .dancer.zapped .av,
    .dancer.zapped::after {
      animation: none !important;
    }
  }
  @media (max-width: 560px) {
    .floor { padding-inline: 0.75rem; }
    .stagerow {
      --stage-avatar-size: 64px;
      grid-template-columns: repeat(3, minmax(64px, 1fr));
      gap: 0.3rem;
      padding-top: 0.85rem;
    }
    .dancer.open .ring svg { width: 25px; height: 25px; }
    .stagerow .nm { max-width: 68px; --nm-w: 68px; font-size: 13px; }
  }
</style>

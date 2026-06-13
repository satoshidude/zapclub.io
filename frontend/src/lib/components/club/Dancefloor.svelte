<script lang="ts">
  import { presence } from '../../nostr/presence.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import { npubEncode } from 'nostr-tools/nip19'
  import { goUser } from '../../router.svelte'
  import type { ClubMember } from '../../nostr/types'
  import { chat } from '../../nostr/chat.svelte'
  import { emotes, sendEmote } from '../../nostr/emotes.svelte'
  import { zaps } from '../../nostr/zaps.svelte'
  import { stage, joinStage, leaveStage, MAX_DJS } from '../../nostr/stage.svelte'
  import { kickFromStage } from '../../nostr/groups'
  import { reactivateMyQueue } from '../../nostr/queue.svelte'
  import { ownPremium } from '../../nostr/premium.svelte'
  import VibeMeter from './VibeMeter.svelte'
  import ComingNext from './ComingNext.svelte'

  let {
    groupId,
    members,
    canChat,
    canModerate = false,
    isOwner = false,
    isMember = false,
    owner = '',
    currentDj = '',
    onkick,
    onpromote,
    ondelete,
  }: {
    groupId: string
    members: ClubMember[]
    canChat: boolean
    canModerate?: boolean
    isOwner?: boolean
    isMember?: boolean
    owner?: string
    currentDj?: string
    onkick?: (pubkey: string) => void
    onpromote?: (pubkey: string) => void
    ondelete?: (eventId: string) => void
  } = $props()

  // DJs currently on stage — the floor's front row (even if their presence beat is a little
  // stale; being on stage means they're here). They are NOT repeated in the crowd below.
  const stageDjs = $derived(stage.djs)
  const stageSet = $derived(new Set(stageDjs.map((d) => d.pubkey)))
  const onStage = $derived(stage.isOnStage(auth.pubkey))
  const emptySlots = $derived(Math.max(0, MAX_DJS - stageDjs.length))
  // A free slot can be taken directly by a signed-in member who isn't on stage yet.
  const canJoin = $derived(auth.canSign && isMember && !onStage && !stage.full)
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

  // Crowd = the club's members minus the stage row. ONLINE members (recent presence beat)
  // dance; the rest are part of the club but shown dimmed + still ("here vs away").
  const online = $derived(members.filter((m) => !stageSet.has(m.pubkey) && presence.isOnline(m.pubkey)))
  const offline = $derived(members.filter((m) => !stageSet.has(m.pubkey) && !presence.isOnline(m.pubkey)))

  const CAP = 48
  const shownOnline = $derived(online.slice(0, CAP))
  const moreOnline = $derived(Math.max(0, online.length - CAP))
  const OFF_CAP = 24
  const shownOffline = $derived(offline.slice(0, OFF_CAP))
  const moreOffline = $derived(Math.max(0, offline.length - OFF_CAP))

  // A DJ is actually playing → the floor dances; otherwise it just idles (no one's on).
  const playing = $derived(!!currentDj)

  // Reactive clock so chat bubbles expire without new events.
  let nowMs = $state(Date.now())
  $effect(() => {
    const t = setInterval(() => (nowMs = Date.now()), 1000)
    return () => clearInterval(t)
  })

  // Chat bubbles: the latest message per author within the last 6 s, shown over their avatar.
  const BUBBLE_MS = 6000
  const bubbleByPubkey = $derived.by(() => {
    const map: Record<string, string> = {}
    for (const m of chat.messages) {
      if (nowMs - m.createdAt * 1000 <= BUBBLE_MS) map[m.pubkey] = m.content
    }
    return map
  })

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

  // Energy: recent chat + emotes + a zap make the floor a touch faster.
  const hyped = $derived(Object.keys(bubbleByPubkey).length + emotes.items.length + (zapped ? 2 : 0) >= 4)

  // Send an ASCII shortcode (not the raw emoji): some signers/extensions choke on signing
  // multi-byte unicode content, so we sign a stable code and render the emoji client-side.
  const EMOTES = [
    { e: '🔥', c: 'fire' },
    { e: '🙌', c: 'raise' },
    { e: '💜', c: 'love' },
    { e: '🕺', c: 'dance' },
    { e: '👏', c: 'clap' },
  ]
  const CODE2EMOJI: Record<string, string> = { fire: '🔥', raise: '🙌', love: '💜', dance: '🕺', clap: '👏' }
  const showEmote = (content: string) => CODE2EMOJI[content] ?? content // fallback: raw emoji from other clients
  function emit(code: string) {
    if (groupId) void sendEmote(groupId, code)
  }
  // Deterministic horizontal lane (10–90%) for a flying emote, from its id.
  const emoteX = (id: string) => (hash(id) % 80) + 10

  // Deterministic per-pubkey dance — stable across renders (no Math.random), so the crowd looks
  // varied but doesn't reshuffle. Encodes variant, duration, phase offset, amplitude and a small
  // scatter offset as CSS vars.
  function hash(pk: string): number {
    let h = 2166136261
    for (let i = 0; i < pk.length; i++) h = (Math.imul(h ^ pk.charCodeAt(i), 16777619)) >>> 0
    return h
  }
  function danceVars(pk: string): string {
    const h = hash(pk)
    const dur = (0.7 + ((h >>> 0) % 60) / 100).toFixed(2) // 0.70–1.29s (varied tempo)
    const delay = (-(((h >>> 5) % 130) / 100)).toFixed(2) // 0 to -1.29s (phase offset → no lockstep)
    const dx = (((h >>> 17) % 9) - 4).toFixed(0) // -4..4 px scatter
    const dy = (((h >>> 21) % 7) - 3).toFixed(0) // -3..3 px scatter
    // Only time/offset vars here — NO CSS var inside the keyframe transforms or animation-name
    // (iOS Safari resolves those unreliably → no animation). Amplitude is baked into the keyframes.
    return `--dur:${dur}s;--delay:${delay}s;--dx:${dx}px;--dy:${dy}px`
  }
  const variantOf = (pk: string) => hash(pk) % 4

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

<section class="floor card" class:playing class:hyped>
  <div class="head">
    <h3>In Da Club</h3>
    <span class="count" title="dancing now / club members">{online.length + stageDjs.length} / {members.length}</span>
  </div>

  <VibeMeter clubId={groupId} />

  {#if online.length === 0 && offline.length === 0 && stageDjs.length === 0}
    <p class="dim">No one here yet — be the first on the floor.</p>
  {/if}

  <!-- Stage row: the on-stage DJs dance up front, right against the crowd. Open slots are
       joinable in place; the people live ONLY here (not repeated in the crowd below). -->
  <div class="stagerow">
    <span class="stage-tag" aria-hidden="true">{stageDjs.length}/{MAX_DJS} ON STAGE</span>
    {#if auth.canSign && isMember && onStage}
      <button class="dancer open leave" onclick={offStage} disabled={stageBusy} title="Leave the stage">
        <span class="ring">↩</span>
        <span class="nm">Leave</span>
      </button>
    {/if}
    {#each stageDjs as dj (dj.pubkey)}
      {@const profile = useProfile(dj.pubkey)}
      <button
        class="dancer up-front"
        class:dj={dj.pubkey === currentDj}
        class:zapped={zapped === dj.pubkey}
        style={danceVars(dj.pubkey)}
        title={displayName(dj.pubkey, profile)}
        onclick={() => (selected = selected === dj.pubkey ? null : dj.pubkey)}
      >
        {#if bubbleByPubkey[dj.pubkey]}<span class="bubble">{bubbleByPubkey[dj.pubkey]}</span>{/if}
        <span class="bob v{variantOf(dj.pubkey)}">
          <img class="av" src={avatarUrl(dj.pubkey, profile)} alt="" width="64" height="64" loading="lazy" />
        </span>
        <span class="nm"><span class="mq-inner">{displayName(dj.pubkey, profile)}</span></span>
      </button>
    {/each}
    {#each Array(emptySlots) as _, i (i)}
      <button
        class="dancer open"
        class:joinable={canJoin}
        onclick={goStage}
        disabled={!canJoin || stageBusy}
        title={canJoin ? 'Take this spot' : ''}
      >
        <span class="ring">+</span>
        <span class="nm">{canJoin ? 'Join' : 'open'}</span>
      </button>
    {/each}
  </div>
  {#if stageError}<p class="dim err">⚠ {stageError}</p>{/if}

  <ComingNext clubId={groupId} />

  <!-- The dancing crowd (online members) — loose flat cluster. -->
  {#if shownOnline.length > 0}
    <div class="crowd">
      {#each shownOnline as m (m.pubkey)}
        {@const profile = useProfile(m.pubkey)}
        <button
          class="dancer"
          class:zapped={zapped === m.pubkey}
          style={danceVars(m.pubkey)}
          title={displayName(m.pubkey, profile)}
          onclick={() => (selected = selected === m.pubkey ? null : m.pubkey)}
        >
          {#if bubbleByPubkey[m.pubkey]}<span class="bubble">{bubbleByPubkey[m.pubkey]}</span>{/if}
          <span class="bob v{variantOf(m.pubkey)}">
            <img class="av" src={avatarUrl(m.pubkey, profile)} alt="" width="58" height="58" loading="lazy" />
          </span>
          <span class="nm"><span class="mq-inner">{displayName(m.pubkey, profile)}</span></span>
        </button>
      {/each}
      {#if moreOnline > 0}<span class="more">+{moreOnline}</span>{/if}

      <!-- Flying emotes rise over the floor (ephemeral floor reactions). -->
      <div class="emote-layer" aria-hidden="true">
        {#each emotes.items as e (e.id)}
          <span class="fly" style={`left:${emoteX(e.id)}%`}>{showEmote(e.emoji)}</span>
        {/each}
      </div>
    </div>
  {/if}

  <!-- Offline members: in the club, but not here right now (dimmed, still). -->
  {#if shownOffline.length > 0}
    <div class="backrow" title="club members not here right now">
      {#each shownOffline as m (m.pubkey)}
        {@const profile = useProfile(m.pubkey)}
        <button class="away" title={displayName(m.pubkey, profile)} onclick={() => (selected = selected === m.pubkey ? null : m.pubkey)}>
          <img class="av" src={avatarUrl(m.pubkey, profile)} alt="" width="26" height="26" loading="lazy" />
        </button>
      {/each}
      {#if moreOffline > 0}<span class="more sm">+{moreOffline}</span>{/if}
    </div>
  {/if}

  <!-- Tapped-avatar card: profile + moderation. -->
  {#if sel}
    {@const profile = useProfile(sel.pubkey)}
    <div class="card-pop">
      <img class="av" src={avatarUrl(sel.pubkey, profile)} alt="" width="36" height="36" />
      <div class="who">
        <span class="nm2"><span class="mq-inner">{displayName(sel.pubkey, profile)}</span></span>
        {#if roleLabel(sel)}<span class="role">{roleLabel(sel)}</span>{/if}
        {#if presence.isOnline(sel.pubkey)}<span class="here">● here</span>{/if}
      </div>
      <button class="link" onclick={() => openProfile(sel.pubkey)}>Profile ↗</button>
      {#if canModerate && stageSet.has(sel.pubkey) && sel.pubkey !== auth.pubkey}
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
  .floor {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.9rem 1rem;
  }
  .head {
    display: flex;
    align-items: baseline;
    gap: 0.45rem;
    margin-bottom: 0.7rem;
  }
  h3 {
    margin: 0;
    font-size: 1rem;
  }
  .count {
    color: var(--text-dim);
    font-size: 0.82rem;
    font-variant-numeric: tabular-nums;
  }
  .dim {
    color: var(--text-dim);
    font-size: 0.85rem;
    margin: 0.3rem 0;
  }

  /* Loose flat cluster: wrap with per-avatar scatter offsets. */
  .crowd {
    position: relative;
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem 0.7rem;
    align-items: flex-end;
    padding: 1.6rem 0.2rem 0.6rem; /* headroom for chat bubbles */
    min-height: 70px;
  }

  /* Stage row: the front of the floor. Slightly bigger dancers, a soft platform glow, and a
     dashed edge towards the crowd right below. */
  .stagerow {
    position: relative;
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem 0.9rem;
    align-items: flex-end;
    padding: 1.7rem 0.2rem 0.7rem; /* headroom for the tag + chat bubbles */
    border-bottom: 1px dashed var(--border);
    background: linear-gradient(180deg, color-mix(in srgb, var(--accent-2) 7%, transparent), transparent 70%);
    border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  }
  .stage-tag {
    position: absolute;
    top: 0.35rem;
    left: 0.4rem;
    font-size: 0.62rem;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-dim);
    pointer-events: none;
  }
  .stagerow .dancer {
    width: 72px;
  }
  .stagerow .nm {
    max-width: 72px;
    --nm-w: 72px;
    font-size: 0.66rem;
  }
  /* Open slot / leave control as a dancer-shaped column so it lines up with the row. */
  .dancer.open .ring {
    width: 64px;
    height: 64px;
    border-radius: 50%;
    border: 2px dashed var(--border);
    display: grid;
    place-items: center;
    font-size: 1.4rem;
    color: var(--text-dim);
  }
  .dancer.open {
    opacity: 0.45;
    cursor: default;
  }
  .dancer.open.joinable,
  .dancer.open.leave {
    opacity: 1;
    cursor: pointer;
  }
  .dancer.open.joinable .ring {
    border-color: var(--accent);
    color: var(--accent);
  }
  .dancer.open.joinable:hover:not(:disabled) .ring {
    background: rgba(74, 222, 94, 0.12);
  }
  .dancer.open.leave .ring {
    border-color: var(--danger);
    color: var(--danger);
    font-size: 1.1rem;
  }
  .dancer.open.leave:hover:not(:disabled) .ring {
    background: rgba(255, 90, 90, 0.12);
  }
  .dancer.open:disabled {
    cursor: default;
  }
  .err {
    color: var(--danger);
  }

  /* Chat bubble over a dancer's head (fades out; the message leaves bubbleByPubkey after 6s). */
  .bubble {
    position: absolute;
    bottom: calc(100% - 6px);
    left: 50%;
    transform: translateX(-50%);
    max-width: 150px;
    width: max-content;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text);
    border-radius: 10px;
    padding: 0.25rem 0.5rem;
    font-size: 0.72rem;
    line-height: 1.25;
    white-space: normal;
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    z-index: 4;
    pointer-events: none;
    animation: bubblein 0.18s ease-out;
  }
  @keyframes bubblein {
    from { opacity: 0; transform: translateX(-50%) translateY(4px); }
    to { opacity: 1; transform: translateX(-50%) translateY(0); }
  }

  /* Flying floor emotes. */
  .emote-layer {
    position: absolute;
    inset: 0;
    overflow: hidden;
    pointer-events: none;
  }
  .fly {
    position: absolute;
    bottom: 8px;
    font-size: 1.5rem;
    animation: floatUp 3.4s ease-out forwards;
    will-change: transform, opacity;
  }
  @keyframes floatUp {
    0% { opacity: 0; transform: translateY(0) scale(0.6); }
    15% { opacity: 1; transform: translateY(-10px) scale(1.1); }
    100% { opacity: 0; transform: translateY(-150px) scale(1); }
  }

  .emote-bar {
    display: flex;
    gap: 0.4rem;
  }
  .emo {
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    border-radius: 999px;
    width: 34px;
    height: 34px;
    font-size: 1rem;
    line-height: 1;
    cursor: pointer;
  }
  .emo:hover {
    border-color: var(--accent-2);
    transform: scale(1.08);
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
  .bob {
    display: block;
    will-change: transform;
    transform-origin: center bottom;
  }
  .av {
    border-radius: 50%;
    object-fit: cover;
    background: var(--bg-elev-2);
    display: block;
  }
  .dancer .av {
    border: 2px solid transparent;
  }
  /* On-stage DJ (front row): a frame. The currently-playing DJ (.dj) additionally glows. */
  .dancer.up-front .av {
    border-color: var(--accent-2);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent-2) 40%, transparent);
  }
  .dancer.dj .av {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 35%, transparent), 0 6px 18px color-mix(in srgb, var(--accent) 45%, transparent);
  }
  .nm {
    max-width: 66px;
    --nm-w: 66px;
    overflow: hidden;
    white-space: nowrap;
    font-size: 0.62rem;
    color: var(--text-dim);
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
  .more {
    align-self: center;
    color: var(--text-dim);
    font-size: 0.8rem;
    font-weight: 600;
  }
  .more.sm {
    font-size: 0.7rem;
  }

  /* Offline = dimmed, still, smaller, at the back. */
  .backrow {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    align-items: center;
    margin-top: 0.5rem;
    padding-top: 0.55rem;
    border-top: 1px dashed var(--border);
    opacity: 0.4;
  }
  .away {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    filter: grayscale(0.6);
  }

  /* The dance: 4 deterministic variants, only while a DJ is playing. Per-pubkey duration/delay
     (vars, iOS-safe) give varied phases → no lockstep. animation-name comes from the concrete
     variant class (NOT a CSS var) so iOS Safari resolves the keyframes. All variants bounce up
     and down (the crowd moving to the beat); sway/two-step add a little side motion. */
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
    25% { transform: translateX(0) translateY(-8px); }
    50% { transform: translateX(4px) translateY(0); }
    75% { transform: translateX(0) translateY(-8px); }
  }

  /* Energy: when the floor is busy, everyone dances a bit faster (iOS-safe calc on duration). */
  .floor.hyped.playing .bob {
    animation-duration: calc(var(--dur, 0.9s) * 0.7);
  }

  /* Zap landed on this DJ → a gold pulse + a spark rising. */
  .dancer.zapped .av {
    animation: zapPulse 0.4s ease-out 3;
    border-color: var(--amber);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--amber) 45%, transparent), 0 0 16px color-mix(in srgb, var(--amber) 55%, transparent);
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
  }

  .card-pop {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-top: 0.6rem;
    padding: 0.5rem 0.6rem;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
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
    color: var(--accent);
  }
  .here {
    font-size: 0.66rem;
    color: var(--accent-2);
  }
  .link {
    background: none;
    border: 1px solid var(--border);
    color: var(--accent);
    border-radius: 7px;
    padding: 0.25rem 0.5rem;
    font-size: 0.76rem;
    cursor: pointer;
    flex: 0 0 auto;
  }
  .mini {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    color: var(--text-dim);
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
    .fly,
    .dancer.zapped .av,
    .dancer.zapped::after {
      animation: none !important;
    }
  }
</style>

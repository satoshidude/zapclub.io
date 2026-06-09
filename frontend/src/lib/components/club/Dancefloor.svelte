<script lang="ts">
  import { presence } from '../../nostr/presence.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import { npubEncode } from 'nostr-tools/nip19'
  import { goUser } from '../../router.svelte'
  import type { ClubMember } from '../../nostr/types'
  import Chat from './Chat.svelte'

  let {
    groupId,
    members,
    canChat,
    canModerate = false,
    isOwner = false,
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
    owner?: string
    currentDj?: string
    onkick?: (pubkey: string) => void
    onpromote?: (pubkey: string) => void
    ondelete?: (eventId: string) => void
  } = $props()

  // Crowd = the club's members. Only ONLINE members (recent presence beat) dance; offline members
  // are part of the club but shown dimmed + still ("here vs away").
  const online = $derived(members.filter((m) => presence.isOnline(m.pubkey)))
  const offline = $derived(members.filter((m) => !presence.isOnline(m.pubkey)))

  const CAP = 48
  const shownOnline = $derived(online.slice(0, CAP))
  const moreOnline = $derived(Math.max(0, online.length - CAP))
  const OFF_CAP = 24
  const shownOffline = $derived(offline.slice(0, OFF_CAP))
  const moreOffline = $derived(Math.max(0, offline.length - OFF_CAP))

  // A DJ is actually playing → the floor dances; otherwise it just idles (no one's on).
  const playing = $derived(!!currentDj)

  // Shared, clock-synced pulse: all clients align the floor's beat to the wall clock, so the
  // crowd appears to move on the same beat without any (impossible, cross-origin) audio analysis.
  const beatDelay = -(Date.now() % 500)

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
    const dur = (0.7 + ((h >>> 0) % 40) / 100).toFixed(2) // 0.70–1.09s
    const delay = (-(((h >>> 5) % 110) / 100)).toFixed(2) // 0 to -1.09s (phase)
    const amp = (0.85 + ((h >>> 11) % 30) / 100).toFixed(2) // 0.85–1.14
    const dx = (((h >>> 17) % 9) - 4).toFixed(0) // -4..4 px scatter
    const dy = (((h >>> 21) % 7) - 3).toFixed(0) // -3..3 px scatter
    return `--dur:${dur}s;--delay:${delay}s;--amp:${amp};--dx:${dx}px;--dy:${dy}px`
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

  let chatOpen = $state(false)
</script>

<section class="floor card" class:playing>
  <div class="head">
    <h3>Dancefloor</h3>
    <span class="count" title="dancing now / club members">{online.length} / {members.length}</span>
  </div>

  {#if online.length === 0 && offline.length === 0}
    <p class="dim">No one here yet — be the first on the floor.</p>
  {/if}

  <!-- The dancing crowd (online members) — loose flat cluster. -->
  {#if shownOnline.length > 0}
    <div class="crowd" style={`--beat-delay:${beatDelay}ms`}>
      {#each shownOnline as m (m.pubkey)}
        {@const profile = useProfile(m.pubkey)}
        <button
          class="dancer"
          class:dj={m.pubkey === currentDj}
          style={danceVars(m.pubkey)}
          title={displayName(m.pubkey, profile)}
          onclick={() => (selected = selected === m.pubkey ? null : m.pubkey)}
        >
          <span class="bob v{variantOf(m.pubkey)}">
            <img class="av" src={avatarUrl(m.pubkey, profile)} alt="" width="44" height="44" loading="lazy" />
          </span>
          <span class="nm">{displayName(m.pubkey, profile)}</span>
        </button>
      {/each}
      {#if moreOnline > 0}<span class="more">+{moreOnline}</span>{/if}
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
        <span class="nm2">{displayName(sel.pubkey, profile)}</span>
        {#if roleLabel(sel)}<span class="role">{roleLabel(sel)}</span>{/if}
        {#if presence.isOnline(sel.pubkey)}<span class="here">● here</span>{/if}
      </div>
      <button class="link" onclick={() => openProfile(sel.pubkey)}>Profile ↗</button>
      {#if canModerate && sel.pubkey !== owner && sel.pubkey !== auth.pubkey}
        {#if isOwner && !sel.roles.includes('moderator')}
          <button class="mini" onclick={() => { onpromote?.(sel.pubkey); selected = null }}>+mod</button>
        {/if}
        <button class="mini danger" onclick={() => { onkick?.(sel.pubkey); selected = null }}>kick</button>
      {/if}
      <button class="x" aria-label="Close" onclick={() => (selected = null)}>✕</button>
    </div>
  {/if}

  <!-- Chat, kept subtle: collapsed by default. -->
  <details class="chat-acc" bind:open={chatOpen}>
    <summary><span>💬 Chat</span><span class="chev" aria-hidden="true">▾</span></summary>
    <Chat {groupId} {canChat} {canModerate} onauthor={(pk) => goUser(npubEncode(pk))} {ondelete} />
  </details>
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
    justify-content: space-between;
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
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem 0.7rem;
    align-items: flex-end;
    padding: 0.4rem 0.2rem 0.6rem;
    min-height: 70px;
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
    gap: 2px;
    width: 52px;
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
  .dancer.dj .av {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 35%, transparent), 0 6px 18px color-mix(in srgb, var(--accent) 45%, transparent);
  }
  .nm {
    max-width: 52px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.62rem;
    color: var(--text-dim);
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

  /* The dance: 4 deterministic variants, only while a DJ is playing. */
  .floor.playing .bob {
    animation: var(--anim, dance0) var(--dur, 0.9s) var(--delay, 0s) infinite ease-in-out;
  }
  .floor.playing .v0 { --anim: dance0; }
  .floor.playing .v1 { --anim: dance1; }
  .floor.playing .v2 { --anim: dance2; }
  .floor.playing .v3 { --anim: dance3; }

  @keyframes dance0 { /* bounce */
    0%, 100% { transform: translateY(0) scaleY(1); }
    50% { transform: translateY(calc(-8px * var(--amp, 1))) scaleY(1.04); }
  }
  @keyframes dance1 { /* sway */
    0%, 100% { transform: rotate(calc(-6deg * var(--amp, 1))) translateX(-1px); }
    50% { transform: rotate(calc(6deg * var(--amp, 1))) translateX(1px); }
  }
  @keyframes dance2 { /* headbob */
    0%, 100% { transform: translateY(0) rotate(-2deg); }
    50% { transform: translateY(calc(-4px * var(--amp, 1))) rotate(2deg); }
  }
  @keyframes dance3 { /* two-step */
    0%, 100% { transform: translateX(calc(-4px * var(--amp, 1))); }
    25% { transform: translateX(0) translateY(-3px); }
    50% { transform: translateX(calc(4px * var(--amp, 1))); }
    75% { transform: translateX(0) translateY(-3px); }
  }

  /* Shared, clock-synced beat: a gentle whole-floor pulse so the crowd reads as "on the beat". */
  .floor.playing .crowd {
    animation: floorpulse 0.5s var(--beat-delay, 0ms) infinite ease-in-out;
  }
  @keyframes floorpulse {
    0%, 100% { transform: scale(1); }
    50% { transform: scale(1.012); }
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
    text-overflow: ellipsis;
    white-space: nowrap;
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

  .chat-acc {
    margin-top: 0.7rem;
    border-top: 1px solid var(--border);
    padding-top: 0.5rem;
  }
  .chat-acc summary {
    display: flex;
    justify-content: space-between;
    align-items: center;
    cursor: pointer;
    color: var(--text-dim);
    font-size: 0.85rem;
    list-style: none;
  }
  .chat-acc summary::-webkit-details-marker { display: none; }
  .chat-acc[open] .chev { transform: rotate(180deg); }
  .chev { transition: transform 0.15s; }

  @media (prefers-reduced-motion: reduce) {
    .floor.playing .bob,
    .floor.playing .crowd {
      animation: none !important;
    }
  }
</style>

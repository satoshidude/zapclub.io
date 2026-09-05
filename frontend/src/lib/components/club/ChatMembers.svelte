<script lang="ts">
  import { npubEncode } from 'nostr-tools/nip19'
  import { auth } from '../../nostr/auth.svelte'
  import { presence } from '../../nostr/presence.svelte'
  import { avatarUrl, displayName, useProfile } from '../../nostr/profiles.svelte'
  import { stage } from '../../nostr/stage.svelte'
  import { goUser } from '../../router.svelte'
  import type { ClubMember } from '../../nostr/types'
  import { COLLAPSED_MEMBER_ROWS, EXPANDED_MEMBER_ROWS } from './memberRoster'

  let {
    members,
    canModerate = false,
    isOwner = false,
    owner = '',
    expanded = false,
    onkick,
    onpromote,
    onexpandedchange,
  }: {
    members: ClubMember[]
    canModerate?: boolean
    isOwner?: boolean
    owner?: string
    expanded?: boolean
    onkick?: (pubkey: string) => void
    onpromote?: (pubkey: string) => void
    onexpandedchange?: (expanded: boolean) => void
  } = $props()

  let selected = $state<string | null>(null)

  function isHere(pubkey: string): boolean {
    return stage.isOnStage(pubkey) || presence.isOnline(pubkey)
  }

  function roleLabel(member: ClubMember): string {
    if (member.pubkey === owner) return 'host'
    if (member.roles.includes('moderator')) return 'mod'
    return ''
  }

  function roleRank(member: ClubMember): number {
    if (member.pubkey === owner) return 0
    if (member.roles.includes('moderator')) return 1
    return 2
  }

  const sortedMembers = $derived.by(() =>
    [...members].sort((a, b) => {
      if (a.pubkey === auth.pubkey) return -1
      if (b.pubkey === auth.pubkey) return 1
      const presenceOrder = Number(isHere(b.pubkey)) - Number(isHere(a.pubkey))
      if (presenceOrder) return presenceOrder
      const roleOrder = roleRank(a) - roleRank(b)
      if (roleOrder) return roleOrder
      return a.pubkey.localeCompare(b.pubkey)
    }),
  )
  const hereCount = $derived(members.filter((member) => isHere(member.pubkey)).length)
  const canExpand = $derived(members.length > COLLAPSED_MEMBER_ROWS)
  const concealedCount = $derived(Math.max(0, members.length - COLLAPSED_MEMBER_ROWS))
  const displayedMembers = $derived(
    expanded ? sortedMembers : sortedMembers.slice(0, COLLAPSED_MEMBER_ROWS),
  )

  function openProfile(pubkey: string) {
    selected = null
    goUser(npubEncode(pubkey))
  }

  function toggleExpanded() {
    selected = null
    onexpandedchange?.(!expanded)
  }

  function trackOverflow(node: HTMLElement) {
    const inner = node.firstElementChild
    const update = () => {
      const overflow = Math.max(0, node.scrollWidth - node.clientWidth)
      node.style.setProperty('--name-overflow', `${overflow}px`)
      node.classList.toggle('overflows', overflow > 1)
    }
    const observer = new ResizeObserver(update)
    observer.observe(node)
    if (inner) observer.observe(inner)
    requestAnimationFrame(update)
    return { destroy: () => observer.disconnect() }
  }
</script>

<aside
  class="members"
  class:collapsible={canExpand}
  class:expanded
  style={`--member-row-limit: ${expanded ? EXPANDED_MEMBER_ROWS : COLLAPSED_MEMBER_ROWS}`}
  aria-label="Club members"
>
  <div class="summary" aria-label={`${hereCount} here, ${members.length} club members`}>
    <span class="status-dot"></span>
    <span>{hereCount} here</span>
    <span class="separator">/</span>
    <span>{members.length} total</span>
  </div>

  <div class="member-list" role="list">
    {#each displayedMembers as member (member.pubkey)}
      {@const profile = useProfile(member.pubkey)}
      {@const here = isHere(member.pubkey)}
      <div class="member-entry" class:expanded={selected === member.pubkey} role="listitem">
        <button
          class="member"
          class:here
          aria-expanded={selected === member.pubkey}
          title={displayName(member.pubkey, profile)}
          onclick={() => (selected = selected === member.pubkey ? null : member.pubkey)}
        >
          <span class="avatar-wrap">
            <img src={avatarUrl(member.pubkey, profile)} alt="" width="30" height="30" loading="lazy" />
            <span class="presence-dot" aria-label={here ? 'Here' : 'Away'}></span>
          </span>
          <span class="identity">
            <span class="name" use:trackOverflow>
              <span class="name-inner">{member.pubkey === auth.pubkey ? 'You' : displayName(member.pubkey, profile)}</span>
            </span>
            <span class="member-state">{stage.isOnStage(member.pubkey) ? 'on stage' : here ? 'here' : 'away'}</span>
          </span>
          {#if roleLabel(member)}<span class="role">{roleLabel(member)}</span>{/if}
        </button>

        {#if selected === member.pubkey}
          <div class="member-actions">
            <button onclick={() => openProfile(member.pubkey)}>Profile</button>
            {#if canModerate && member.pubkey !== owner && member.pubkey !== auth.pubkey}
              {#if isOwner && !member.roles.includes('moderator')}
                <button onclick={() => { onpromote?.(member.pubkey); selected = null }}>Make mod</button>
              {/if}
              <button class="danger" onclick={() => { onkick?.(member.pubkey); selected = null }}>Remove</button>
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  </div>

  {#if canExpand}
    <button class="member-toggle" type="button" aria-expanded={expanded} onclick={toggleExpanded}>
      {#if expanded}
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m3 10 5-5 5 5"></path></svg>
        <span>Show fewer</span>
      {:else}
        <span class="toggle-count">+{concealedCount}</span>
        <span>Show members</span>
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="m3 6 5 5 5-5"></path></svg>
      {/if}
    </button>
  {/if}
</aside>

<style>
  .members {
    display: flex;
    min-width: 0;
    height: 100%;
    flex-direction: column;
    color: var(--lcd-text);
    font-family: 'DotGothic16', ui-monospace, monospace;
  }
  .summary {
    display: flex;
    align-items: center;
    gap: 0.38rem;
    min-height: 32px;
    padding: 0 0.7rem 0.55rem;
    color: var(--lcd-text-dim);
    font-size: 0.68rem;
    font-variant-numeric: tabular-nums;
  }
  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--lcd-text-bright);
    box-shadow: 0 0 5px rgba(207, 233, 255, 0.52);
  }
  .separator { opacity: 0.52; }
  .member-list {
    flex: 0 1 auto;
    min-height: 0;
    overflow: hidden;
    overscroll-behavior: contain;
    scrollbar-color: rgba(241, 243, 244, 0.25) transparent;
  }
  .members.collapsible .member-list {
    max-height: calc(var(--member-row-limit) * 51px);
  }
  .members.collapsible.expanded .member-list {
    overflow-y: auto;
  }
  .member-entry {
    min-width: 0;
    border-top: 1px solid rgba(201, 206, 209, 0.08);
  }
  .member {
    display: grid;
    width: 100%;
    min-width: 0;
    grid-template-columns: 34px minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.45rem;
    padding: 0.42rem 0.7rem;
    border: 0;
    color: inherit;
    background: transparent;
    font: inherit;
    text-align: left;
    cursor: pointer;
    min-height: 51px;
  }
  .member:hover,
  .member:focus-visible,
  .member-entry.expanded .member {
    background: rgba(207, 233, 255, 0.07);
  }
  .member:focus-visible {
    outline: 1px solid rgba(207, 233, 255, 0.74);
    outline-offset: -2px;
  }
  .avatar-wrap {
    position: relative;
    display: block;
    width: 30px;
    height: 30px;
  }
  .avatar-wrap img {
    display: block;
    width: 30px;
    height: 30px;
    border-radius: 50%;
    object-fit: cover;
    opacity: 0.64;
    filter: grayscale(0.5) saturate(0.68) contrast(1.08);
  }
  .member.here .avatar-wrap img {
    opacity: 0.96;
    filter: grayscale(0.18) saturate(0.88) contrast(1.08);
  }
  .presence-dot {
    position: absolute;
    right: -1px;
    bottom: -1px;
    width: 8px;
    height: 8px;
    border: 2px solid #09204a;
    border-radius: 50%;
    background: rgba(201, 206, 209, 0.4);
  }
  .member.here .presence-dot {
    background: var(--lcd-text-bright);
    box-shadow: 0 0 4px rgba(207, 233, 255, 0.55);
  }
  .identity {
    display: flex;
    min-width: 0;
    flex-direction: column;
  }
  .name {
    overflow: hidden;
    color: var(--lcd-text-soft);
    font-size: calc(0.73rem + 2px);
    text-overflow: ellipsis;
    text-shadow: var(--lcd-text-shadow);
    white-space: nowrap;
  }
  .name-inner {
    display: inline-block;
    min-width: max-content;
    transform: translateX(0);
  }
  .member:hover .name:global(.overflows) .name-inner,
  .member:focus-visible .name:global(.overflows) .name-inner {
    animation: member-name-scroll 4s ease-in-out infinite;
  }
  @keyframes member-name-scroll {
    0%, 18%, 100% { transform: translateX(0); }
    62%, 82% { transform: translateX(calc(-1 * var(--name-overflow))); }
  }
  .member.here .name { color: var(--lcd-text); }
  .member-state {
    color: var(--lcd-text-dim);
    font-size: 0.59rem;
  }
  .role {
    color: var(--lcd-text-dim);
    font-size: 0.58rem;
  }
  .member-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    padding: 0 0.7rem 0.5rem 2.85rem;
  }
  .member-actions button {
    padding: 0.18rem 0;
    border: 0;
    color: var(--lcd-text-dim);
    background: transparent;
    font: 0.62rem 'DotGothic16', ui-monospace, monospace;
    cursor: pointer;
  }
  .member-actions button + button::before {
    content: '/';
    padding-right: 0.35rem;
    color: rgba(201, 206, 209, 0.35);
  }
  .member-actions button:hover,
  .member-actions button:focus-visible { color: var(--lcd-text-bright); }
  .member-actions button:focus-visible {
    outline: 1px solid var(--lcd-text-bright);
    outline-offset: 2px;
  }
  .member-actions .danger:hover,
  .member-actions .danger:focus-visible { color: #ffd0d0; }
  .member-toggle {
    display: flex;
    width: 100%;
    min-height: 29px;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    padding: 0.25rem 0.7rem 0;
    border: 0;
    border-top: 1px solid rgba(201, 206, 209, 0.12);
    color: var(--lcd-text-dim);
    background: transparent;
    font: 0.62rem 'DotGothic16', ui-monospace, monospace;
    text-shadow: var(--lcd-text-shadow);
    cursor: pointer;
  }
  .member-toggle:hover,
  .member-toggle:focus-visible { color: var(--lcd-text-bright); }
  .member-toggle:focus-visible {
    outline: 1px solid rgba(207, 233, 255, 0.74);
    outline-offset: -2px;
  }
  .member-toggle svg {
    width: 13px;
    height: 13px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
    filter: drop-shadow(0 0 3px rgba(207, 233, 255, 0.52));
  }
  .toggle-count {
    color: var(--lcd-text-bright);
    font-variant-numeric: tabular-nums;
  }

  @media (prefers-reduced-motion: reduce), (hover: none) {
    .member:hover .name:global(.overflows) .name-inner,
    .member:focus-visible .name:global(.overflows) .name-inner { animation: none; }
  }

  @media (max-width: 700px) {
    .members { display: block; }
    .summary {
      min-height: 24px;
      padding: 0 0 0.4rem;
    }
    .member-list {
      display: flex;
      gap: 0.35rem;
      padding: 0 0 0.55rem;
      overflow-x: auto;
      overflow-y: hidden;
      scroll-snap-type: x proximity;
    }
    .members.collapsible .member-list,
    .members.collapsible.expanded .member-list {
      max-height: none;
      overflow-x: auto;
      overflow-y: hidden;
    }
    .member-entry {
      position: relative;
      flex: 0 0 auto;
      border: 0;
      scroll-snap-align: start;
    }
    .member {
      width: 116px;
      grid-template-columns: 30px minmax(0, 1fr);
      gap: 0.4rem;
      padding: 0.34rem 0.42rem;
    }
    .role { display: none; }
    .member-actions { padding: 0.15rem 0.42rem 0.45rem; }
    .member-toggle { display: none; }
  }
</style>

<script lang="ts">
  import { stage, joinStage, leaveStage, MAX_DJS } from '../../nostr/stage.svelte'
  import { sync } from '../../nostr/sync.svelte'
  import { kickFromStage } from '../../nostr/groups'
  import { auth } from '../../nostr/auth.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { goUser } from '../../router.svelte'
  import { npubEncode } from 'nostr-tools/nip19'
  import { presence } from '../../nostr/presence.svelte'

  interface Props {
    groupId: string
    /** Can the local user moderate (owner/mod)? Enables kick buttons. */
    canModerate?: boolean
    /** Is the local user a club member? Only members may take the stage. */
    isMember?: boolean
  }
  let { groupId, canModerate = false, isMember = false }: Props = $props()

  let busy = $state(false)
  let error = $state('')

  const me = $derived(auth.pubkey)
  const djs = $derived(stage.djs)
  const onStage = $derived(stage.isOnStage(me))
  const conductor = $derived(stage.conductor)
  // The DJ whose track is actually playing right now (pulsing highlight).
  const liveDj = $derived(sync.live?.dj ?? '')
  const emptySlots = $derived(Math.max(0, MAX_DJS - djs.length))
  // A free slot can be taken directly by a signed-in member who isn't on stage yet.
  const canJoin = $derived(auth.canSign && isMember && !onStage && !stage.full)

  async function go() {
    busy = true
    error = ''
    try {
      await joinStage(groupId)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }

  async function leave() {
    busy = true
    error = ''
    try {
      await leaveStage(groupId)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }

  async function kick(pubkey: string) {
    error = ''
    try {
      await kickFromStage(groupId, pubkey)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    }
  }
</script>

<div class="stage card">
  <div class="head">
    <h3>Stage <span class="count">{djs.length}/{MAX_DJS}</span></h3>
    {#if auth.canSign && isMember}
      {#if onStage}
        <button class="btn btn-ghost btn-sm" onclick={leave} disabled={busy}>Leave stage</button>
      {:else if !stage.full}
        <button class="btn btn-primary btn-sm" onclick={go} disabled={busy}>Go on stage</button>
      {:else}
        <span class="full">Stage full</span>
      {/if}
    {/if}
  </div>

  {#if error}<p class="err">⚠ {error}</p>{/if}

  <div class="slots">
    {#each djs as dj (dj.pubkey)}
      {@const profile = useProfile(dj.pubkey)}
      <div class="slot filled" class:live={dj.pubkey === liveDj}>
        <img class="avatar" class:online={presence.isOnline(dj.pubkey)} src={avatarUrl(dj.pubkey, profile)} alt="" width="44" height="44" title={presence.isOnline(dj.pubkey) ? 'online now' : ''} />
        <a class="name" href={`/user/${npubEncode(dj.pubkey)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(dj.pubkey)) }}>{displayName(dj.pubkey, profile)}</a>
        {#if dj.pubkey === conductor}<span class="badge">conductor</span>{/if}
        {#if canModerate && dj.pubkey !== me}
          <button class="kick" onclick={() => kick(dj.pubkey)} title="Remove from stage">✕</button>
        {/if}
      </div>
    {/each}
    {#each Array(emptySlots) as _, i (i)}
      <button
        class="slot empty"
        class:joinable={canJoin}
        onclick={go}
        disabled={!canJoin || busy}
        title={canJoin ? 'Take this spot' : ''}
      >
        <span class="plus">+</span>
        <span class="name">{canJoin ? 'Join' : 'Open'}</span>
      </button>
    {/each}
  </div>
</div>

<style>
  .stage {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.9rem 1rem;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.8rem;
  }
  h3 {
    margin: 0;
    font-size: 1rem;
  }
  .count {
    color: var(--text-dim);
    font-weight: 600;
    font-size: 0.85rem;
  }
  .full {
    font-size: 0.8rem;
    color: var(--text-dim);
  }
  .slots {
    display: flex;
    flex-wrap: wrap;
    gap: 0.7rem;
  }
  .slot {
    position: relative;
    width: 84px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.35rem;
    padding: 0.6rem 0.3rem;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: var(--bg);
  }
  /* The active DJ (whose track is playing) gets a pulsing green frame. */
  .slot.live {
    border-color: var(--accent);
    animation: stage-pulse 1.6s ease-in-out infinite;
  }
  @keyframes stage-pulse {
    0%,
    100% {
      box-shadow: 0 0 0 1px var(--accent), 0 0 6px rgba(74, 222, 94, 0.3);
    }
    50% {
      box-shadow: 0 0 0 2px var(--accent), 0 0 18px rgba(74, 222, 94, 0.7);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .slot.live {
      animation: none;
      box-shadow: 0 0 0 1px var(--accent);
    }
  }
  .slot.empty {
    border-style: dashed;
    opacity: 0.6;
    justify-content: center;
    min-height: 96px;
    color: inherit;
    font: inherit;
  }
  .slot.empty.joinable {
    opacity: 1;
    cursor: pointer;
    transition: border-color 0.15s ease, opacity 0.15s ease;
  }
  .slot.empty.joinable:hover {
    border-color: var(--accent);
  }
  .slot.empty.joinable:hover .plus,
  .slot.empty.joinable:hover .name {
    color: var(--accent);
  }
  .slot.empty:disabled {
    cursor: default;
  }
  .avatar {
    width: 44px;
    height: 44px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
  }
  /* Nostr-purple ring = this user is online right now (live presence heartbeat). */
  .avatar.online {
    border-color: var(--accent-2);
    box-shadow: 0 0 0 2px var(--accent-2), 0 0 8px rgba(177, 77, 255, 0.55);
  }
  .name {
    font-size: 0.74rem;
    max-width: 78px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-align: center;
    color: inherit;
    text-decoration: none;
    cursor: pointer;
  }
  .name:hover {
    text-decoration: underline;
  }
  .badge {
    font-size: 0.62rem;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .plus {
    font-size: 1.6rem;
    color: var(--text-dim);
  }
  .kick {
    position: absolute;
    top: -6px;
    right: -6px;
    width: 20px;
    height: 20px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-elev-2);
    color: var(--text-dim);
    font-size: 0.7rem;
    line-height: 1;
    cursor: pointer;
  }
  .kick:hover {
    border-color: var(--danger);
    color: var(--danger);
  }
  .err {
    color: var(--danger);
    font-size: 0.8rem;
    margin: 0 0 0.5rem;
  }
</style>

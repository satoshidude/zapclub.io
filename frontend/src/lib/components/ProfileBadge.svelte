<script lang="ts">
  import { avatarUrl, displayName } from '../nostr/profiles.svelte'
  import type { ProfileMetadata } from '../nostr/types'

  let {
    pubkey,
    npub,
    profile,
    size = 34,
  }: {
    pubkey: string
    npub: string
    profile: ProfileMetadata | null
    size?: number
  } = $props()

  const name = $derived(displayName(pubkey, profile))
  const avatar = $derived(avatarUrl(pubkey, profile))

  let nameEl = $state<HTMLSpanElement | null>(null)
  let scrolling = $state(false)

  $effect(() => {
    if (!nameEl) return
    const check = () => { scrolling = nameEl!.scrollWidth > nameEl!.offsetWidth }
    check()
    const ro = new ResizeObserver(check)
    ro.observe(nameEl)
    return () => ro.disconnect()
  })
</script>

<span class="badge">
  <img class="avatar" src={avatar} alt="" width={size} height={size} style:width="{size}px" style:height="{size}px" />
  <span class="name-clip">
    <span class="name" class:scrolling bind:this={nameEl}>{name}</span>
  </span>
</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
  }
  .avatar {
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
  }
  .name-clip {
    max-width: 16ch;
    overflow: hidden;
    display: inline-block;
  }
  .name {
    display: inline-block;
    font-size: 1rem;
    font-weight: 800;
    letter-spacing: -0.02em;
    color: #fff;
    white-space: nowrap;
  }
  .name.scrolling {
    animation: name-scroll 5s ease-in-out infinite;
  }
  @keyframes name-scroll {
    0%, 15%  { transform: translateX(0); }
    70%, 85% { transform: translateX(calc(-100% + 16ch)); }
    95%, 100% { transform: translateX(0); }
  }
</style>

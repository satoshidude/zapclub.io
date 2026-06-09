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
</script>

<span class="badge">
  <img class="avatar" src={avatar} alt="" width={size} height={size} style:width="{size}px" style:height="{size}px" />
  <span class="name">{name}</span>
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
  .name {
    /* Same look as the logo wordmark (App.svelte .brand .word). */
    font-size: 1rem;
    font-weight: 800;
    letter-spacing: -0.02em;
    color: #fff;
    max-width: 12ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>

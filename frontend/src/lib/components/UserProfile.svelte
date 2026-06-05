<script lang="ts">
  import { decode } from 'nostr-tools/nip19'
  import { auth } from '../nostr/auth.svelte'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import { playlists, fetchPlaylists, deletePlaylist, loadMyPlaylists } from '../nostr/playlists.svelte'
  import type { Playlist } from '../nostr/types'

  let { npub }: { npub: string } = $props()

  const pubkey = $derived.by(() => {
    try {
      const { type, data } = decode(npub)
      return type === 'npub' ? (data as string) : ''
    } catch {
      return ''
    }
  })
  const isMe = $derived(!!auth.pubkey && auth.pubkey === pubkey)
  const profile = $derived(pubkey ? useProfile(pubkey) : null)

  let othersPlaylists = $state<Playlist[]>([])
  let loading = $state(true)

  $effect(() => {
    const pk = pubkey
    if (!pk) {
      loading = false
      return
    }
    loading = true
    if (isMe) {
      void loadMyPlaylists().finally(() => (loading = false))
    } else {
      fetchPlaylists(pk)
        .then((p) => (othersPlaylists = p))
        .finally(() => (loading = false))
    }
  })

  const list = $derived(isMe ? playlists.mine : othersPlaylists)

  function fmt(s: number): string {
    if (!s) return ''
    const m = Math.floor(s / 60)
    const sec = Math.floor(s % 60)
    return `${m}:${sec.toString().padStart(2, '0')}`
  }
</script>

<div class="wrap">
  <header class="phead">
    <img class="pavatar" src={avatarUrl(pubkey, profile)} alt="" width="64" height="64" />
    <div class="pinfo">
      <h1>{displayName(pubkey, profile)}</h1>
      <span class="npub">{npub.slice(0, 18)}…</span>
      {#if profile?.about}<p class="pabout">{profile.about}</p>{/if}
    </div>
  </header>

  <section class="pls">
    <h2>Playlists {#if list.length}<span class="count">{list.length}</span>{/if}</h2>

    {#if loading}
      <p class="dim">Loading playlists…</p>
    {:else if list.length === 0}
      <p class="dim">
        {isMe ? 'No saved playlists yet — save a set from a club’s DJ Station.' : 'No public playlists.'}
      </p>
    {:else}
      <ul class="pl-list">
        {#each list as pl (pl.id)}
          <li class="card">
            <details>
              <summary>
                <span class="pl-name">{pl.name}</span>
                <span class="dim">{pl.tracks.length} track{pl.tracks.length === 1 ? '' : 's'}</span>
                {#if isMe}
                  <button
                    class="del"
                    title="Delete playlist"
                    onclick={(e) => { e.preventDefault(); void deletePlaylist(pl.id) }}
                  >✕</button>
                {/if}
              </summary>
              {#if pl.tracks.length > 0}
                <ol class="tracks">
                  {#each pl.tracks as t (t.videoId)}
                    <li>
                      <span class="t-title">{t.title}</span>
                      <span class="dim dur">{fmt(t.duration)}</span>
                    </li>
                  {/each}
                </ol>
              {/if}
            </details>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
</div>

<style>
  .wrap {
    max-width: 680px;
    margin: 0 auto;
    padding: 1.4rem 1rem 4rem;
  }
  .phead {
    display: flex;
    gap: 1rem;
    align-items: center;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.2rem;
  }
  .pavatar {
    width: 64px;
    height: 64px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    flex: 0 0 auto;
  }
  .pinfo {
    min-width: 0;
  }
  h1 {
    margin: 0;
    font-size: 1.4rem;
  }
  .npub {
    font-size: 0.78rem;
    color: var(--text-dim);
    font-family: ui-monospace, monospace;
  }
  .pabout {
    margin: 0.4rem 0 0;
    color: var(--text-dim);
    font-size: 0.9rem;
  }
  .pls {
    margin-top: 1.4rem;
  }
  h2 {
    font-size: 1.1rem;
  }
  .count {
    font-size: 0.8rem;
    color: var(--text-dim);
    font-weight: 600;
  }
  .dim {
    color: var(--text-dim);
  }
  .pl-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }
  .card {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.6rem 0.9rem;
  }
  summary {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    cursor: pointer;
    list-style: none;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  .pl-name {
    font-weight: 600;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .del {
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 6px;
    width: 22px;
    height: 22px;
    font-size: 0.7rem;
    cursor: pointer;
    flex: 0 0 auto;
  }
  .del:hover {
    border-color: var(--danger);
    color: var(--danger);
  }
  .tracks {
    list-style: none;
    margin: 0.7rem 0 0.2rem;
    padding: 0.7rem 0 0;
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    counter-reset: t;
  }
  .tracks li {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    font-size: 0.85rem;
  }
  .tracks li::before {
    counter-increment: t;
    content: counter(t);
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
    flex: 0 0 1.3rem;
  }
  .t-title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dur {
    flex: 0 0 auto;
    font-size: 0.78rem;
    font-variant-numeric: tabular-nums;
  }
</style>

<script lang="ts">
  import { untrack } from 'svelte'
  import {
    playlists,
    addTrackToPlaylist,
    createPlaylist,
    loadMyPlaylists,
    setPlaylistTrackTitle,
  } from '../nostr/playlists.svelte'
  import { setTrackTitle } from '../nostr/queue.svelte'
  import { auth } from '../nostr/auth.svelte'
  import type { QueueTrack } from '../nostr/types'

  let {
    track,
    context,
    groupId,
    playlistId,
    canEdit = false,
    onAdd,
    onClose,
  }: {
    track: QueueTrack
    context: 'queue' | 'library' | 'search'
    groupId?: string
    playlistId?: string
    canEdit?: boolean
    onAdd?: (t: QueueTrack) => void
    onClose: () => void
  } = $props()

  const signedIn = $derived(!!auth.pubkey)
  const editable = $derived(canEdit && (context === 'queue' || context === 'library'))

  // Editable copy of the title. The modal is mounted fresh per track ({#if preview}), so
  // capturing the initial value (untracked) is correct — no need to react to `track` changing.
  let title = $state(untrack(() => track.title))
  let savingTitle = $state(false)
  let titleSaved = $state(false)

  // Load the playlist library for the "save to playlist" picker.
  $effect(() => {
    if (signedIn && !playlists.loaded) void loadMyPlaylists()
  })

  async function saveTitle() {
    const t = title.trim()
    if (!t || t === track.title) return
    savingTitle = true
    try {
      if (context === 'queue' && groupId) await setTrackTitle(groupId, track.videoId, t)
      else if (context === 'library' && playlistId) await setPlaylistTrackTitle(playlistId, track.videoId, t)
      titleSaved = true
      setTimeout(() => (titleSaved = false), 1500)
    } finally {
      savingTitle = false
    }
  }

  let addedTo = $state<string | null>(null)
  async function saveTo(plId: string, name: string) {
    await addTrackToPlaylist(plId, {
      videoId: track.videoId,
      title: title.trim() || track.title,
      duration: track.duration,
    })
    addedTo = name
    setTimeout(() => (addedTo = null), 1600)
  }

  let newName = $state('')
  let creating = $state(false)
  async function createAndAdd() {
    const n = newName.trim()
    if (!n) return
    creating = true
    try {
      const pl = await createPlaylist(n)
      await saveTo(pl.id, pl.name)
      newName = ''
    } finally {
      creating = false
    }
  }

  const fmt = (s: number) => (s ? `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}` : '')

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose()
  }
</script>

<svelte:window onkeydown={onKey} />

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="backdrop" role="presentation" onclick={onClose}>
  <div class="sheet" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => e.stopPropagation()}>
    <button class="x" onclick={onClose} aria-label="Close">✕</button>

    <div class="video">
      <iframe
        title="Track preview"
        src={`https://www.youtube-nocookie.com/embed/${track.videoId}?autoplay=1&rel=0`}
        allow="autoplay; encrypted-media; fullscreen; picture-in-picture"
        allowfullscreen
      ></iframe>
    </div>

    {#if editable}
      <label class="fld">
        <span class="lbl">Artist – Title</span>
        <div class="row">
          <input bind:value={title} maxlength="200" placeholder="Artist – Title" autocomplete="off" />
          <button
            class="btn btn-primary btn-sm"
            onclick={saveTitle}
            disabled={savingTitle || !title.trim() || title.trim() === track.title}
          >
            {titleSaved ? '✓' : savingTitle ? '…' : 'Save'}
          </button>
        </div>
      </label>
    {:else}
      <p class="ttl">{track.title}</p>
    {/if}
    {#if fmt(track.duration)}<p class="meta">{fmt(track.duration)}</p>{/if}

    {#if context === 'search' && onAdd}
      <button class="btn btn-primary big" onclick={() => { onAdd?.(track); onClose() }}>+ Add to my set</button>
    {/if}

    {#if signedIn}
      <div class="save">
        <span class="lbl">Save to a playlist</span>
        {#if playlists.mine.length > 0}
          <div class="pls">
            {#each playlists.mine as pl (pl.id)}
              <button class="pl" onclick={() => saveTo(pl.id, pl.name)}>{addedTo === pl.name ? '✓ ' : '+ '}{pl.name}</button>
            {/each}
          </div>
        {/if}
        <div class="row">
          <input bind:value={newName} maxlength="60" placeholder="New playlist…" autocomplete="off" />
          <button class="btn btn-ghost btn-sm" onclick={createAndAdd} disabled={creating || !newName.trim()}>Create</button>
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 220;
    background: rgba(0, 0, 0, 0.65);
    backdrop-filter: blur(3px);
    display: grid;
    place-items: center;
    padding: 1rem;
  }
  .sheet {
    position: relative;
    width: 100%;
    max-width: 460px;
    max-height: 90vh;
    overflow-y: auto;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.1rem;
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.55);
  }
  .x {
    position: absolute;
    top: 0.5rem;
    right: 0.6rem;
    background: none;
    border: none;
    color: var(--text-dim);
    font-size: 1rem;
    cursor: pointer;
    line-height: 1;
  }
  .x:hover {
    color: var(--text);
  }
  .video {
    position: relative;
    width: 100%;
    aspect-ratio: 16 / 9;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: #000;
  }
  .video iframe {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    border: 0;
  }
  .ttl {
    margin: 0;
    font-weight: 600;
    font-size: 0.95rem;
  }
  .meta {
    margin: 0;
    color: var(--text-dim);
    font-size: 0.78rem;
    font-variant-numeric: tabular-nums;
  }
  .fld,
  .save {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }
  .lbl {
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-dim);
  }
  .row {
    display: flex;
    gap: 0.5rem;
  }
  .row input {
    flex: 1;
    min-width: 0;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.7rem;
    color: var(--text);
    font-size: 0.88rem;
  }
  .row input:focus {
    outline: none;
    border-color: var(--accent-2);
  }
  .save {
    border-top: 1px solid var(--border);
    padding-top: 0.7rem;
  }
  .pls {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem;
  }
  .pl {
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--accent);
    border-radius: 7px;
    padding: 0.3rem 0.55rem;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .pl:hover {
    border-color: var(--accent);
  }
  .big {
    width: 100%;
  }
</style>

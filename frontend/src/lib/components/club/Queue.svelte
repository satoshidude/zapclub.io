<script lang="ts">
  import { queues, addTrack, addTracks, removeTrack, moveTrack, setMyQueue, clearQueue, shuffleQueue } from '../../nostr/queue.svelte'
  import { skipTrack, canSkip } from '../../nostr/sync.svelte'
  import { playlists, savePlaylistAs, deletePlaylist, loadMyPlaylists } from '../../nostr/playlists.svelte'
  import { searchYouTube, fetchYouTubePlaylist, parseYouTubePlaylistId, type SearchHit } from '../../player/youtube'
  import { auth } from '../../nostr/auth.svelte'
  import { stage } from '../../nostr/stage.svelte'
  import type { QueueTrack, Playlist } from '../../nostr/types'

  let { groupId }: { groupId: string } = $props()

  const me = $derived(auth.pubkey)
  const onStage = $derived(stage.isOnStage(me))
  const myQueue = $derived(me ? queues.get(me) : null)
  const tracks = $derived(myQueue?.tracks ?? [])

  // Saved playlists (library): load once when signed in.
  $effect(() => {
    if (me && !playlists.loaded) void loadMyPlaylists()
  })
  let saving = $state(false)
  let saveName = $state('')
  let showLib = $state(false)

  async function doSave() {
    if (!saveName.trim() || tracks.length === 0) return
    await savePlaylistAs(saveName, tracks.map((t) => ({ videoId: t.videoId, title: t.title, duration: t.duration })))
    saveName = ''
    saving = false
  }
  async function loadPl(pl: Playlist) {
    await setMyQueue(groupId, pl.tracks.map((t) => ({ videoId: t.videoId, title: t.title, duration: t.duration })))
    showLib = false
  }

  let query = $state('')
  let results = $state<SearchHit[]>([])
  let searching = $state(false)
  let searchError = $state('')
  let isPlaylist = $state(false) // results came from a pasted playlist link

  // Clear the results (and error) when the search field is emptied — no wasted space.
  $effect(() => {
    if (!query.trim()) {
      results = []
      searchError = ''
      isPlaylist = false
    }
  })

  async function doSearch() {
    const q = query.trim()
    if (!q) return
    searching = true
    searchError = ''
    results = []
    // A pasted YouTube playlist link → import the whole playlist; otherwise keyword search.
    const listId = parseYouTubePlaylistId(q)
    isPlaylist = !!listId
    try {
      results = listId ? await fetchYouTubePlaylist(listId) : await searchYouTube(q)
      if (results.length === 0) {
        searchError = isPlaylist
          ? 'Empty playlist (or search is unavailable).'
          : 'No results (or search is unavailable).'
      }
    } catch (e) {
      searchError = String((e as Error)?.message ?? e)
    } finally {
      searching = false
    }
  }

  async function add(hit: SearchHit) {
    const track: QueueTrack = { videoId: hit.videoId, title: hit.title, duration: hit.duration }
    await addTrack(groupId, track)
    results = results.filter((r) => r.videoId !== hit.videoId)
  }

  async function addAll() {
    const all: QueueTrack[] = results.map((r) => ({ videoId: r.videoId, title: r.title, duration: r.duration }))
    await addTracks(groupId, all)
    results = []
    query = ''
    isPlaylist = false
  }

  function fmt(s: number): string {
    if (!s) return ''
    const m = Math.floor(s / 60)
    const sec = Math.floor(s % 60)
    return `${m}:${sec.toString().padStart(2, '0')}`
  }
</script>

<div class="queue card">
  <div class="head">
    <h3>My set <span class="count">{tracks.length}</span></h3>
    <div class="head-actions">
      {#if canSkip()}
        <button class="btn btn-ghost btn-sm" onclick={() => skipTrack(groupId)} title="Skip current track">⏭ Skip</button>
      {/if}
      {#if tracks.length > 1}
        <button class="mini" onclick={() => shuffleQueue(groupId)} title="Shuffle">🔀</button>
      {/if}
      {#if tracks.length > 0}
        <button class="mini" onclick={() => clearQueue(groupId)} title="Clear set">🗑</button>
      {/if}
    </div>
  </div>

  <!-- Save / load playlists -->
  <div class="lib">
    {#if saving}
      <input class="lib-name" bind:value={saveName} placeholder="Playlist name…" maxlength="60" autocomplete="off" />
      <button class="btn btn-primary btn-sm" onclick={doSave} disabled={!saveName.trim() || tracks.length === 0}>Save</button>
      <button class="mini" onclick={() => { saving = false; saveName = '' }} title="Cancel">✕</button>
    {:else}
      {#if tracks.length > 0}
        <button class="mini wide" onclick={() => (saving = true)}>💾 Save as playlist</button>
      {/if}
      {#if playlists.mine.length > 0}
        <button class="mini wide" onclick={() => (showLib = !showLib)}>📚 My playlists ({playlists.mine.length})</button>
      {/if}
    {/if}
  </div>
  {#if showLib && playlists.mine.length > 0}
    <ul class="lib-list">
      {#each playlists.mine as pl (pl.id)}
        <li>
          <span class="t-title">{pl.name}</span>
          <span class="dur">{pl.tracks.length}</span>
          <button class="add" onclick={() => loadPl(pl)} title="Load into my set">Load</button>
          <button class="rm" onclick={() => deletePlaylist(pl.id)} title="Delete playlist">✕</button>
        </li>
      {/each}
    </ul>
  {/if}

  <!-- My tracks (top), drag-free reordering with the arrows -->
  {#if tracks.length > 0}
    <ul class="tracks">
      {#each tracks as track, i (track.videoId + i)}
        <li class:played={track.active === false}>
          <span class="t-idx">{i + 1}</span>
          <span class="t-title">{track.title}</span>
          <span class="dur">{fmt(track.duration)}</span>
          <span class="reorder">
            <button class="ord" onclick={() => moveTrack(groupId, i, i - 1)} disabled={i === 0} title="Move up">▲</button>
            <button class="ord" onclick={() => moveTrack(groupId, i, i + 1)} disabled={i === tracks.length - 1} title="Move down">▼</button>
          </span>
          <button class="rm" onclick={() => removeTrack(groupId, i)} title="Remove">✕</button>
        </li>
      {/each}
    </ul>
  {:else if onStage}
    <p class="hint">Your set is empty — search below to add tracks.</p>
  {/if}

  {#if !onStage}
    <p class="hint">Go on stage to add tracks to the round-robin.</p>
  {/if}

  <!-- Search to add tracks (below the set) -->
  <form class="search" onsubmit={(e) => { e.preventDefault(); doSearch() }}>
    <input
      bind:value={query}
      placeholder="Search YouTube or paste a playlist link…"
      maxlength="200"
      autocomplete="off"
    />
    <button class="btn btn-primary btn-sm" type="submit" disabled={searching || !query.trim()}>
      {searching ? '…' : 'Search'}
    </button>
  </form>
  {#if searchError}<p class="err">{searchError}</p>{/if}

  {#if isPlaylist && results.length > 0}
    <div class="pl-bar">
      <span>Playlist · {results.length} tracks</span>
      <button class="btn btn-primary btn-sm" onclick={addAll}>+ Add all</button>
    </div>
  {/if}

  {#if results.length > 0}
    <ul class="results">
      {#each results as hit (hit.videoId)}
        <li>
          <img class="thumb" src={`https://i.ytimg.com/vi/${hit.videoId}/default.jpg`} alt="" width="48" height="36" loading="lazy" />
          <span class="r-title">{hit.title}</span>
          <span class="dur">{fmt(hit.duration)}</span>
          <button class="add" onclick={() => add(hit)} title="Add to queue">+ Add</button>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .queue {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.9rem 1rem;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.7rem;
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
  .head-actions {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }
  .hint {
    margin: 0 0 0.7rem;
    font-size: 0.8rem;
    color: var(--text-dim);
  }
  .search {
    display: flex;
    gap: 0.5rem;
    margin: 0.9rem 0 0.6rem;
    padding-top: 0.9rem;
    border-top: 1px solid var(--border);
  }
  .search input {
    flex: 1;
    min-width: 0;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.7rem;
    color: var(--text);
    font-size: 0.88rem;
  }
  .search input:focus {
    outline: none;
    border-color: var(--accent-2);
  }
  .pl-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.6rem;
    margin-bottom: 0.6rem;
    font-size: 0.82rem;
    color: var(--text-dim);
  }
  .results,
  .tracks {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .results {
    margin-top: 0.6rem;
  }
  .results li,
  .tracks li {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    font-size: 0.85rem;
  }
  .reorder {
    display: inline-flex;
    flex-direction: column;
    gap: 1px;
    flex: 0 0 auto;
  }
  .ord {
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 4px;
    width: 18px;
    height: 13px;
    font-size: 0.55rem;
    line-height: 1;
    cursor: pointer;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .ord:hover:not(:disabled) {
    border-color: var(--accent-2);
    color: var(--text);
  }
  .ord:disabled {
    opacity: 0.3;
    cursor: default;
  }
  .thumb {
    width: 48px;
    height: 36px;
    border-radius: 5px;
    object-fit: cover;
    flex: 0 0 auto;
    background: var(--bg-elev-2);
  }
  .r-title,
  .t-title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tracks li.played .t-title {
    color: var(--text-dim);
    text-decoration: line-through;
  }
  .t-idx {
    flex: 0 0 1.4rem;
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
  }
  .dur {
    flex: 0 0 auto;
    color: var(--text-dim);
    font-size: 0.76rem;
    font-variant-numeric: tabular-nums;
  }
  .add {
    flex: 0 0 auto;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--accent);
    border-radius: 7px;
    padding: 0.25rem 0.55rem;
    font-size: 0.75rem;
    cursor: pointer;
  }
  .add:hover {
    border-color: var(--accent);
  }
  .rm {
    flex: 0 0 auto;
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 0.8rem;
  }
  .rm:hover {
    color: var(--danger);
  }
  .mini {
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 7px;
    padding: 0.25rem 0.45rem;
    font-size: 0.8rem;
    cursor: pointer;
  }
  .mini:hover {
    border-color: var(--accent-2);
    color: var(--text);
  }
  .mini.wide {
    padding: 0.35rem 0.65rem;
  }
  .lib {
    display: flex;
    gap: 0.4rem;
    flex-wrap: wrap;
    margin-bottom: 0.6rem;
  }
  .lib-name {
    flex: 1;
    min-width: 0;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.4rem 0.6rem;
    color: var(--text);
    font-size: 0.85rem;
  }
  .lib-name:focus {
    outline: none;
    border-color: var(--accent-2);
  }
  .lib-list {
    list-style: none;
    margin: 0 0 0.6rem;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .lib-list li {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    font-size: 0.85rem;
  }
  .err {
    color: var(--danger);
    font-size: 0.8rem;
    margin: 0 0 0.5rem;
  }
</style>

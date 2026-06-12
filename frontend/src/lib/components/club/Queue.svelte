<script lang="ts">
  import { queues, addTrack, addTracks, removeTrack, setMyQueue, clearQueue, shuffleQueue, setTrackActive, enrichQueueTitles, reactivateMyQueue } from '../../nostr/queue.svelte'
  import { requestSkip, canSkip } from '../../nostr/sync.svelte'
  import { playlists, savePlaylistAs, deletePlaylist, loadMyPlaylists } from '../../nostr/playlists.svelte'
  import { ownPremium } from '../../nostr/premium.svelte'
  import PremiumModal from '../PremiumModal.svelte'
  import { searchYouTube, fetchYouTubePlaylist, parseYouTubePlaylistId, type SearchHit } from '../../player/youtube'
  import { auth } from '../../nostr/auth.svelte'
  import { stage } from '../../nostr/stage.svelte'
  import type { QueueTrack, Playlist } from '../../nostr/types'
  import TrackPreview from '../TrackPreview.svelte'
  import { marquee } from '../../actions/marquee'
  import RtmpStreamButton from './RtmpStreamButton.svelte'

  let { groupId, clubName = '', canModerate = false }: { groupId: string; clubName?: string; canModerate?: boolean } = $props()

  const me = $derived(auth.pubkey)
  const onStage = $derived(stage.isOnStage(me))
  const myQueue = $derived(me ? queues.get(me) : null)
  const tracks = $derived(myQueue?.tracks ?? [])

  // Saved playlists (library): load once when signed in.
  $effect(() => {
    if (me && !playlists.loaded) void loadMyPlaylists()
  })

  // Backfill bare titles with the interpreter (like the card) — once per mount, when my queue
  // has tracks still missing an artist. Resolved via the relay (oEmbed), persisted to the queue.
  let titlesEnriched = false
  $effect(() => {
    if (titlesEnriched || !me) return
    if (tracks.some((t) => !/ [–—-] /.test(t.title))) {
      titlesEnriched = true
      void enrichQueueTitles(groupId)
    }
  })
  let saving = $state(false)
  let saveName = $state('')
  let showLib = $state(false)
  let showPremModal = $state(false)

  async function doSave() {
    if (!saveName.trim() || tracks.length === 0) return
    await savePlaylistAs(saveName, tracks.map((t) => ({ videoId: t.videoId, title: t.title, duration: t.duration })))
    saveName = ''
    saving = false
  }
  // Which saved playlist the current set was loaded from (shown in the header). Stays as long
  // as the set still contains exactly that playlist's tracks — diverges (add/remove) → cleared.
  let selectedPlaylistId = $state<string | null>(null)
  const selectedPlaylist = $derived.by(() => {
    if (!selectedPlaylistId) return null
    const pl = playlists.mine.find((p) => p.id === selectedPlaylistId)
    if (!pl) return null
    const setIds = new Set(tracks.map((t) => t.videoId))
    return setIds.size === pl.tracks.length && pl.tracks.every((t) => setIds.has(t.videoId)) ? pl : null
  })

  async function loadPl(pl: Playlist) {
    await setMyQueue(groupId, pl.tracks.map((t) => ({ videoId: t.videoId, title: t.title, duration: t.duration })))
    selectedPlaylistId = pl.id
    showLib = false
  }

  // ▶ preview / details modal.
  let preview = $state<{ track: QueueTrack; context: 'queue' | 'search' } | null>(null)

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

  // Pointer-based drag-to-reorder — works with touch AND mouse (HTML5 drag has no touch events).
  let drag = $state<{ from: number } | null>(null)
  let dropIndex = $state<number | null>(null)

  function dragStart(e: PointerEvent, from: number) {
    drag = { from }
    dropIndex = from
    ;(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId)
    window.addEventListener('pointermove', dragMove)
    window.addEventListener('pointerup', dragEnd)
  }
  function dragMove(e: PointerEvent) {
    if (!drag) return
    const li = (document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null)
      ?.closest('li[data-i]') as HTMLElement | null
    if (li?.dataset.i != null && li.closest('ul[data-queue]')) {
      const i = Number(li.dataset.i)
      if (!Number.isNaN(i)) dropIndex = i
    }
  }
  function dragEnd() {
    window.removeEventListener('pointermove', dragMove)
    window.removeEventListener('pointerup', dragEnd)
    if (drag && dropIndex !== null && drag.from !== dropIndex) {
      const ts = [...tracks]
      const [item] = ts.splice(drag.from, 1)
      ts.splice(dropIndex, 0, item)
      void setMyQueue(groupId, ts)
    }
    drag = null
    dropIndex = null
  }
</script>

<div class="queue card">
  <div class="head">
    <h3>My live Playlist <span class="count">{tracks.length}</span>{#if selectedPlaylist}<span class="from">📚 {selectedPlaylist.name}</span>{/if}</h3>
    <div class="head-actions">
      {#if canSkip(canModerate)}
        <button class="btn btn-ghost btn-sm" onclick={() => requestSkip(groupId)} title="Skip current track">⏭ Skip</button>
      {/if}
      {#if tracks.some((t) => t.active === false)}
        <button class="mini" onclick={() => reactivateMyQueue(groupId)} title="Re-activate all played tracks">↻ All</button>
      {/if}
      {#if tracks.length > 1}
        <button class="mini" onclick={() => shuffleQueue(groupId)} title="Shuffle">🔀</button>
      {/if}
      {#if tracks.length > 0}
        <button class="mini" onclick={() => clearQueue(groupId)} title="Clear set">🗑</button>
      {/if}
    </div>
  </div>

  {#if onStage}
    <div class="rtmp-row">
      <RtmpStreamButton {groupId} {clubName} />
    </div>
  {/if}

  <!-- Save / load playlists. Free: 1 playlist. Premium: unlimited. -->
  <div class="lib">
    {#if saving}
      <input class="lib-name" bind:value={saveName} placeholder="Playlist name…" maxlength="60" autocomplete="off" />
      <button class="btn btn-primary btn-sm" onclick={doSave} disabled={!saveName.trim() || tracks.length === 0}>Save</button>
      <button class="mini" onclick={() => { saving = false; saveName = '' }} title="Cancel">✕</button>
    {:else}
      {#if tracks.length > 0 && (ownPremium.active || playlists.mine.length < 1)}
        <button class="mini wide" onclick={() => (saving = true)}>💾 Save as playlist</button>
      {/if}
      {#if playlists.mine.length > 0}
        <button class="mini wide" onclick={() => (showLib = !showLib)}>📚 My playlists ({playlists.mine.length})</button>
      {/if}
      {#if !ownPremium.active && playlists.mine.length >= 1 && tracks.length > 0}
        <button class="mini wide prem-upsell" onclick={() => (showPremModal = true)} title="Upgrade for unlimited playlists">⚡ More playlists — Premium</button>
      {/if}
    {/if}
  </div>
  {#if showLib && playlists.mine.length > 0}
    <ul class="lib-list">
      {#each playlists.mine as pl (pl.id)}
        <li class:sel={selectedPlaylist?.id === pl.id}>
          {#if selectedPlaylist?.id === pl.id}<span class="sel-dot" title="Currently loaded">●</span>{/if}
          <span class="t-title" use:marquee><span class="mq-inner">{pl.name}</span></span>
          <span class="dur">{pl.tracks.length}</span>
          <button class="add" onclick={() => loadPl(pl)} title="Load into my set">{selectedPlaylist?.id === pl.id ? 'Loaded' : 'Load'}</button>
          <button class="rm" onclick={() => deletePlaylist(pl.id)} title="Delete playlist">✕</button>
        </li>
      {/each}
    </ul>
  {/if}

  {#if showPremModal}<PremiumModal onClose={() => (showPremModal = false)} />{/if}

  {#if tracks.length > 0}
    <ul class="tracks" data-queue>
      {#each tracks as track, i (track.videoId + i)}
        <li
          data-i={i}
          class:played={track.active === false}
          class:dragging={drag?.from === i}
          class:drop={dropIndex === i && drag?.from !== i}
        >
          <span class="grip" onpointerdown={(e) => dragStart(e, i)} title="Drag to reorder">⠿</span>
          <button class="play" onclick={() => (preview = { track, context: 'queue' })} title="Preview / edit details">▶</button>
          <span class="t-idx">{i + 1}</span>
          <span class="t-title" use:marquee><span class="mq-inner">{track.title}</span></span>
          <span class="dur">{fmt(track.duration)}</span>
          {#if track.active === false}
            <button class="reactivate" onclick={() => setTrackActive(groupId, track.videoId, true)} title="Play again — re-activate this track">↻</button>
          {/if}
          <button class="rm" onclick={() => removeTrack(groupId, i)} title="Remove">✕</button>
        </li>
      {/each}
    </ul>
  {:else if onStage}
    <p class="hint">Your set is empty — search below to add tracks.</p>
  {/if}

  {#if !onStage}
    <p class="hint">Enter stage to add tracks to the round-robin.</p>
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
          <button class="play" onclick={() => (preview = { track: { videoId: hit.videoId, title: hit.title, duration: hit.duration }, context: 'search' })} title="Preview">▶</button>
          <img class="thumb" src={`https://i.ytimg.com/vi/${hit.videoId}/default.jpg`} alt="" width="48" height="36" loading="lazy" />
          <span class="r-title">{hit.title}</span>
          <span class="dur">{fmt(hit.duration)}</span>
          <button class="add" onclick={() => add(hit)} title="Add to queue">+ Add</button>
        </li>
      {/each}
    </ul>
  {/if}
</div>

{#if preview}
  <TrackPreview
    track={preview.track}
    context={preview.context}
    {groupId}
    canEdit={preview.context === 'queue' && !!me}
    onAdd={preview.context === 'search'
      ? (t) => { void addTrack(groupId, t); results = results.filter((r) => r.videoId !== t.videoId) }
      : undefined}
    onClose={() => (preview = null)}
  />
{/if}

<style>
  .queue {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.9rem 1rem;
  }
  .rtmp-row {
    margin-bottom: 0.5rem;
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
  .from {
    margin-left: 0.5rem;
    font-size: 0.78rem;
    font-weight: 600;
    color: var(--accent);
    vertical-align: middle;
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
  .play {
    flex: 0 0 auto;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--accent);
    border-radius: 50%;
    width: 24px;
    height: 24px;
    font-size: 0.62rem;
    line-height: 1;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0;
  }
  .play:hover {
    border-color: var(--accent);
    filter: brightness(1.15);
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
    white-space: nowrap;
  }
  .t-title .mq-inner {
    display: inline-block;
  }
  /* Scroll on row hover (desktop); always on touch */
  .tracks li:hover .t-title[data-mq] .mq-inner,
  .lib-list li:hover .t-title[data-mq] .mq-inner {
    animation: t-scroll 4s ease-in-out infinite;
  }
  @media (hover: none) {
    .t-title[data-mq] .mq-inner {
      animation: t-scroll 5s ease-in-out 0.5s infinite;
    }
  }
  @keyframes t-scroll {
    0%, 20%  { transform: translateX(0); }
    80%, 100% { transform: translateX(var(--mq-shift, 0px)); }
  }
  .tracks li.played .t-title {
    color: var(--text-dim);
    text-decoration: line-through;
  }
  .grip {
    flex: 0 0 auto;
    color: var(--text-dim);
    font-size: 1rem;
    cursor: grab;
    line-height: 1;
    touch-action: none;
    user-select: none;
    padding: 0 2px;
  }
  .grip:active {
    cursor: grabbing;
  }
  .tracks li.dragging {
    opacity: 0.4;
  }
  .tracks li.drop {
    outline: 2px solid var(--accent-2);
    border-radius: var(--radius-sm);
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
  .reactivate {
    flex: 0 0 auto;
    background: none;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: 0.85rem;
    line-height: 1;
  }
  .reactivate:hover {
    filter: brightness(1.2);
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
  .prem-upsell {
    border-color: var(--amber);
    color: var(--amber);
  }
  .prem-upsell:hover {
    background: color-mix(in srgb, var(--amber) 10%, transparent);
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
  .lib-list li.sel .t-title {
    color: var(--accent);
    font-weight: 700;
  }
  .sel-dot {
    color: var(--accent);
    font-size: 0.7rem;
    flex: 0 0 auto;
  }
  .err {
    color: var(--danger);
    font-size: 0.8rem;
    margin: 0 0 0.5rem;
  }
</style>

<script lang="ts">
  import { decode } from 'nostr-tools/nip19'
  import { auth } from '../nostr/auth.svelte'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import {
    playlists,
    fetchPlaylists,
    deletePlaylist,
    loadMyPlaylists,
    reorderTrack,
    removeFromPlaylist,
    createPlaylist,
    addTrackToPlaylist,
    addTracksToPlaylist,
  } from '../nostr/playlists.svelte'
  import { addTracks } from '../nostr/queue.svelte'
  import { persistedStageGroup } from '../nostr/stage.svelte'
  import { searchYouTube, fetchYouTubePlaylist, parseYouTubePlaylistId, type SearchHit } from '../player/youtube'
  import { fetchUserClubActivity } from '../nostr/groups'
  import { goClub, goUser, goHowto, goLeaderboard } from '../router.svelte'
  import TrackPreview from './TrackPreview.svelte'
  import { clubAvatar } from '../avatar'
  import { npubEncode } from 'nostr-tools/nip19'
  import { publishMyProfile } from '../nostr/profileEdit'
  import { logout } from '../nostr/nostrLogin'
  import { fetchReceivedZaps, requestZapInvoice, type ReceivedZaps } from '../nostr/zaps.svelte'
  import { showPay } from '../nostr/payModal.svelte'
  import { fetchZapRank, type ZapRank } from '../nostr/leaderboard'
  import { fetchUserLikes, unlikeTrack, type UserLike } from '../nostr/likes.svelte'
  import { isSuperadmin } from '../nostr/admin'
  import { ownPremium } from '../nostr/premium.svelte'
  import PremiumModal from './PremiumModal.svelte'
  import type { Playlist, QueueTrack, Club } from '../nostr/types'

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
  // Liked tracks + playlists are PRIVATE: only the owner (logged in as themselves) or the
  // superadmin may see them. Everyone else sees just the public profile + the zap ranking.
  const canSeePrivate = $derived(isMe || isSuperadmin())
  const profile = $derived(pubkey ? useProfile(pubkey) : null)

  let othersPlaylists = $state<Playlist[]>([])
  let loading = $state(true)
  let memberOf = $state<Club[]>([])
  let djingIn = $state<Club[]>([])
  let topClubId = $state<string | null>(null)
  let rolesById = $state<Record<string, string[]>>({})
  let received = $state<ReceivedZaps | null>(null)
  let zapRank = $state<ZapRank | null>(null)
  let likedTracks = $state<UserLike[]>([])

  // Clubs the user is currently on stage in (relay-derived djingIn; on the own profile also
  // the locally-persisted stage marker) → green "on stage" border in the clubs list, like home.
  const onStageIds = $derived(
    new Set<string>([
      ...djingIn.map((c) => c.id),
      ...(isMe && persistedStageGroup() ? [persistedStageGroup() as string] : []),
    ]),
  )

  $effect(() => {
    const pk = pubkey
    if (!pk) {
      loading = false
      return
    }
    loading = true
    // Liked tracks + playlists are private (owner or superadmin only). Don't even fetch them
    // for a stranger — they can't be shown and it saves the relay queries.
    likedTracks = []
    othersPlaylists = []
    if (canSeePrivate) {
      void fetchUserLikes(pk).then((l) => (likedTracks = l))
      if (isMe) {
        void loadMyPlaylists().finally(() => (loading = false))
      } else {
        fetchPlaylists(pk)
          .then((p) => (othersPlaylists = p))
          .finally(() => (loading = false))
      }
    } else {
      loading = false
    }
    // Clubs this user is a member of (current/last-DJ'd pinned on top), public for everyone.
    memberOf = []
    djingIn = []
    topClubId = null
    rolesById = {}
    void fetchUserClubActivity(pk).then((a) => {
      memberOf = a.memberOf
      djingIn = a.djingIn
      topClubId = a.topClubId
      rolesById = a.rolesById
    })
    // Public zap standing: sats received, how many people, and the global placement — all from the
    // 9735-based leaderboard (shown to everyone). The detailed "who zapped you" list (sender
    // identities) stays owner-only further down.
    received = null
    zapRank = null
    void fetchZapRank(pk).then((r) => (zapRank = r))
    if (isMe) void fetchReceivedZaps(pk).then((r) => (received = r))
  })

  // ── Profile editor (own profile only) ──────────────────────────────────
  let editing = $state(false)
  let showPremModal = $state(false)
  let eName = $state('')
  let eAbout = $state('')
  let ePic = $state('')
  let eLud16 = $state('')
  let eNip05 = $state('')
  let saving = $state(false)
  let saveErr = $state('')

  function openEditor() {
    eName = profile?.display_name || profile?.name || ''
    eAbout = profile?.about || ''
    ePic = profile?.picture || ''
    eLud16 = (profile?.lud16 as string) || ''
    eNip05 = profile?.nip05 || ''
    saveErr = ''
    editing = true
  }

  async function saveProfile() {
    saving = true
    saveErr = ''
    try {
      await publishMyProfile({
        display_name: eName.trim(),
        about: eAbout.trim(),
        picture: ePic.trim(),
        lud16: eLud16.trim(),
        nip05: eNip05.trim(),
      })
      editing = false
    } catch (e) {
      saveErr = String((e as Error)?.message ?? e)
    } finally {
      saving = false
    }
  }

  const list = $derived(isMe ? playlists.mine : othersPlaylists)

  function fmt(s: number): string {
    if (!s) return ''
    const m = Math.floor(s / 60)
    const sec = Math.floor(s % 60)
    return `${m}:${sec.toString().padStart(2, '0')}`
  }

  // Pointer-based reorder WITHIN a playlist — works with touch AND mouse (HTML5 drag fires no
  // touch events). Cross-playlist move/copy is the ⋯ select below (already touch-friendly).
  let drag = $state<{ plId: string; index: number } | null>(null)
  let dropIndex = $state<number | null>(null)
  function dragStart(e: PointerEvent, plId: string, index: number) {
    drag = { plId, index }
    dropIndex = index
    ;(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId)
    window.addEventListener('pointermove', dragMove)
    window.addEventListener('pointerup', dragEnd)
  }
  function dragMove(e: PointerEvent) {
    if (!drag) return
    const li = (document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null)?.closest('li[data-i]') as HTMLElement | null
    const ol = li?.closest('ol[data-pl]') as HTMLElement | null
    if (li?.dataset.i != null && ol?.dataset.pl === drag.plId) {
      const i = Number(li.dataset.i)
      if (!Number.isNaN(i)) dropIndex = i
    }
  }
  function dragEnd() {
    window.removeEventListener('pointermove', dragMove)
    window.removeEventListener('pointerup', dragEnd)
    if (drag && dropIndex !== null && drag.index !== dropIndex) void reorderTrack(drag.plId, drag.index, dropIndex)
    drag = null
    dropIndex = null
  }

  // ▶ preview / edit-details modal (library context).
  let preview = $state<{ track: QueueTrack; playlistId: string; canEdit: boolean } | null>(null)


  // ── Create / fill playlists + push to the live stage set ─────────────────
  // The club the user is currently a DJ in (if any) — target for "add to set".
  const stageGroup = persistedStageGroup()
  let newName = $state('')
  let creating = $state(false)
  async function doCreate() {
    if (!newName.trim() || creating) return
    creating = true
    try {
      await createPlaylist(newName.trim())
      newName = ''
    } finally {
      creating = false
    }
  }

  // Per-playlist YouTube search/import (to add tracks). Only one open at a time.
  let searchPl = $state<string | null>(null)
  let searchQ = $state('')
  let searchHits = $state<SearchHit[]>([])
  let searching = $state(false)
  let searchError = $state('')
  let isPlaylist = $state(false) // results came from a pasted playlist link
  function openSearch(plId: string) {
    searchPl = searchPl === plId ? null : plId
    searchQ = ''
    searchHits = []
    searchError = ''
    isPlaylist = false
  }
  async function runSearch() {
    const q = searchQ.trim()
    if (!q || searching) return
    searching = true
    searchError = ''
    searchHits = []
    // A pasted YouTube playlist link → import the whole playlist; otherwise keyword search.
    const listId = parseYouTubePlaylistId(q)
    isPlaylist = !!listId
    try {
      searchHits = listId ? await fetchYouTubePlaylist(listId) : await searchYouTube(q)
      if (searchHits.length === 0) {
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
  function addHit(plId: string, h: SearchHit) {
    void addTrackToPlaylist(plId, { videoId: h.videoId, title: h.title, duration: h.duration })
    searchHits = searchHits.filter((r) => r.videoId !== h.videoId)
  }
  function addAllHits(plId: string) {
    void addTracksToPlaylist(
      plId,
      searchHits.map((h) => ({ videoId: h.videoId, title: h.title, duration: h.duration })),
    )
    searchHits = []
    searchQ = ''
    isPlaylist = false
  }

  // Zap this user directly from their public profile (NIP-57 to their lud16). The recipient pubkey
  // goes into the zap request, so the 9735 receipt is attributable → counts on the leaderboard.
  let zapping = $state(false)
  let zapErr = $state('')
  async function zapUser(sats: number) {
    if (zapping || !pubkey || !profile?.lud16) return
    zapping = true
    zapErr = ''
    try {
      const { invoice, verify } = await requestZapInvoice(pubkey, profile.lud16 as string, sats, 'Zap on zapclub')
      showPay(invoice, sats, `Zap ${displayName(pubkey, profile)}`, { verify })
    } catch (e) {
      zapErr = String((e as Error)?.message ?? 'Zap failed')
      setTimeout(() => (zapErr = ''), 4000)
    } finally {
      zapping = false
    }
  }

  // Remove a like (own profile only): optimistic drop + NIP-09 delete of the reaction(s).
  function removeLike(l: UserLike) {
    likedTracks = likedTracks.filter((x) => x.videoId !== l.videoId)
    void unlikeTrack(l.videoId, l.eventIds)
  }

  let addedTo = $state('')
  function addToSet(pl: Playlist) {
    if (!stageGroup || pl.tracks.length === 0) return
    void addTracks(
      stageGroup,
      pl.tracks.map((t) => ({ videoId: t.videoId, title: t.title, duration: t.duration })),
    )
    addedTo = pl.id
    setTimeout(() => (addedTo = addedTo === pl.id ? '' : addedTo), 1800)
  }
</script>

<div class="wrap">
  <header class="phead">
    <img class="pavatar" src={avatarUrl(pubkey, profile)} alt="" width="64" height="64" />
    <div class="pinfo">
      <h1>{displayName(pubkey, profile)}</h1>
      <div class="pid">
        <span class="npub">{npub.slice(0, 18)}…</span>
        <a class="njump" href={`https://njump.me/${npub}`} target="_blank" rel="noopener noreferrer" title="Open this profile on njump.me for full Nostr details">on Nostr ↗</a>
      </div>
      {#if profile?.about}<p class="pabout">{profile.about}</p>{/if}
      {#if profile?.lud16}
        <p class="plud">⚡ {profile.lud16}</p>
        {#if !isMe}
          <div class="zap-row">
            <span class="zap-label">⚡ Zap</span>
            {#each [100, 1000, 5000] as amt (amt)}
              <button class="zap-amt" onclick={() => zapUser(amt)} disabled={zapping}>{amt >= 1000 ? `${amt / 1000}k` : amt}</button>
            {/each}
            <span class="zap-sats">sats</span>
          </div>
          {#if zapErr}<p class="zap-err">⚠ {zapErr}</p>{/if}
        {/if}
      {:else if isMe}
        <p class="plud missing">⚡ No lightning address yet — add one to receive sats.</p>
      {/if}
    </div>
    {#if isMe}
      <div class="me-actions">
        <button class="edit-btn" onclick={openEditor} title="Edit profile">✏️ Edit</button>
        <button class="prem-btn" onclick={() => (showPremModal = true)} title="zapclub Premium">
          {#if ownPremium.active}⚡ Premium active{:else}⚡ Get Premium{/if}
        </button>
        <button class="logout-btn" onclick={() => logout()} title="Log out of zapclub">Log out</button>
      </div>
    {/if}
  </header>

  {#if editing}
    <div class="card editor">
      <h2>Edit profile</h2>
      <label class="fld">Display name
        <input class="in" bind:value={eName} maxlength="60" placeholder="Your name" />
      </label>
      <label class="fld">About
        <textarea class="in" bind:value={eAbout} rows="2" maxlength="280" placeholder="A short bio"></textarea>
      </label>
      <label class="fld">Picture URL
        <input class="in" bind:value={ePic} placeholder="https://…" />
      </label>
      <label class="fld">⚡ Lightning address (lud16)
        <input class="in" bind:value={eLud16} placeholder="you@walletofsatoshi.com" autocomplete="off" />
        <span class="fhint">Needed to receive zaps. No address? See the <a href="/howto" onclick={(e) => { e.preventDefault(); goHowto() }}>How-to</a> for one-click providers.</span>
      </label>
      <label class="fld">NIP-05 (optional)
        <input class="in" bind:value={eNip05} placeholder="you@domain.com" autocomplete="off" />
      </label>
      {#if saveErr}<p class="err">⚠ {saveErr}</p>{/if}
      <div class="editor-actions">
        <button class="btn btn-primary btn-sm" onclick={saveProfile} disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
        <button class="btn btn-ghost btn-sm" onclick={() => (editing = false)} disabled={saving}>Cancel</button>
      </div>
    </div>
  {/if}

  <!-- Public zap standing: global placement + sats received + how many people (NOT who). The whole
       card links to the leaderboard. Shown to everyone, from the 9735-based ranking. -->
  {#if zapRank}
    {@const medal = zapRank.rank === 1 ? '🥇' : zapRank.rank === 2 ? '🥈' : zapRank.rank === 3 ? '🥉' : '🏆'}
    <a
      class="card ziprank"
      class:top3={zapRank.rank <= 3}
      href="/leaderboard"
      onclick={(e) => { e.preventDefault(); goLeaderboard() }}
      title="View the zap leaderboard"
    >
      <span class="zr-badge"><span class="zr-medal">{medal}</span><span class="zr-rank">#{zapRank.rank.toLocaleString()}</span></span>
      <span class="zr-main">
        <span class="zr-amt">⚡ {zapRank.sats.toLocaleString()} <span class="zr-unit">sats</span></span>
        <span class="zr-sub">
          received from {zapRank.zappers.toLocaleString()} {zapRank.zappers === 1 ? 'person' : 'people'}
          · rank {zapRank.rank.toLocaleString()} of {zapRank.total.toLocaleString()}
        </span>
      </span>
      <span class="zr-cta">Leaderboard →</span>
    </a>
  {/if}

  <!-- Who zapped you (verified 9735, incl. names) — OWNER-ONLY by request. -->
  {#if isMe && received && received.bySender.length > 0}
    <section class="card zaps-recv">
      <h2>⚡ Who zapped you <span class="count">private</span></h2>
      {#if !profile?.lud16}
        <p class="recv-note">
          🙏 No lightning address yet, so these sats went to <strong>zapclub</strong> — thank you!
          <button class="recv-link" onclick={openEditor}>Add a receiving address</button> so future zaps reach you directly.
        </p>
      {/if}
      <ul class="senders">
        {#each received.bySender.slice(0, 12) as s (s.sender)}
          {#if s.anon}
            <li>
              <span class="av anon-av" aria-hidden="true">⚡</span>
              <span class="who anon">Anonymous</span>
              <span class="s-sats">{s.sats.toLocaleString()} sats</span>
              <span class="s-count">{s.count}×</span>
            </li>
          {:else}
            {@const sp = useProfile(s.sender)}
            <li>
              <img class="av" src={avatarUrl(s.sender, sp)} alt="" width="22" height="22" />
              <a class="who" href={`/user/${npubEncode(s.sender)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(s.sender)) }}>{displayName(s.sender, sp)}</a>
              <span class="s-sats">{s.sats.toLocaleString()} sats</span>
              <span class="s-count">{s.count}×</span>
            </li>
          {/if}
        {/each}
      </ul>
    </section>
  {/if}

  {#if canSeePrivate && likedTracks.length > 0}
    <section class="card liked">
      <h2>🔥 {isMe ? 'Tracks you liked' : 'Liked tracks'} <span class="count">{likedTracks.length}</span></h2>
      <ol class="liked-list">
        {#each likedTracks as t (t.videoId)}
          <li>
            <a class="lt-thumb" href={`https://youtu.be/${t.videoId}`} target="_blank" rel="noopener noreferrer" title="Open on YouTube">
              <img src={`https://i.ytimg.com/vi/${t.videoId}/default.jpg`} alt="" loading="lazy" />
            </a>
            <div class="lt-meta">
              <div class="lt-title">{t.title}</div>
              {#if t.clubId}
                <button class="lt-club" onclick={() => goClub(t.clubId)}>played in {t.clubName || 'a club'}</button>
              {/if}
            </div>
            {#if isMe}
              <button class="lt-remove" onclick={() => removeLike(t)} title="Remove like">✕</button>
            {/if}
          </li>
        {/each}
      </ol>
    </section>
  {/if}

  {#snippet roleTag(c: Club)}
    {#if c.owner === pubkey}
      <span class="ctag role host">👑 Host</span>
    {:else if (rolesById[c.id] ?? []).includes('moderator')}
      <span class="ctag role">🛡️ Mod</span>
    {:else if onStageIds.has(c.id)}
      <span class="ctag role">🎛️ DJ</span>
    {/if}
  {/snippet}

  {#snippet clubRow(c: Club, live: boolean, lastStage: boolean)}
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <div class="club-row" class:live role="button" tabindex="0" onclick={() => goClub(c.id)}>
      {#if live}<span class="live-badge">● on stage</span>
      {:else if lastStage}<span class="live-badge last">last on stage</span>{/if}
      <div class="club-pic"><img src={c.picture || clubAvatar(c.owner || c.id)} alt="" /></div>
      <div class="club-meta">
        <div class="club-name">{c.name}</div>
        {#if c.about}<div class="club-about">{c.about}</div>{/if}
        <div class="club-tags">
          {@render roleTag(c)}
          <span class="ctag">👥 {c.memberCount ?? 0} member{(c.memberCount ?? 0) === 1 ? '' : 's'}</span>
        </div>
      </div>
    </div>
  {/snippet}

  {#if memberOf.length}
    <section class="clubs">
      <h2>Member of <span class="count">{memberOf.length}</span></h2>
      <div class="club-list">
        {#each memberOf as c (c.id)}
          {@render clubRow(c, onStageIds.has(c.id), c.id === topClubId && !onStageIds.has(c.id))}
        {/each}
      </div>
    </section>
  {/if}

  <!-- Playlists are PRIVATE: only the owner or the superadmin sees them. -->
  {#if canSeePrivate}
  <section class="pls">
    <div class="pls-head">
      <h2>Playlists {#if list.length}<span class="count">{list.length}</span>{/if}</h2>
      {#if isMe}
        <form class="new-pl" onsubmit={(e) => { e.preventDefault(); void doCreate() }}>
          <input class="in" bind:value={newName} placeholder="New playlist name…" maxlength="60" />
          <button class="btn btn-primary btn-sm" disabled={!newName.trim() || creating}>＋ New</button>
        </form>
      {/if}
    </div>

    {#if loading}
      <p class="dim">Loading playlists…</p>
    {:else if list.length === 0}
      <p class="dim">
        {isMe ? 'No saved playlists yet — save a set from a club’s DJ Station.' : 'No playlists.'}
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
                <ol class="tracks" data-pl={pl.id}>
                  {#each pl.tracks as t, ti (t.videoId)}
                    <li
                      data-i={ti}
                      class:dragging={drag?.plId === pl.id && drag?.index === ti}
                      class:drop={dropIndex === ti && drag?.plId === pl.id && drag?.index !== ti}
                    >
                      {#if isMe}
                        <!-- svelte-ignore a11y_no_static_element_interactions -->
                        <span class="grip" aria-hidden="true" onpointerdown={(e) => dragStart(e, pl.id, ti)}>⠿</span>
                      {/if}
                      <button class="play" onclick={() => (preview = { track: t, playlistId: pl.id, canEdit: isMe })} title="Preview / edit details">▶</button>
                      <span class="t-title">{t.title}</span>
                      <span class="dim dur">{fmt(t.duration)}</span>
                      {#if isMe}
                        <button class="t-rm" title="Remove" onclick={() => removeFromPlaylist(pl.id, t.videoId)}>✕</button>
                      {/if}
                    </li>
                  {/each}
                </ol>
                {#if isMe}<p class="dnd-hint">Drag ⠿ to reorder · ▶ to preview</p>{/if}
              {/if}

              {#if isMe}
                <div class="pl-actions">
                  {#if stageGroup}
                    <button class="btn btn-primary btn-sm" disabled={pl.tracks.length === 0} onclick={() => addToSet(pl)}>
                      {addedTo === pl.id ? '✓ Added' : '▶ Add to my set on stage'}
                    </button>
                  {/if}
                  <button class="btn btn-ghost btn-sm" onclick={() => openSearch(pl.id)}>
                    {searchPl === pl.id ? 'Close' : '＋ Add tracks'}
                  </button>
                </div>
                {#if searchPl === pl.id}
                  <div class="pl-search">
                    <form onsubmit={(e) => { e.preventDefault(); void runSearch() }}>
                      <input class="in" bind:value={searchQ} placeholder="Search YouTube or paste a playlist link…" />
                      <button class="btn btn-primary btn-sm" disabled={!searchQ.trim() || searching}>
                        {searching ? '…' : 'Search'}
                      </button>
                    </form>
                    {#if searchError}
                      <p class="pl-search-err">{searchError}</p>
                    {/if}
                    {#if isPlaylist && searchHits.length > 0}
                      <button class="btn btn-primary btn-sm pl-addall" onclick={() => addAllHits(pl.id)}>
                        ＋ Add all {searchHits.length} tracks
                      </button>
                    {/if}
                    {#each searchHits as h (h.videoId)}
                      <button class="hit" onclick={() => addHit(pl.id, h)} title="Add to playlist">
                        <span class="hit-title">{h.title}</span>
                        <span class="hit-add">＋</span>
                      </button>
                    {/each}
                  </div>
                {/if}
              {/if}
            </details>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
  {/if}
</div>

{#if preview}
  <TrackPreview
    track={preview.track}
    context="library"
    playlistId={preview.playlistId}
    canEdit={preview.canEdit}
    onClose={() => (preview = null)}
  />
{/if}

{#if showPremModal}
  <PremiumModal onClose={() => (showPremModal = false)} />
{/if}

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
    flex: 1;
  }
  h1 {
    margin: 0;
    font-size: 1.4rem;
  }
  .pid {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .npub {
    font-size: 0.78rem;
    color: var(--text-dim);
    font-family: ui-monospace, monospace;
  }
  .njump {
    font-size: 0.74rem;
    color: var(--accent-2);
    text-decoration: none;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.05rem 0.5rem;
    white-space: nowrap;
  }
  .njump:hover {
    border-color: var(--accent-2);
  }
  .recv-note {
    margin: 0 0 0.7rem;
    font-size: 0.8rem;
    color: var(--text-dim);
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.7rem;
    line-height: 1.45;
  }
  .recv-note strong {
    color: var(--accent);
  }
  .recv-link {
    background: none;
    border: none;
    padding: 0;
    color: var(--accent-2);
    font: inherit;
    font-weight: 600;
    cursor: pointer;
    text-decoration: underline;
  }
  .pabout {
    margin: 0.4rem 0 0;
    color: var(--text-dim);
    font-size: 0.9rem;
  }
  .plud {
    margin: 0.35rem 0 0;
    font-size: 0.82rem;
    color: var(--amber);
    font-weight: 600;
  }
  .plud.missing {
    color: var(--text-dim);
    font-weight: 400;
    font-style: italic;
  }
  .zap-row {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    margin-top: 0.5rem;
    flex-wrap: wrap;
  }
  .zap-label {
    font-size: 0.8rem;
    font-weight: 800;
    color: var(--amber);
  }
  .zap-amt {
    background: var(--bg-elev-2);
    border: 1px solid color-mix(in srgb, var(--amber) 35%, var(--border));
    color: var(--amber);
    border-radius: 999px;
    padding: 0.2rem 0.7rem;
    font-size: 0.82rem;
    font-weight: 800;
    cursor: pointer;
    font-variant-numeric: tabular-nums;
  }
  .zap-amt:hover:not(:disabled) {
    background: var(--amber);
    color: #07070a;
  }
  .zap-amt:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .zap-sats {
    font-size: 0.74rem;
    color: var(--text-dim);
  }
  .zap-err {
    margin: 0.3rem 0 0;
    font-size: 0.76rem;
    color: var(--danger);
  }
  .edit-btn {
    flex: 0 0 auto;
    align-self: flex-start;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 999px;
    padding: 0.3rem 0.7rem;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .edit-btn:hover {
    border-color: var(--accent-2);
    color: var(--text);
  }
  .me-actions {
    flex: 0 0 auto;
    align-self: flex-start;
    display: flex;
    gap: 0.4rem;
  }
  .logout-btn {
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 999px;
    padding: 0.3rem 0.7rem;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .logout-btn:hover {
    border-color: var(--danger);
    color: var(--danger);
  }
  .prem-btn {
    background: var(--bg-elev-2);
    border: 1px solid var(--amber, #f59e0b);
    color: var(--amber, #f59e0b);
    border-radius: 999px;
    padding: 0.3rem 0.7rem;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .prem-btn:hover {
    background: color-mix(in srgb, var(--amber, #f59e0b) 12%, transparent);
  }
  .editor {
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
    margin-top: 1rem;
  }
  .editor h2 {
    margin: 0;
    font-size: 1.05rem;
  }
  .fld {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    font-size: 0.8rem;
    color: var(--text-dim);
  }
  .in {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.7rem;
    color: var(--text);
    font-size: 0.88rem;
    font-family: inherit;
  }
  .in:focus {
    outline: none;
    border-color: var(--accent-2);
  }
  .fhint {
    font-size: 0.72rem;
    color: var(--text-dim);
  }
  .editor-actions {
    display: flex;
    gap: 0.5rem;
  }
  .err {
    color: var(--danger);
    font-size: 0.82rem;
    margin: 0;
  }
  /* Public zap standing — clickable card linking to the leaderboard. */
  .ziprank {
    margin-top: 1rem;
    display: flex;
    align-items: center;
    gap: 1rem;
    text-decoration: none;
    color: var(--text);
    position: relative;
    overflow: hidden;
    background:
      radial-gradient(120% 140% at 0% 0%, rgba(245, 166, 35, 0.12) 0%, transparent 55%),
      linear-gradient(135deg, var(--bg-elev) 0%, var(--bg-elev-2) 100%);
    border-color: color-mix(in srgb, var(--amber) 45%, var(--border));
    transition: border-color 0.15s ease, transform 0.08s ease;
  }
  .ziprank:hover {
    border-color: var(--amber);
  }
  .ziprank:active {
    transform: translateY(1px);
  }
  .zr-badge {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.05rem;
    width: 64px;
    height: 64px;
    border-radius: 16px;
    background: var(--bg);
    border: 1px solid var(--border);
  }
  .ziprank.top3 .zr-badge {
    border-color: var(--amber);
    box-shadow: 0 0 14px rgba(245, 166, 35, 0.35);
  }
  .zr-medal {
    font-size: 1.25rem;
    line-height: 1;
  }
  .zr-rank {
    font-size: 0.92rem;
    font-weight: 900;
    color: var(--text);
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.02em;
  }
  .zr-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }
  .zr-amt {
    font-size: 1.7rem;
    font-weight: 900;
    color: var(--amber);
    line-height: 1;
    letter-spacing: -0.02em;
    font-variant-numeric: tabular-nums;
  }
  .zr-unit {
    font-size: 0.85rem;
    font-weight: 700;
    color: var(--amber);
    opacity: 0.7;
  }
  .zr-sub {
    font-size: 0.78rem;
    color: var(--text-dim);
  }
  .zr-cta {
    flex: 0 0 auto;
    align-self: center;
    font-size: 0.76rem;
    font-weight: 700;
    color: var(--accent-2);
    white-space: nowrap;
  }
  .ziprank:hover .zr-cta {
    color: var(--amber);
  }
  @media (max-width: 460px) {
    .zr-cta {
      display: none;
    }
  }
  .zaps-recv {
    margin-top: 1rem;
  }
  .zaps-recv h2 {
    margin: 0 0 0.6rem;
    font-size: 1.05rem;
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .senders {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.45rem;
  }
  .senders li {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.85rem;
  }
  .senders .av {
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    flex: 0 0 auto;
  }
  .senders .who {
    color: var(--text);
    text-decoration: none;
    font-weight: 600;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .senders .who:hover {
    color: var(--accent-2);
  }
  /* Anonymous (guest) zaps: a neutral bolt avatar + dimmed label, no profile link. */
  .senders .anon-av {
    width: 22px;
    height: 22px;
    display: grid;
    place-items: center;
    font-size: 0.78rem;
    border: 1px solid var(--border);
  }
  .senders .who.anon {
    color: var(--text-dim);
    font-style: italic;
  }
  .s-sats {
    color: var(--amber);
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }
  .s-count {
    color: var(--text-dim);
    font-size: 0.74rem;
  }
  .liked {
    margin-top: 1rem;
  }
  .liked h2 {
    margin: 0 0 0.6rem;
    font-size: 1.05rem;
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
  }
  .liked-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .liked-list li {
    display: flex;
    align-items: center;
    gap: 0.7rem;
  }
  .lt-thumb {
    flex: 0 0 auto;
    width: 56px;
    height: 38px;
    border-radius: 6px;
    overflow: hidden;
    background: #000;
  }
  .lt-thumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .lt-meta {
    flex: 1;
    min-width: 0;
  }
  .lt-title {
    font-weight: 600;
    font-size: 0.9rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .lt-club {
    background: none;
    border: none;
    padding: 0;
    color: var(--accent);
    font-size: 0.76rem;
    cursor: pointer;
  }
  .lt-club:hover {
    text-decoration: underline;
  }
  .lt-remove {
    flex: 0 0 auto;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 7px;
    width: 28px;
    height: 28px;
    cursor: pointer;
    font-size: 0.8rem;
  }
  .lt-remove:hover {
    color: var(--danger);
    border-color: var(--danger);
  }
  .clubs {
    margin-top: 1.4rem;
  }
  .club-list {
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
    margin-top: 0.6rem;
  }
  .club-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 0.9rem;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.8rem;
    cursor: pointer;
    color: var(--text);
    transition: border-color 0.15s ease, transform 0.08s ease;
  }
  .club-row:hover {
    border-color: var(--accent-2);
  }
  .club-row:active {
    transform: translateY(1px);
  }
  .club-row.live {
    border-color: var(--accent);
    animation: club-pulse 1.6s ease-in-out infinite;
  }
  @keyframes club-pulse {
    0%,
    100% {
      box-shadow: 0 0 0 1px var(--accent), 0 0 6px rgba(74, 222, 94, 0.25);
    }
    50% {
      box-shadow: 0 0 0 1px var(--accent), 0 0 16px rgba(74, 222, 94, 0.55);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .club-row.live {
      animation: none;
    }
  }
  .live-badge {
    position: absolute;
    top: 8px;
    right: 10px;
    font-size: 0.64rem;
    font-weight: 800;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  .live-badge.last {
    color: var(--text-dim);
    font-weight: 700;
  }
  .ctag.role {
    color: var(--accent-2);
    border-color: var(--accent-2);
    font-weight: 700;
  }
  .ctag.role.host {
    color: var(--amber);
    border-color: var(--amber);
  }
  .club-pic {
    width: 52px;
    height: 52px;
    flex: 0 0 52px;
    border-radius: 11px;
    overflow: hidden;
    background: var(--bg-elev-2);
  }
  .club-pic img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .club-meta {
    flex: 1;
    min-width: 0;
  }
  .club-name {
    font-weight: 700;
    font-size: 1rem;
  }
  .club-about {
    font-size: 0.82rem;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-bottom: 0.35rem;
  }
  .club-tags {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    margin-top: 0.15rem;
  }
  .ctag {
    font-size: 0.7rem;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.1rem 0.5rem;
    white-space: nowrap;
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
  .tracks li.dragging {
    opacity: 0.4;
  }
  .tracks li.drop {
    box-shadow: inset 0 2px 0 var(--accent);
  }
  .grip {
    flex: 0 0 auto;
    color: var(--text-dim);
    cursor: grab;
    user-select: none;
    touch-action: none; /* the grip owns touch drags — don't scroll the page */
    padding: 0.2rem 0.1rem;
  }
  .grip:active {
    cursor: grabbing;
  }
  .tracks .play {
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
  .tracks .play:hover {
    border-color: var(--accent);
    filter: brightness(1.15);
  }
  .t-rm {
    flex: 0 0 auto;
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 0.8rem;
  }
  .t-rm:hover {
    color: var(--danger);
  }
  .dnd-hint {
    margin: 0.5rem 0 0;
    font-size: 0.7rem;
    color: var(--text-dim);
  }
  .pls-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.8rem;
    flex-wrap: wrap;
  }
  .new-pl {
    display: flex;
    gap: 0.4rem;
  }
  .new-pl .in {
    width: 11rem;
    max-width: 48vw;
  }
  .pl-actions {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-top: 0.7rem;
  }
  .pl-search {
    margin-top: 0.6rem;
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }
  .pl-search form {
    display: flex;
    gap: 0.4rem;
  }
  .pl-search .in {
    flex: 1;
    min-width: 0;
  }
  .pl-search-err {
    margin: 0;
    font-size: 0.8rem;
    color: var(--text-dim);
  }
  .pl-addall {
    align-self: flex-start;
  }
  .hit {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.4rem 0.6rem;
    cursor: pointer;
    text-align: left;
    color: var(--text);
  }
  .hit:hover {
    border-color: var(--accent);
  }
  .hit-title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.84rem;
  }
  .hit-add {
    flex: 0 0 auto;
    color: var(--accent);
    font-weight: 800;
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

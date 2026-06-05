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
    moveTrackBetween,
    copyTrackTo,
  } from '../nostr/playlists.svelte'
  import { fetchUserClubActivity } from '../nostr/groups'
  import { goClub, goUser, goHowto } from '../router.svelte'
  import { clubAvatar } from '../avatar'
  import { npubEncode } from 'nostr-tools/nip19'
  import { publishMyProfile } from '../nostr/profileEdit'
  import { fetchReceivedZaps, type ReceivedZaps } from '../nostr/zaps.svelte'
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
  const profile = $derived(pubkey ? useProfile(pubkey) : null)

  let othersPlaylists = $state<Playlist[]>([])
  let loading = $state(true)
  let hosting = $state<Club[]>([])
  let djingIn = $state<Club[]>([])
  let received = $state<ReceivedZaps | null>(null)

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
    // Clubs this user hosts / is currently DJing in.
    hosting = []
    djingIn = []
    void fetchUserClubActivity(pk).then((a) => {
      hosting = a.hosting
      djingIn = a.djingIn
    })
    // Zaps received (all-time), grouped by sender.
    received = null
    void fetchReceivedZaps(pk).then((r) => (received = r))
  })

  // ── Profile editor (own profile only) ──────────────────────────────────
  let editing = $state(false)
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

  // Drag-and-drop: reorder within a playlist, or move a track to another playlist.
  let drag = $state<{ plId: string; index: number } | null>(null)
  function onTrackDrop(plId: string, toIndex: number) {
    if (!drag) return
    if (drag.plId === plId) {
      void reorderTrack(plId, drag.index, toIndex)
    } else {
      const from = list.find((p) => p.id === drag!.plId)
      const track = from?.tracks[drag.index]
      if (track) void moveTrackBetween(drag.plId, plId, track.videoId)
    }
    drag = null
  }
  function onMoveCopy(plId: string, track: QueueTrack, sel: HTMLSelectElement) {
    const v = sel.value
    sel.value = ''
    if (!v) return
    const [action, toId] = v.split(':')
    if (action === 'move') void moveTrackBetween(plId, toId, track.videoId)
    else if (action === 'copy')
      void copyTrackTo(toId, { videoId: track.videoId, title: track.title, duration: track.duration })
  }
</script>

<div class="wrap">
  <header class="phead">
    <img class="pavatar" src={avatarUrl(pubkey, profile)} alt="" width="64" height="64" />
    <div class="pinfo">
      <h1>{displayName(pubkey, profile)}</h1>
      <span class="npub">{npub.slice(0, 18)}…</span>
      {#if profile?.about}<p class="pabout">{profile.about}</p>{/if}
      {#if profile?.lud16}
        <p class="plud">⚡ {profile.lud16}</p>
      {:else if isMe}
        <p class="plud missing">⚡ No lightning address yet — add one to receive sats.</p>
      {/if}
    </div>
    {#if isMe}
      <button class="edit-btn" onclick={openEditor} title="Edit profile">✏️ Edit</button>
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

  {#if received && received.total > 0}
    <section class="card zaps-recv">
      <h2>⚡ Received <span class="recv-total">{received.total.toLocaleString()} sats</span>
        <span class="count">from {received.bySender.length} {received.bySender.length === 1 ? 'person' : 'people'}</span>
      </h2>
      <ul class="senders">
        {#each received.bySender.slice(0, 12) as s (s.sender)}
          {@const sp = useProfile(s.sender)}
          <li>
            <img class="av" src={avatarUrl(s.sender, sp)} alt="" width="22" height="22" />
            <a class="who" href={`/user/${npubEncode(s.sender)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(s.sender)) }}>{displayName(s.sender, sp)}</a>
            <span class="s-sats">{s.sats.toLocaleString()} sats</span>
            <span class="s-count">{s.count}×</span>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#snippet clubRow(c: Club, live: boolean)}
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <div class="club-row" class:live role="button" tabindex="0" onclick={() => goClub(c.id)}>
      {#if live}<span class="live-badge">● on stage</span>{/if}
      <div class="club-pic"><img src={c.picture || clubAvatar(c.owner || c.id)} alt="" /></div>
      <div class="club-meta">
        <div class="club-name">{c.name}</div>
        {#if c.about}<div class="club-about">{c.about}</div>{/if}
        <div class="club-tags">
          {#if c.open}<span class="ctag">open</span>{/if}
          {#if c.isPublic}<span class="ctag">public</span>{/if}
          <span class="ctag">👥 {c.memberCount ?? 0} member{(c.memberCount ?? 0) === 1 ? '' : 's'}</span>
        </div>
      </div>
    </div>
  {/snippet}

  {#if djingIn.length}
    <section class="clubs">
      <h2>On stage now <span class="live-dot" aria-hidden="true"></span></h2>
      <div class="club-list">
        {#each djingIn as c (c.id)}{@render clubRow(c, true)}{/each}
      </div>
    </section>
  {/if}

  {#if hosting.length}
    <section class="clubs">
      <h2>Hosting <span class="count">{hosting.length}</span></h2>
      <div class="club-list">
        {#each hosting as c (c.id)}{@render clubRow(c, false)}{/each}
      </div>
    </section>
  {/if}

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
                  {#each pl.tracks as t, ti (t.videoId)}
                    <!-- svelte-ignore a11y_no_static_element_interactions -->
                    <li
                      class:dragging={drag?.plId === pl.id && drag?.index === ti}
                      draggable={isMe}
                      ondragstart={() => isMe && (drag = { plId: pl.id, index: ti })}
                      ondragover={(e) => isMe && e.preventDefault()}
                      ondrop={() => isMe && onTrackDrop(pl.id, ti)}
                      ondragend={() => (drag = null)}
                    >
                      {#if isMe}<span class="grip" aria-hidden="true">⠿</span>{/if}
                      <span class="t-title">{t.title}</span>
                      <span class="dim dur">{fmt(t.duration)}</span>
                      {#if isMe}
                        {#if list.length > 1}
                          <select class="mc" title="Move or copy to another playlist" onchange={(e) => onMoveCopy(pl.id, t, e.currentTarget)}>
                            <option value="">⋯</option>
                            {#each list.filter((p) => p.id !== pl.id) as other (other.id)}
                              <option value="move:{other.id}">→ Move to {other.name}</option>
                              <option value="copy:{other.id}">⎘ Copy to {other.name}</option>
                            {/each}
                          </select>
                        {/if}
                        <button class="t-rm" title="Remove" onclick={() => removeFromPlaylist(pl.id, t.videoId)}>✕</button>
                      {/if}
                    </li>
                  {/each}
                </ol>
                {#if isMe}<p class="dnd-hint">Drag ⠿ to reorder · drop onto another playlist’s tracks to move</p>{/if}
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
    flex: 1;
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
  .recv-total {
    color: var(--amber);
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
  .s-sats {
    color: var(--amber);
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }
  .s-count {
    color: var(--text-dim);
    font-size: 0.74rem;
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
  .live-dot {
    display: inline-block;
    width: 9px;
    height: 9px;
    border-radius: 999px;
    background: var(--accent);
    animation: club-pulse 1.6s ease-in-out infinite;
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
  .tracks li[draggable='true'] {
    cursor: grab;
  }
  .tracks li.dragging {
    opacity: 0.45;
  }
  .grip {
    flex: 0 0 auto;
    color: var(--text-dim);
    cursor: grab;
    user-select: none;
  }
  .mc {
    flex: 0 0 auto;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 6px;
    font-size: 0.72rem;
    padding: 0.1rem 0.2rem;
    max-width: 5.5rem;
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

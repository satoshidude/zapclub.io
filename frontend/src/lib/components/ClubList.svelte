<script lang="ts">
  import { listClubs, createClub, joinClub, type MyClub } from '../nostr/groups'
  import { fetchMyClubs } from '../nostr/groups'
  import { goClub } from '../router.svelte'
  import { auth } from '../nostr/auth.svelte'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import { persistedStageGroup } from '../nostr/stage.svelte'
  import { clubAvatar } from '../avatar'
  import { fetchTopLikes, type TopTrack } from '../nostr/likes.svelte'
  import type { Club } from '../nostr/types'

  let clubs = $state<Club[]>([])
  let myClubs = $state<MyClub[]>([])
  let loading = $state(true)
  let error = $state('')

  // Create-club form
  let showCreate = $state(false)
  let name = $state('')
  let about = $state('')
  let creating = $state(false)

  const myIds = $derived(new Set(myClubs.map((c) => c.id)))

  // The club the user is currently DJing in → pin to the top + highlight.
  const onStageClub = persistedStageGroup()
  const displayClubs = $derived.by(() => {
    if (!onStageClub) return clubs
    const top = clubs.filter((c) => c.id === onStageClub)
    const rest = clubs.filter((c) => c.id !== onStageClub)
    return [...top, ...rest]
  })

  let topTracks = $state<TopTrack[]>([])

  async function load() {
    loading = true
    error = ''
    try {
      clubs = await listClubs()
      if (auth.pubkey) myClubs = await fetchMyClubs(auth.pubkey)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      loading = false
    }
    void fetchTopLikes(8).then((t) => (topTracks = t))
  }

  async function create() {
    if (!name.trim()) return
    creating = true
    error = ''
    try {
      const id = await createClub({ name: name.trim(), about: about.trim() || undefined })
      // Creator is auto-admin + member; jump straight into the new club.
      name = ''
      about = ''
      showCreate = false
      goClub(id)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      creating = false
    }
  }

  async function join(id: string, ev: MouseEvent) {
    ev.stopPropagation()
    try {
      await joinClub(id)
      await load()
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    }
  }

  $effect(() => {
    load()
  })
</script>

<div class="wrap">
  <div class="head">
    <h2>Clubs</h2>
    {#if auth.canSign}
      <button class="btn btn-primary btn-sm" onclick={() => (showCreate = !showCreate)}>
        {showCreate ? 'Cancel' : '+ New club'}
      </button>
    {/if}
  </div>

  {#if showCreate}
    <div class="create card">
      <div class="field">
        <label for="club-name">Club name</label>
        <input id="club-name" bind:value={name} placeholder="e.g. Midnight Synthwave" maxlength="60" />
      </div>
      <div class="field">
        <label for="club-about">About (optional)</label>
        <textarea id="club-about" bind:value={about} rows="2" placeholder="What's this club about?" maxlength="280"></textarea>
      </div>
      <button class="btn btn-primary" onclick={create} disabled={creating || !name.trim()}>
        {creating ? 'Creating…' : 'Create club'}
      </button>
    </div>
  {/if}

  {#if error}<p class="err">⚠ {error}</p>{/if}

  {#if topTracks.length}
    <section class="top">
      <h3>🔥 Top tracks</h3>
      <ol class="top-list">
        {#each topTracks as t, i (t.videoId)}
          <li>
            <span class="rank">{i + 1}</span>
            <a class="tt-thumb" href={`https://youtu.be/${t.videoId}`} target="_blank" rel="noopener noreferrer" title="Open on YouTube">
              <img src={`https://i.ytimg.com/vi/${t.videoId}/default.jpg`} alt="" loading="lazy" />
            </a>
            <div class="tt-meta">
              <div class="tt-title">{t.title}</div>
              {#if t.clubId}
                <button class="tt-club" onclick={() => goClub(t.clubId)}>played in {t.clubName || 'a club'}</button>
              {/if}
            </div>
            <span class="tt-likes">🔥 {t.likes}</span>
          </li>
        {/each}
      </ol>
    </section>
  {/if}

  {#if loading}
    <p class="dim">Loading clubs…</p>
  {:else if clubs.length === 0}
    <p class="dim">No clubs yet. {auth.canSign ? 'Be the first to create one.' : 'Sign in to create one.'}</p>
  {:else}
    <div class="list">
      {#each displayClubs as club (club.id)}
        <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
        <div class="card row" class:onstage={club.id === onStageClub} role="button" tabindex="0" onclick={() => goClub(club.id)}>
          {#if club.id === onStageClub}<span class="live-badge">● on stage</span>{/if}
          <div class="pic">
            <img class="pic-img" src={club.picture || clubAvatar(club.owner || club.id)} alt="" />
          </div>
          <div class="meta">
            <div class="name">{club.name}</div>
            {#if club.about}<div class="about">{club.about}</div>{/if}
            <div class="tags">
              {#if club.open}<span class="tag">open</span>{/if}
              {#if club.isPublic}<span class="tag">public</span>{/if}
              <span class="tag">👥 {club.memberCount} member{club.memberCount === 1 ? '' : 's'}</span>
            </div>
            {#if club.owner}
              {@const ownerProfile = useProfile(club.owner)}
              <div class="host">
                <img class="host-avatar" src={avatarUrl(club.owner, ownerProfile)} alt="" width="18" height="18" />
                <span>Hosted by {displayName(club.owner, ownerProfile)}</span>
              </div>
            {/if}
          </div>
          {#if auth.canSign && !myIds.has(club.id)}
            <button class="btn btn-ghost btn-sm join" onclick={(e) => join(club.id, e)}>Join</button>
          {:else if myIds.has(club.id)}
            <span class="badge-in">Member</span>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .wrap {
    max-width: 680px;
    margin: 0 auto;
    padding: 1.2rem 1rem 4rem;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;
  }
  h2 {
    margin: 0;
    font-size: 1.3rem;
  }
  .card {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1rem;
  }
  .create {
    margin-bottom: 1.2rem;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
  }
  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 0.9rem;
    cursor: pointer;
    transition: border-color 0.15s ease, transform 0.08s ease;
  }
  .row:hover {
    border-color: var(--accent-2);
  }
  .row:active {
    transform: translateY(1px);
  }
  /* The club the user is DJing in: pinned to the top, pulsing green. */
  .row.onstage {
    border-color: var(--accent);
    animation: club-pulse 1.6s ease-in-out infinite;
  }
  @keyframes club-pulse {
    0%,
    100% {
      box-shadow: 0 0 0 1px var(--accent), 0 0 8px rgba(74, 222, 94, 0.25);
    }
    50% {
      box-shadow: 0 0 0 1px var(--accent), 0 0 20px rgba(74, 222, 94, 0.6);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .row.onstage {
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
  .pic {
    width: 52px;
    height: 52px;
    flex: 0 0 52px;
    border-radius: 11px;
    overflow: hidden;
    background: var(--bg-elev-2);
  }
  .pic-img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .meta {
    flex: 1;
    min-width: 0;
  }
  .name {
    font-weight: 700;
    font-size: 1rem;
  }
  .about {
    font-size: 0.82rem;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-bottom: 0.35rem;
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    margin-top: 0.15rem;
  }
  .tag {
    font-size: 0.7rem;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.1rem 0.5rem;
    white-space: nowrap;
  }
  .host {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    margin-top: 0.4rem;
    font-size: 0.74rem;
    color: var(--text-dim);
  }
  .host-avatar {
    width: 18px;
    height: 18px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
  }
  .join {
    flex: 0 0 auto;
  }
  .badge-in {
    flex: 0 0 auto;
    font-size: 0.72rem;
    color: var(--accent);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.2rem 0.6rem;
  }
  .dim {
    color: var(--text-dim);
  }
  .err {
    color: var(--danger);
    font-size: 0.85rem;
  }
  .top {
    margin-bottom: 1.4rem;
  }
  .top h3 {
    margin: 0 0 0.6rem;
    font-size: 1.05rem;
  }
  .top-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .top-list li {
    display: flex;
    align-items: center;
    gap: 0.7rem;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.5rem 0.7rem;
  }
  .rank {
    flex: 0 0 auto;
    width: 1.4rem;
    text-align: center;
    font-weight: 800;
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
  }
  .tt-thumb {
    flex: 0 0 auto;
    width: 56px;
    height: 38px;
    border-radius: 6px;
    overflow: hidden;
    background: #000;
  }
  .tt-thumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .tt-meta {
    flex: 1;
    min-width: 0;
  }
  .tt-title {
    font-weight: 600;
    font-size: 0.9rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tt-club {
    background: none;
    border: none;
    padding: 0;
    color: var(--accent);
    font-size: 0.76rem;
    cursor: pointer;
  }
  .tt-club:hover {
    text-decoration: underline;
  }
  .tt-likes {
    flex: 0 0 auto;
    font-size: 0.82rem;
    font-weight: 700;
    color: var(--amber);
  }
</style>

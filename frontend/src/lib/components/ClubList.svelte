<script lang="ts">
  import { listClubs, joinClub, fetchLiveClubIds, subscribeClubPresence, type MyClub } from '../nostr/groups'
  import { fetchMyClubs } from '../nostr/groups'
  import { goClub, goUser, goLeaderboard } from '../router.svelte'
  import { npubEncode } from 'nostr-tools/nip19'
  import { auth } from '../nostr/auth.svelte'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import { persistedStageGroup } from '../nostr/stage.svelte'
  import { clubAvatar } from '../avatar'
  import type { Club } from '../nostr/types'

  import { fetchLeaderboard, type LeaderboardEntry } from '../nostr/leaderboard'

  let clubs = $state<Club[]>([])
  let myClubs = $state<MyClub[]>([])
  let liveClubIds = $state<Set<string>>(new Set())
  let loading = $state(true)
  let error = $state('')
  let lbEntries = $state<LeaderboardEntry[]>([])

  // Per-club presence: clubId → pubkey → last beat ms
  const ONLINE_MS = 50_000
  let clubBeats = $state<Record<string, Record<string, number>>>({})
  let onlineTick = $state(Date.now())
  const onlineCounts = $derived.by(() => {
    void onlineTick
    const now = Date.now()
    const out: Record<string, number> = {}
    for (const [id, byPk] of Object.entries(clubBeats)) {
      out[id] = Object.values(byPk).filter((ms) => now - ms < ONLINE_MS).length
    }
    return out
  })

  const myIds = $derived(new Set(myClubs.map((c) => c.id)))
  let showAllClubs = $state(false)

  // The club the user is currently DJing in → pin to the top + highlight.
  const onStageClub = persistedStageGroup()
  const sortedClubs = $derived.by(() => {
    const byMembers = [...clubs].sort((a, b) => (b.memberCount ?? 0) - (a.memberCount ?? 0))
    // 1. own DJ club, 2. clubs with active stage, 3. rest — each group sorted by members
    const myStage = byMembers.filter((c) => c.id === onStageClub)
    const live    = byMembers.filter((c) => c.id !== onStageClub && liveClubIds.has(c.id))
    const rest    = byMembers.filter((c) => c.id !== onStageClub && !liveClubIds.has(c.id))
    return [...myStage, ...live, ...rest]
  })
  const displayClubs = $derived(showAllClubs ? sortedClubs : sortedClubs.slice(0, 3))

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
    const ids = clubs.map((c) => c.id)
    void fetchLiveClubIds(ids).then((s) => (liveClubIds = s))
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

  $effect(() => {
    void fetchLeaderboard().then((r) => (lbEntries = r.top.slice(0, 5)))
  })

  // Subscribe to presence beats for all clubs; refresh online counts every 15 s.
  $effect(() => {
    const ids = clubs.map((c) => c.id)
    const unsub = subscribeClubPresence(ids, (clubId, pubkey, ms) => {
      const prev = clubBeats[clubId] ?? {}
      if (ms > (prev[pubkey] ?? 0)) {
        clubBeats = { ...clubBeats, [clubId]: { ...prev, [pubkey]: ms } }
      }
    })
    const tick = setInterval(() => { onlineTick = Date.now() }, 15_000)
    return () => { unsub(); clearInterval(tick) }
  })
</script>

<div class="wrap">
  <header class="hero">
    <p class="eyebrow">Collaborative · Decentralized · Rewarding</p>
    <h1 class="hero-title">Drop in. Take the stage.<br />Own the night.</h1>
    <p class="hero-sub">
      zapclub is one turntable, shared. Fill your playlists, take the deck, pass it on. The room
      rides every transition with you. Drop in with a key, not an email. Tip the DJ in sats,
      not likes. Just you, playlists and the crowd.
    </p>
    <div class="chips">
      <span class="chip">🎛️ Pass the deck</span>
      <span class="chip">⚡ Zap the DJ</span>
      <span class="chip">🔑 Key in, no signup</span>
      <span class="chip">👥 Crowd-owned</span>
    </div>
  </header>

  <div class="head">
    <h2>Clubs</h2>
  </div>

  {#if error}<p class="err">⚠ {error}</p>{/if}

  {#if loading}
    <p class="dim">Loading clubs…</p>
  {:else if clubs.length === 0}
    <p class="dim">No clubs yet. {auth.canSign ? 'Be the first to create one.' : 'Sign in to create one.'}</p>
  {:else}
    <div class="list">
      {#each displayClubs as club (club.id)}
        <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
        <div class="card row" class:onstage={liveClubIds.has(club.id) || club.id === onStageClub} role="button" tabindex="0" onclick={() => goClub(club.id)}>
          {#if liveClubIds.has(club.id) || club.id === onStageClub}<span class="live-badge">● on stage</span>{/if}
          <div class="pic">
            <img class="pic-img" src={club.picture || clubAvatar(club.owner || club.id)} alt="" />
          </div>
          <div class="meta">
            <div class="name">{club.name}</div>
            {#if club.about}<div class="about">{club.about}</div>{/if}
            <div class="tags">
              <span class="tag">👥 {club.memberCount} member{club.memberCount === 1 ? '' : 's'}</span>
              {#if (onlineCounts[club.id] ?? 0) > 0}
                <span class="tag online">● {onlineCounts[club.id]} online</span>
              {/if}
              {#if club.access === 'paid'}<span class="tag paid">🔒 {club.price} sats</span>{/if}
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
    {#if clubs.length > 3}
      <button class="all-clubs-link" onclick={() => (showAllClubs = !showAllClubs)}>
        {showAllClubs ? '↑ Show less' : `All clubs (${clubs.length}) →`}
      </button>
    {/if}
  {/if}

  {#if lbEntries.length > 0}
    <section class="lb-preview">
      <div class="lb-head">
        <h2>⚡ Top DJs</h2>
        <button class="lb-all" onclick={goLeaderboard}>Full leaderboard →</button>
      </div>

      {#if lbEntries.length >= 3}
        <div class="lb-podium">
          {#each [lbEntries[1], lbEntries[0], lbEntries[2]] as e (e.pubkey)}
            {@const p = useProfile(e.pubkey)}
            {@const npub = npubEncode(e.pubkey)}
            {@const isFirst = e.rank === 1}
            <button class="lb-pod" class:lb-pod-first={isFirst} onclick={() => goUser(npub)}>
              <span class="lb-pod-medal">{e.rank === 1 ? '🥇' : e.rank === 2 ? '🥈' : '🥉'}</span>
              <img class="lb-pod-av" src={avatarUrl(e.pubkey, p)} alt=""
                width={isFirst ? 52 : 40} height={isFirst ? 52 : 40} />
              <span class="lb-pod-name">{displayName(e.pubkey, p)}</span>
              <span class="lb-pod-sats">⚡ {e.sats.toLocaleString()}</span>
            </button>
          {/each}
        </div>
      {/if}

      {#if lbEntries.length > 3}
        <ol class="lb-list">
          {#each lbEntries.slice(3, 5) as e (e.pubkey)}
            {@const p = useProfile(e.pubkey)}
            {@const npub = npubEncode(e.pubkey)}
            <li>
              <a class="lb-row" href={`/user/${npub}`} onclick={(ev) => { ev.preventDefault(); goUser(npub) }}>
                <span class="lb-rank"><span class="lb-num">#{e.rank}</span></span>
                <img class="lb-av" src={avatarUrl(e.pubkey, p)} alt="" width="40" height="40" />
                <span class="lb-name">{displayName(e.pubkey, p)}</span>
                <span class="lb-stats">
                  <span class="lb-sats">⚡ {e.sats.toLocaleString()}</span>
                  <span class="lb-from">from {e.zappers.toLocaleString()} {e.zappers === 1 ? 'person' : 'people'}</span>
                </span>
              </a>
            </li>
          {/each}
        </ol>
      {/if}
    </section>
  {/if}

  <a class="tg-block" href="https://t.me/zapclub_io" target="_blank" rel="noopener noreferrer">
    <svg class="tg-block-icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 0C5.373 0 0 5.373 0 12s5.373 12 12 12 12-5.373 12-12S18.627 0 12 0zm5.894 8.221-1.97 9.28c-.145.658-.537.818-1.084.508l-3-2.21-1.447 1.394c-.16.16-.295.295-.605.295l.213-3.053 5.56-5.023c.242-.213-.054-.333-.373-.12L8.32 13.617l-2.96-.924c-.643-.204-.657-.643.136-.953l11.57-4.461c.537-.194 1.006.131.828.942z"/></svg>
    <span class="tg-block-text">
      <strong>Join our Telegram</strong>
      <span>Chat, feedback &amp; updates</span>
    </span>
    <span class="tg-block-arrow">→</span>
  </a>
</div>

<style>
  .wrap {
    max-width: 680px;
    margin: 0 auto;
    padding: 1.2rem 1rem 4rem;
  }
  /* ── Home hero ─────────────────────────────────────────────────────────── */
  .hero {
    position: relative;
    overflow: hidden;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    margin-bottom: 1.6rem;
    padding: 1.9rem 1.5rem 1.7rem;
    background:
      radial-gradient(130% 150% at 0% 0%, color-mix(in srgb, var(--accent) 26%, transparent), transparent 55%),
      radial-gradient(130% 150% at 100% 8%, color-mix(in srgb, var(--accent-2) 20%, transparent), transparent 55%),
      var(--bg-elev);
  }
  .eyebrow {
    margin: 0 0 0.55rem;
    font-size: 0.72rem;
    letter-spacing: 0.16em;
    text-transform: uppercase;
    color: var(--accent);
    font-weight: 800;
  }
  .hero-title {
    margin: 0 0 0.75rem;
    font-size: clamp(1.7rem, 5vw, 2.5rem);
    line-height: 1.08;
    font-weight: 800;
    letter-spacing: -0.015em;
  }
  .hero-sub {
    margin: 0 0 1.1rem;
    max-width: 54ch;
    color: var(--text-dim);
    font-size: 0.95rem;
    line-height: 1.55;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem;
    margin-bottom: 1.2rem;
  }
  .chip {
    font-size: 0.76rem;
    padding: 0.32rem 0.62rem;
    border-radius: 999px;
    background: color-mix(in srgb, var(--bg) 55%, transparent);
    border: 1px solid var(--border);
    color: var(--text);
    white-space: nowrap;
  }
  .all-clubs-link {
    display: block;
    width: 100%;
    margin-top: 0.7rem;
    padding: 0.45rem 0;
    background: none;
    border: none;
    color: var(--accent-2);
    font-size: 0.85rem;
    font-weight: 700;
    cursor: pointer;
    text-align: center;
    letter-spacing: 0.01em;
  }
  .all-clubs-link:hover { text-decoration: underline; }
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
  .tag.paid {
    color: var(--amber);
    border-color: var(--amber);
    font-weight: 700;
  }
  .tag.online {
    color: #4ade80;
    border-color: #4ade80;
    font-weight: 600;
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
  /* ── Top DJs leaderboard preview ───────────────────────────────────────── */
  .lb-preview {
    margin-top: 2rem;
  }
  .lb-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.75rem;
  }
  .lb-head h2 {
    margin: 0;
    font-size: 1.3rem;
  }
  .lb-all {
    background: none;
    border: none;
    color: var(--accent-2);
    font-size: 0.85rem;
    font-weight: 700;
    cursor: pointer;
    padding: 0;
  }
  .lb-all:hover { text-decoration: underline; }
  /* Podium */
  .lb-podium {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 0.5rem;
    margin-bottom: 0.5rem;
    align-items: end;
  }
  .lb-pod {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.3rem;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.8rem 0.5rem 0.7rem;
    cursor: pointer;
    color: var(--text);
    transition: border-color 0.15s ease, transform 0.08s ease;
    text-align: center;
  }
  .lb-pod:hover { border-color: var(--accent-2); }
  .lb-pod:active { transform: translateY(1px); }
  .lb-pod.lb-pod-first {
    border-color: color-mix(in srgb, var(--amber) 55%, var(--border));
    background: radial-gradient(120% 140% at 50% 0%, rgba(245,166,35,0.13) 0%, transparent 65%), var(--bg-elev);
    padding-top: 1.1rem;
    padding-bottom: 0.9rem;
  }
  .lb-pod-medal { font-size: 1.2rem; line-height: 1; }
  .lb-pod-first .lb-pod-medal { font-size: 1.5rem; }
  .lb-pod-av {
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    border: 2px solid var(--border);
  }
  .lb-pod-first .lb-pod-av { border-color: var(--amber); }
  .lb-pod-name {
    font-weight: 700;
    font-size: 0.8rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }
  .lb-pod-first .lb-pod-name { font-size: 0.95rem; }
  .lb-pod-sats {
    color: var(--amber);
    font-weight: 800;
    font-size: 0.76rem;
    font-variant-numeric: tabular-nums;
  }
  .lb-pod-first .lb-pod-sats { font-size: 0.88rem; }
  /* List rows #4–5 (same style as leaderboard full list) */
  .lb-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .lb-row {
    display: flex;
    align-items: center;
    gap: 0.8rem;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.7rem 0.9rem;
    cursor: pointer;
    color: var(--text);
    text-decoration: none;
    transition: border-color 0.15s ease, transform 0.08s ease;
  }
  .lb-row:hover { border-color: var(--accent-2); }
  .lb-row:active { transform: translateY(1px); }
  .lb-rank {
    flex: 0 0 auto;
    min-width: 3.2rem;
    font-size: 1.1rem;
  }
  .lb-num {
    font-weight: 800;
    font-variant-numeric: tabular-nums;
    color: var(--text-dim);
    font-size: 0.95rem;
  }
  .lb-av {
    flex: 0 0 auto;
    width: 40px;
    height: 40px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
  }
  .lb-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 700;
  }
  .lb-stats {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 0.1rem;
  }
  .lb-sats {
    color: var(--amber);
    font-weight: 800;
    font-variant-numeric: tabular-nums;
  }
  .lb-from {
    color: var(--text-dim);
    font-size: 0.72rem;
  }

  /* ── Telegram block ─────────────────────────────────────────────────────── */
  .tg-block {
    display: flex;
    align-items: center;
    gap: 0.9rem;
    margin-top: 1.5rem;
    padding: 1rem 1.1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    color: var(--text);
    text-decoration: none;
    transition: border-color 0.15s;
  }
  .tg-block:hover {
    border-color: #2ca5e0;
  }
  .tg-block-icon {
    width: 2rem;
    height: 2rem;
    color: #2ca5e0;
    flex-shrink: 0;
  }
  .tg-block-text {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    flex: 1;
  }
  .tg-block-text strong {
    font-size: 0.95rem;
    font-weight: 700;
  }
  .tg-block-text span {
    font-size: 0.78rem;
    color: var(--text-dim);
  }
  .tg-block-arrow {
    color: var(--text-dim);
    font-size: 1rem;
  }
</style>

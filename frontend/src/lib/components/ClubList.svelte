<script lang="ts">
  import { listClubs, fetchOnAirClubDjs, fetchOnStageClubDjs, subscribeClubPresence, type MyClub } from '../nostr/groups'
  import { fetchMyClubs } from '../nostr/groups'
  import { goClub, goUser, goLeaderboard } from '../router.svelte'
  import { npubEncode } from 'nostr-tools/nip19'
  import { auth } from '../nostr/auth.svelte'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import { persistedStageGroup } from '../nostr/stage.svelte'
  import { clubAvatar } from '../avatar'
  import ZapButton from './club/ZapButton.svelte'
  import { findClubSuggestions } from './clubSearch'
  import type { Club } from '../nostr/types'

  import { fetchLeaderboard, type LeaderboardEntry } from '../nostr/leaderboard'

  const TELEGRAM_BOT_CLUB_ID = 'c7ca6a16dd1ed946'

  let clubs = $state<Club[]>([])
  let myClubs = $state<MyClub[]>([])
  let onAirDjs = $state<Map<string, string>>(new Map())
  let onStageDjs = $state<Map<string, string>>(new Map())
  let loading = $state(true)
  let error = $state('')
  let lbEntries = $state<LeaderboardEntry[]>([])
  let loadVersion = 0

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
  const directoryClubs = $derived(
    clubs.filter((club) => club.id !== TELEGRAM_BOT_CLUB_ID && onAirDjs.has(club.id)),
  )
  const telegramBotClub = $derived(clubs.find((club) => club.id === TELEGRAM_BOT_CLUB_ID) ?? null)
  const searchableClubs = $derived(clubs.filter((club) => club.id !== TELEGRAM_BOT_CLUB_ID))
  let clubQuery = $state('')
  let searchOpen = $state(false)
  let activeSuggestion = $state(-1)
  const clubSuggestions = $derived(findClubSuggestions(searchableClubs, clubQuery))
  const showSuggestions = $derived(searchOpen && clubQuery.trim().length > 0)
  let showAllClubs = $state(false)

  // The club the user is currently DJing in → pin to the top + highlight.
  const onStageClub = persistedStageGroup()
  const sortedClubs = $derived.by(() => {
    const byMembers = [...directoryClubs].sort((a, b) => (b.memberCount ?? 0) - (a.memberCount ?? 0))
    // 1. own DJ club, 2. clubs with active stage, 3. rest — each group sorted by members
    const myStage = byMembers.filter((c) => c.id === onStageClub)
    const rest = byMembers.filter((c) => c.id !== onStageClub)
    return [...myStage, ...rest]
  })
  const displayClubs = $derived(showAllClubs ? sortedClubs : sortedClubs.slice(0, 3))

  function selectClubSuggestion(clubId: string) {
    clubQuery = ''
    searchOpen = false
    activeSuggestion = -1
    goClub(clubId)
  }

  function handleSearchInput() {
    searchOpen = true
    activeSuggestion = clubQuery.trim() ? 0 : -1
  }

  function handleSearchKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      searchOpen = false
      activeSuggestion = -1
      return
    }
    if (!clubSuggestions.length) return
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      searchOpen = true
      activeSuggestion = (activeSuggestion + 1) % clubSuggestions.length
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      searchOpen = true
      activeSuggestion = (activeSuggestion - 1 + clubSuggestions.length) % clubSuggestions.length
    } else if (event.key === 'Enter' && searchOpen && activeSuggestion >= 0) {
      event.preventDefault()
      const club = clubSuggestions[activeSuggestion]
      if (club) selectClubSuggestion(club.id)
    }
  }

  function handleSearchFocusOut(event: FocusEvent) {
    const next = event.relatedTarget
    if (!(next instanceof Node) || !(event.currentTarget as HTMLElement).contains(next)) {
      searchOpen = false
      activeSuggestion = -1
    }
  }

  async function load(pubkey: string | null) {
    const version = ++loadVersion
    loading = true
    error = ''
    try {
      const [loadedClubs, loadedMine] = await Promise.all([
        listClubs(),
        pubkey ? fetchMyClubs(pubkey) : Promise.resolve([]),
      ])
      const clubIds = loadedClubs.map((club) => club.id)
      const [loadedOnAirDjs, loadedOnStageDjs] = await Promise.all([
        fetchOnAirClubDjs(clubIds),
        fetchOnStageClubDjs([TELEGRAM_BOT_CLUB_ID]),
      ])
      if (version !== loadVersion) return
      clubs = loadedClubs
      myClubs = loadedMine
      onAirDjs = loadedOnAirDjs
      onStageDjs = loadedOnStageDjs
    } catch (e) {
      if (version !== loadVersion) return
      error = String((e as Error)?.message ?? e)
    } finally {
      if (version === loadVersion) loading = false
    }
  }

  $effect(() => {
    const pubkey = auth.pubkey
    void load(pubkey)
  })

  // Keep the public directory live after the initial snapshot. A club that
  // starts or stops broadcasting should appear/disappear without a reload.
  $effect(() => {
    const clubIds = clubs.map((club) => club.id)
    if (clubIds.length === 0) return
    let cancelled = false
    const timer = setInterval(() => {
      void fetchOnAirClubDjs(clubIds).then((next) => {
        if (!cancelled) onAirDjs = next
      }).catch(() => {
        // Preserve the last good snapshot through a transient relay failure.
      })
    }, 15_000)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  })

  $effect(() => {
    void fetchLeaderboard().then((r) => (lbEntries = r.top.slice(0, 5)))
  })

  // Presence contains member identities. Subscribe only to clubs the signed-in
  // account belongs to; guests never request or receive this social data.
  $effect(() => {
    const ids = myClubs.map((c) => c.id)
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
  <header class="hero led-zone">
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

  <section class="clubs-panel led-zone">
    <div class="head">
      <h2 class="lcd-card-title">Clubs</h2>
    </div>

    <div class="club-search" onfocusout={handleSearchFocusOut}>
      <div class="search-line">
        <svg class="search-icon" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <circle cx="11" cy="11" r="6.5"></circle>
          <path d="m16 16 4 4"></path>
        </svg>
        <input
          class="club-search-input"
          type="search"
          placeholder="Search clubs…"
          aria-label="Search clubs"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={showSuggestions}
          aria-controls="club-search-suggestions"
          aria-activedescendant={activeSuggestion >= 0 && clubSuggestions[activeSuggestion] ? `club-suggestion-${clubSuggestions[activeSuggestion].id}` : undefined}
          autocomplete="off"
          bind:value={clubQuery}
          oninput={handleSearchInput}
          onfocus={() => { searchOpen = true; activeSuggestion = clubSuggestions.length ? 0 : -1 }}
          onkeydown={handleSearchKeydown}
        />
      </div>

      {#if showSuggestions}
        <div class="search-suggestions" id="club-search-suggestions" role="listbox" aria-label="Club suggestions">
          {#each clubSuggestions as club, index (club.id)}
            <button
              class="search-suggestion"
              class:active={index === activeSuggestion}
              id={`club-suggestion-${club.id}`}
              role="option"
              aria-selected={index === activeSuggestion}
              onpointerdown={(event) => {
                if (event.button !== 0) return
                event.preventDefault()
                selectClubSuggestion(club.id)
              }}
              onclick={() => selectClubSuggestion(club.id)}
              onmouseenter={() => (activeSuggestion = index)}
            >
              <img src={club.picture || clubAvatar(club.owner || club.id)} alt="" width="38" height="38" />
              <span class="suggestion-copy">
                <strong>{club.name}</strong>
                {#if club.about}<span>{club.about}</span>{/if}
              </span>
              <span class:on-air={onAirDjs.has(club.id)} class="suggestion-state">
                {onAirDjs.has(club.id) ? 'ON AIR' : 'ENTER CLUB'}
              </span>
            </button>
          {:else}
            <p class="search-empty">No clubs found.</p>
          {/each}
        </div>
      {/if}
    </div>

    {#if error}<p class="err">⚠ {error}</p>{/if}

    {#if loading}
      <p class="dim">Loading clubs…</p>
    {:else if directoryClubs.length === 0}
      <p class="dim">No DJ is on air right now.</p>
    {:else}
      <ul class="list">
        {#each displayClubs as club (club.id)}
          {@const liveDj = onAirDjs.get(club.id)}
          <li>
            <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
            <div class="row club-directory-row" class:onstage={!!liveDj} role="button" tabindex="0" onclick={() => goClub(club.id)}>
            <div class="pic">
              <img class="pic-img" src={club.picture || clubAvatar(club.owner || club.id)} alt="" />
            </div>
            <div class="meta">
              <div class="name-line">
                <div class="name">{club.name}</div>
                {#if myIds.has(club.id)}<span class="badge-in">Member</span>{/if}
              </div>
              {#if club.about}<div class="about">{club.about}</div>{/if}
              <div class="tags">
                {#if club.memberCount != null}
                  <span class="tag">👥 {club.memberCount} member{club.memberCount === 1 ? '' : 's'}</span>
                {/if}
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
            {#if liveDj}
              {@const liveDjProfile = useProfile(liveDj)}
              <span class="dj-status club-dj-status">
                <span class="club-dj-name">{displayName(liveDj, liveDjProfile)}</span>
                <span class="live-label">ON AIR</span>
              </span>
            {/if}
            <button class="enter-club" onclick={(event) => { event.stopPropagation(); goClub(club.id) }}>Enter club</button>
            </div>
          </li>
        {/each}
      </ul>
      {#if directoryClubs.length > 3}
        <button class="all-clubs-link" onclick={() => (showAllClubs = !showAllClubs)}>
          {showAllClubs ? '↑ Show less' : `All clubs (${directoryClubs.length}) →`}
        </button>
      {/if}
    {/if}
  </section>

  <section class="tg-block led-zone" aria-labelledby="telegram-club-title">
    <div class="head">
      <h2 class="lcd-card-title" id="telegram-club-title">Telegram Club Bot</h2>
    </div>

    {#if loading}
      <p class="dim">Loading club…</p>
    {:else if telegramBotClub}
      {@const botOnAirDj = onAirDjs.get(TELEGRAM_BOT_CLUB_ID)}
      {@const botStageDj = onStageDjs.get(TELEGRAM_BOT_CLUB_ID)}
      {@const botStatusDj = botOnAirDj || botStageDj}
      <ul class="list">
        <li>
          <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
          <div class="row telegram-club-row" class:onstage={!!botStatusDj} role="button" tabindex="0" onclick={() => goClub(TELEGRAM_BOT_CLUB_ID)}>
            <div class="pic">
              <img class="pic-img" src={telegramBotClub.picture || clubAvatar(telegramBotClub.owner || telegramBotClub.id)} alt="" />
            </div>
            <div class="meta">
              <div class="name-line">
                <div class="name">{telegramBotClub.name}</div>
                {#if myIds.has(TELEGRAM_BOT_CLUB_ID)}<span class="badge-in">Member</span>{/if}
              </div>
              <div class="about">{telegramBotClub.about || 'Add tracks from Telegram and listen together.'}</div>
              {#if telegramBotClub.owner}
                {@const ownerProfile = useProfile(telegramBotClub.owner)}
                <div class="host">
                  <img class="host-avatar" src={avatarUrl(telegramBotClub.owner, ownerProfile)} alt="" width="18" height="18" />
                  <span>Hosted by {displayName(telegramBotClub.owner, ownerProfile)}</span>
                </div>
              {/if}
            </div>
            {#if botStatusDj}
              <span class="dj-status">
                <ZapButton pubkey={botStatusDj} club={TELEGRAM_BOT_CLUB_ID} iconOnly={true} showName={true} showSelf={true} />
                <span class:live-label={!!botOnAirDj} class:stage-label={!botOnAirDj}>{botOnAirDj ? 'ON AIR' : 'ON STAGE'}</span>
              </span>
            {/if}
            {#if auth.canSign && !myIds.has(TELEGRAM_BOT_CLUB_ID)}
              <button class="enter-club" onclick={(event) => { event.stopPropagation(); goClub(TELEGRAM_BOT_CLUB_ID) }}>Enter club</button>
            {/if}
          </div>
        </li>
      </ul>
    {:else}
      <p class="dim">Telegram Bot Club is unavailable.</p>
    {/if}
  </section>

  {#if lbEntries.length > 0}
    <section class="lb-preview led-zone">
      <div class="lb-head">
        <h2 class="lcd-card-title">⚡ Top DJs</h2>
        <a class="lb-all" href="/leaderboard" onclick={(event) => { event.preventDefault(); goLeaderboard() }}>FULL LEADERBOARD</a>
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
  }
  .club-search {
    position: relative;
    z-index: 12;
    margin: 0 0 0.75rem;
  }
  .search-line {
    display: flex;
    align-items: center;
    gap: 0.65rem;
    min-height: 42px;
    border-bottom: 1px solid rgba(241, 243, 244, 0.2);
    background: rgba(0, 0, 0, 0.16);
    transition: border-color 0.15s ease, background-color 0.15s ease;
  }
  .search-line:focus-within {
    border-bottom-color: var(--accent);
    background: rgba(0, 0, 0, 0.24);
  }
  .search-icon {
    width: 22px;
    height: 22px;
    flex: 0 0 auto;
    margin-left: 0.2rem;
    stroke: var(--lcd-text-bright);
    stroke-width: 1.8;
    stroke-linecap: round;
  }
  .club-search-input {
    width: 100%;
    min-width: 0;
    padding: 0.55rem 0.3rem 0.55rem 0;
    border: 0;
    border-radius: 0;
    outline: 0;
    color: var(--lcd-text-bright);
    background: transparent;
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 0.92rem;
    letter-spacing: 0.03em;
    text-shadow: var(--lcd-text-shadow);
  }
  .club-search-input::placeholder {
    color: var(--lcd-text-soft);
    opacity: 0.9;
  }
  .club-search-input::-webkit-search-cancel-button {
    filter: grayscale(1) invert(1);
    opacity: 0.72;
  }
  .search-suggestions {
    position: absolute;
    inset: calc(100% + 1px) 0 auto;
    z-index: 30;
    background:
      repeating-linear-gradient(180deg, rgba(0, 0, 0, 0.36) 0 1px, transparent 1px 3px),
      color-mix(in srgb, var(--card-led-a, #111820) 15%, #050708 85%);
    box-shadow: 0 16px 32px rgba(0, 0, 0, 0.52);
  }
  .search-suggestion {
    display: grid;
    grid-template-columns: 38px minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.75rem;
    width: 100%;
    min-height: 58px;
    padding: 0.55rem 0.35rem;
    border: 0;
    border-bottom: 1px solid rgba(241, 243, 244, 0.12);
    border-radius: 0;
    color: var(--lcd-text);
    background: transparent;
    text-align: left;
  }
  .search-suggestion:last-of-type {
    border-bottom: 0;
  }
  .search-suggestion:hover,
  .search-suggestion.active,
  .search-suggestion:focus-visible {
    outline: 0;
    background: rgba(241, 243, 244, 0.07);
  }
  .search-suggestion img {
    display: block;
    width: 38px;
    height: 38px;
    border-radius: 8px;
    object-fit: cover;
    filter: saturate(0.86) contrast(1.04);
  }
  .suggestion-copy {
    display: flex;
    flex-direction: column;
    min-width: 0;
    gap: 0.08rem;
  }
  .suggestion-copy strong {
    overflow: hidden;
    color: var(--lcd-text-bright);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 0.94rem;
    font-weight: 400;
    text-overflow: ellipsis;
    text-shadow: var(--lcd-text-shadow);
    white-space: nowrap;
  }
  .suggestion-copy span {
    overflow: hidden;
    color: var(--lcd-text-soft);
    font-size: 0.75rem;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .suggestion-state {
    color: var(--lcd-text-bright);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 0.72rem;
    letter-spacing: 0.04em;
    text-shadow: var(--lcd-text-shadow);
    white-space: nowrap;
  }
  .suggestion-state.on-air {
    color: var(--accent);
  }
  .search-empty {
    margin: 0;
    padding: 0.9rem 0.35rem;
    color: var(--lcd-text-soft);
    font-size: 0.82rem;
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
  .club-directory-row {
    display: grid;
    grid-template-columns: 52px minmax(0, 1fr) auto;
    grid-template-rows: auto auto;
    column-gap: 0.9rem;
    row-gap: 0.35rem;
    align-items: start;
  }
  .club-directory-row > .pic {
    grid-column: 1;
    grid-row: 1 / span 2;
    align-self: center;
  }
  .club-directory-row > .meta {
    grid-column: 2;
    grid-row: 1 / span 2;
  }
  .club-directory-row > .club-dj-status {
    grid-column: 3;
    grid-row: 1;
    justify-self: end;
    align-self: start;
  }
  .club-directory-row > .enter-club {
    grid-column: 3;
    grid-row: 2;
    justify-self: end;
    align-self: end;
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
  .dj-status {
    display: flex;
    align-items: center;
    flex: 0 1 auto;
    min-width: 0;
    gap: 0;
    color: var(--lcd-text);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .dj-status :global(.zap-mini.icon-only.with-name) {
    max-width: 220px;
    height: 24px;
    padding: 0;
    gap: 0.35rem;
    color: var(--lcd-text);
  }
  .dj-status :global(.bolt-icon) {
    width: 24px;
    height: 24px;
  }
  .dj-status :global(.icon-dj-name) {
    max-width: 180px;
  }
  .live-label {
    margin-left: 1.25em;
    color: var(--accent);
    white-space: nowrap;
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
  .name-line {
    display: flex;
    align-items: baseline;
    min-width: 0;
    gap: 0.55rem;
  }
  .name-line .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
  .club-dj-status {
    gap: 0.25rem;
    font-size: 0.74rem;
    letter-spacing: 0;
    text-transform: none;
  }
  .club-dj-status .live-label {
    margin-left: 0;
    padding: 0;
    border: 0;
    color: var(--accent);
    background: transparent;
    font-size: 8px;
    letter-spacing: 0.06em;
    line-height: 1;
    transform: translateY(-0.45em);
  }
  .club-dj-name {
    max-width: 130px;
    overflow: hidden;
    color: var(--lcd-text);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 1rem;
    letter-spacing: 0;
    line-height: 1;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .host-avatar {
    width: 18px;
    height: 18px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
  }
  .enter-club {
    flex: 0 0 auto;
    padding: 0;
    border: 0;
    border-radius: 0;
    color: var(--lcd-text-bright);
    background: transparent;
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 0.82rem;
    font-weight: 400;
    letter-spacing: 0.04em;
    text-shadow: var(--lcd-text-shadow);
    text-transform: uppercase;
  }
  .enter-club:hover,
  .enter-club:focus-visible {
    color: var(--accent);
    background: transparent;
  }
  .enter-club:focus-visible {
    outline: 1px solid currentColor;
    outline-offset: 4px;
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
  }
  .lb-all {
    background: none;
    border: none;
    color: var(--accent-2);
    font-size: 0.85rem;
    font-weight: 700;
    cursor: pointer;
    padding: 0;
    text-decoration: none;
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
    display: block;
    margin-top: 1.5rem;
    padding: 1rem 1.1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    color: var(--text);
  }
  .tg-block .head {
    margin-top: 0;
  }
  .stage-label {
    margin-left: 1.25em;
    color: var(--lcd-text-soft);
    white-space: nowrap;
  }

  /* Site-wide LCD pass: open modules, selected LED tint, no SaaS-card chrome. */
  :global(body.site-led-page) .wrap {
    width: min(960px, 100%);
    max-width: 960px;
    padding: 0.8rem 0.8rem 4rem;
  }
  :global(body.site-led-page) .hero,
  :global(body.site-led-page) .tg-block {
    border: 0;
    border-radius: 0;
    box-shadow: none;
  }
  :global(body.site-led-page) .hero {
    min-height: 290px;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    margin-bottom: 0.7rem;
    padding: clamp(1.2rem, 4vw, 2.4rem);
  }
  :global(body.site-led-page) .eyebrow,
  :global(body.site-led-page) .hero-title,
  :global(body.site-led-page) .name,
  :global(body.site-led-page) .lb-pod-name {
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    text-shadow: var(--lcd-text-shadow);
  }
  :global(body.site-led-page) .eyebrow {
    color: var(--accent);
    letter-spacing: 0.08em;
  }
  :global(body.site-led-page) .hero-title {
    max-width: 760px;
    font-size: clamp(27px, calc(4.4vw - 5px), var(--site-h1-max));
    line-height: 1;
    letter-spacing: 0.01em;
    word-spacing: -0.12em;
  }
  :global(body.site-led-page) .hero-sub {
    max-width: 66ch;
    color: var(--lcd-text-soft);
  }
  :global(body.site-led-page) .chips {
    gap: 0.45rem 1.1rem;
    margin-bottom: 0;
  }
  :global(body.site-led-page) .chip {
    padding: 0;
    border: 0;
    border-radius: 0;
    color: var(--lcd-text);
    background: transparent;
    font-family: 'DotGothic16', ui-monospace, monospace;
  }
  :global(body.site-led-page) .head {
    margin: 1rem 0 0.7rem;
    padding: 0 0 0.5rem;
    border-bottom: 1px solid rgba(241, 243, 244, 0.22);
  }
  :global(body.site-led-page) .clubs-panel {
    padding: 1rem;
  }
  :global(body.site-led-page) .clubs-panel .head {
    margin: 0 0 0.7rem;
  }
  :global(body.site-led-page) .tg-block .head {
    margin: 0 0 0.7rem;
  }
  :global(body.site-led-page) .list {
    gap: 0;
  }
  :global(body.site-led-page) .clubs-panel .row,
  :global(body.site-led-page) .tg-block .row {
    min-height: 88px;
    padding: 0.85rem 0.2rem;
    border: 0;
    border-bottom: 1px solid rgba(241, 243, 244, 0.14);
    color: var(--lcd-text);
    background: transparent;
    transition: background-color 0.15s ease, transform 0.08s ease;
  }
  :global(body.site-led-page) .clubs-panel .list > li:last-child .row,
  :global(body.site-led-page) .tg-block .list > li:last-child .row {
    border-bottom: 0;
  }
  :global(body.site-led-page) .clubs-panel .row:hover,
  :global(body.site-led-page) .tg-block .row:hover {
    border-color: rgba(241, 243, 244, 0.14);
    background: rgba(241, 243, 244, 0.035);
  }
  :global(body.site-led-page) .row.onstage {
    animation: none;
  }
  :global(body.site-led-page) .pic,
  :global(body.site-led-page) .host-avatar,
  :global(body.site-led-page) .lb-pod-av,
  :global(body.site-led-page) .lb-av {
    border-color: rgba(241, 243, 244, 0.26);
    filter: saturate(0.86) contrast(1.04);
  }
  :global(body.site-led-page) .tag,
  :global(body.site-led-page) .badge-in {
    padding: 0;
    border: 0;
    border-radius: 0;
    font-family: 'DotGothic16', ui-monospace, monospace;
  }
  :global(body.site-led-page) .lb-preview {
    margin-top: 0.7rem;
    padding: 1rem;
  }
  :global(body.site-led-page) .lb-pod,
  :global(body.site-led-page) .lb-row,
  :global(body.site-led-page) .lb-pod.lb-pod-first {
    border: 0;
    border-radius: 0;
    background: rgba(0, 0, 0, 0.28);
    box-shadow: none;
  }
  :global(body.site-led-page) .lb-all,
  :global(body.site-led-page) .all-clubs-link {
    color: var(--lcd-text-bright);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    text-shadow: var(--lcd-text-shadow);
  }
  :global(body.site-led-page) .tg-block {
    margin-top: 0.7rem;
    padding: 1rem;
    color: var(--lcd-text);
  }
  @media (max-width: 560px) {
    :global(body.site-led-page) .wrap { padding: 0.45rem 0.45rem 3rem; }
    :global(body.site-led-page) .hero {
      min-height: 260px;
      margin-bottom: 0.55rem;
      padding: 1rem;
    }
    :global(body.site-led-page) .hero-title { font-size: clamp(27px, calc(9vw - 5px), 33.4px); }
    :global(body.site-led-page) .chips { gap: 0.4rem 0.8rem; }
    :global(body.site-led-page) .club-search { margin-bottom: 0.6rem; }
    :global(body.site-led-page) .search-line { min-height: 40px; }
    :global(body.site-led-page) .search-suggestion {
      grid-template-columns: 34px minmax(0, 1fr) auto;
      gap: 0.6rem;
      min-height: 52px;
      padding-inline: 0.2rem;
    }
    :global(body.site-led-page) .search-suggestion img { width: 34px; height: 34px; }
    :global(body.site-led-page) .suggestion-copy span { display: none; }
    :global(body.site-led-page) .suggestion-state { font-size: 0.66rem; }
    :global(body.site-led-page) .clubs-panel .row,
    :global(body.site-led-page) .tg-block .row {
      display: grid;
      grid-template-columns: 52px minmax(0, 1fr) auto;
      min-height: 96px;
      padding: 0.7rem 0.1rem;
    }
    :global(body.site-led-page) .clubs-panel .pic,
    :global(body.site-led-page) .tg-block .pic { grid-row: 1 / span 2; }
    :global(body.site-led-page) .clubs-panel .meta,
    :global(body.site-led-page) .tg-block .meta { grid-column: 2; }
    :global(body.site-led-page) .clubs-panel .enter-club { grid-column: 3; grid-row: 2; }
    :global(body.site-led-page) .tg-block .enter-club { grid-column: 3; grid-row: 1; }
    :global(body.site-led-page) .clubs-panel .dj-status { grid-column: 3; grid-row: 1; }
    :global(body.site-led-page) .tg-block .dj-status { grid-column: 2 / -1; grid-row: 2; }
    :global(body.site-led-page) .clubs-panel { padding: 0.8rem; }
    :global(body.site-led-page) .lb-preview,
    :global(body.site-led-page) .tg-block { margin-top: 0.55rem; padding: 0.8rem; }
  }
</style>

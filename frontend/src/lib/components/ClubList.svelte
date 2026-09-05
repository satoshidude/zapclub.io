<script lang="ts">
  import { listClubs, fetchClubMemberCounts, fetchOnAirClubTracks, fetchOnStageClubDjs, subscribeClubMemberCounts, type OnAirClubTrack } from '../nostr/groups'
  import { goClub } from '../router.svelte'
  import { useProfile, displayName } from '../nostr/profiles.svelte'
  import { persistedStageGroup } from '../nostr/stage.svelte'
  import { marquee } from '../actions/marquee'
  import { LISTENER_COUNT_STALE_MS, subscribeListenerCounts } from '../nostr/listeners.svelte'
  import { findClubSuggestions } from './clubSearch'
  import { clubAvatar } from '../avatar'
  import type { Club } from '../nostr/types'

  const TELEGRAM_BOT_CLUB_ID = 'c7ca6a16dd1ed946'

  let clubs = $state<Club[]>([])
  let memberCounts = $state<Record<string, { count: number; sentAt: number }>>({})
  let onAirTracks = $state<Map<string, OnAirClubTrack>>(new Map())
  let onStageDjs = $state<Map<string, string>>(new Map())
  let loading = $state(true)
  let error = $state('')
  let loadVersion = 0

  let listenerTotals = $state<Record<string, { count: number; sentAt: number }>>({})
  let listenerTick = $state(Date.now())
  const listenerCounts = $derived.by(() => {
    void listenerTick
    const now = Date.now()
    const out: Record<string, number> = {}
    for (const [id, total] of Object.entries(listenerTotals)) {
      if (now - total.sentAt <= LISTENER_COUNT_STALE_MS) out[id] = total.count
    }
    return out
  })

  const directoryClubs = $derived(
    clubs.filter((club) => club.id !== TELEGRAM_BOT_CLUB_ID && onAirTracks.has(club.id)),
  )
  const telegramBotClub = $derived(clubs.find((club) => club.id === TELEGRAM_BOT_CLUB_ID) ?? null)
  const searchableClubs = $derived(clubs.filter((club) => club.id !== TELEGRAM_BOT_CLUB_ID))
  let clubQuery = $state('')
  let searchOpen = $state(false)
  let activeSuggestion = $state(-1)
  const clubSuggestions = $derived(findClubSuggestions(searchableClubs, clubQuery))
  const showSuggestions = $derived(searchOpen && clubQuery.trim().length > 0)
  let showAllClubs = $state(false)

  function trackParts(value: string): { artist: string; title: string } {
    const full = value.trim()
    const match = full.match(/^(.+?) [–—-] (.+)$/)
    return match ? { artist: match[1], title: match[2] } : { artist: '', title: full }
  }

  // The club the user is currently DJing in → pin to the top + highlight.
  const onStageClub = persistedStageGroup()
  const sortedClubs = $derived.by(() => {
    const byMembers = [...directoryClubs].sort(
      (a, b) => (memberCounts[b.id]?.count ?? 0) - (memberCounts[a.id]?.count ?? 0),
    )
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

  async function load() {
    const version = ++loadVersion
    loading = true
    error = ''
    try {
      const loadedClubs = await listClubs()
      const clubIds = loadedClubs.map((club) => club.id)
      const [loadedOnAirTracks, loadedOnStageDjs, loadedMemberCounts] = await Promise.all([
        fetchOnAirClubTracks(clubIds),
        fetchOnStageClubDjs([TELEGRAM_BOT_CLUB_ID]),
        fetchClubMemberCounts(clubIds),
      ])
      if (version !== loadVersion) return
      clubs = loadedClubs
      memberCounts = Object.fromEntries(loadedMemberCounts)
      onAirTracks = loadedOnAirTracks
      onStageDjs = loadedOnStageDjs
    } catch (e) {
      if (version !== loadVersion) return
      error = String((e as Error)?.message ?? e)
    } finally {
      if (version === loadVersion) loading = false
    }
  }

  $effect(() => {
    void load()
  })

  // Keep the public directory live after the initial snapshot. A club that
  // starts or stops broadcasting should appear/disappear without a reload.
  $effect(() => {
    const clubIds = clubs.map((club) => club.id)
    if (clubIds.length === 0) return
    let cancelled = false
    const timer = setInterval(() => {
      void fetchOnAirClubTracks(clubIds).then((next) => {
        if (!cancelled) onAirTracks = next
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
    const ids = clubs.map((club) => club.id)
    const unsub = subscribeListenerCounts(ids, (clubId, count, sentAt) => {
      const previous = listenerTotals[clubId]
      if (!previous || sentAt >= previous.sentAt) {
        listenerTotals = { ...listenerTotals, [clubId]: { count, sentAt } }
      }
    })
    const tick = setInterval(() => { listenerTick = Date.now() }, 5_000)
    return () => { unsub(); clearInterval(tick) }
  })

  $effect(() => {
    const ids = clubs.map((club) => club.id)
    return subscribeClubMemberCounts(ids, (clubId, count, sentAt) => {
      const previous = memberCounts[clubId]
      if (!previous || sentAt >= previous.sentAt) {
        memberCounts = { ...memberCounts, [clubId]: { count, sentAt } }
      }
    })
  })

</script>

<div class="wrap home-page">
  <header class="hero led-zone">
    <div class="hero-art" aria-hidden="true"></div>
    <p class="eyebrow lcd-card-title">Social · Decentralized · Entertaining</p>
    <h1 class="hero-title site-h1">Drop in.<br />Take the stage.<br />Own the night.</h1>
    <p class="hero-sub">
      zapclub is one turntable, shared in a club. Fill your playlists, enter the stage, pass it on.
      Tip the DJ in sats and rate his tracks. Just you, playlists and the crowd.
    </p>
    <div class="hero-features" aria-label="Zapclub features">
      <span class="hero-feature">
        <svg class="hero-feature-icon hero-feature-sync" viewBox="0 0 24 24" aria-hidden="true">
          <path d="M4.5 9A8 8 0 0 1 18 5.5L20.5 8"></path>
          <path d="M20.5 4.5V8h-3.5"></path>
          <path d="M19.5 15A8 8 0 0 1 6 18.5L3.5 16"></path>
          <path d="M3.5 19.5V16H7"></path>
          <circle cx="12" cy="12" r="1.6"></circle>
        </svg>
        <span>deck conductor</span>
      </span>
      <span class="hero-feature">
        <span class="hero-feature-icon hero-feature-zap" aria-hidden="true">⚡︎</span>
        <span>Zap the DJ</span>
      </span>
      <span class="hero-feature hero-feature-nostr">
        <img class="hero-feature-icon hero-feature-nostrich" src="/nostrich.png" alt="" aria-hidden="true" />
        <span>nostr driven experience</span>
      </span>
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
              <span class:on-air={onAirTracks.has(club.id)} class="suggestion-state">
                {onAirTracks.has(club.id) ? 'ON AIR' : 'ENTER CLUB'}
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
          {@const live = onAirTracks.get(club.id)!}
          {@const liveDjProfile = useProfile(live.dj)}
          {@const track = trackParts(live.title)}
          <li>
            <a class="club-player-row" href={`/club/${club.id}`} onclick={(event) => { event.preventDefault(); goClub(club.id) }}>
              <div class="pic club-player-pic">
                <img class="pic-img" src={club.picture || clubAvatar(club.owner || club.id)} alt="" />
              </div>
              <div class="club-player-content">
                <div class="club-player-status">
                  <span class="club-player-club">
                    <span class="club-player-name">{club.name}</span>
                    <span class="club-player-tags">
                      {#if memberCounts[club.id]}
                        {@const memberCount = memberCounts[club.id].count}
                        <span class="club-player-separator" aria-hidden="true">|</span>
                        <span class="tag members-tag" title={`${memberCount} member${memberCount === 1 ? '' : 's'}`}>
                          <svg class="tag-icon" viewBox="0 0 24 24" aria-hidden="true">
                            <path d="M16 20v-1.5a4.5 4.5 0 0 0-4.5-4.5h-4A4.5 4.5 0 0 0 3 18.5V20"></path>
                            <circle cx="9.5" cy="7.5" r="3.5"></circle>
                            <path d="M16 11a3 3 0 1 0 0-6M18 14.5a4 4 0 0 1 3 3.87V20"></path>
                          </svg>
                          {memberCount}
                        </span>
                      {/if}
                      {#if club.owner}
                        {@const ownerProfile = useProfile(club.owner)}
                        <span class="club-player-separator" aria-hidden="true">|</span>
                        <span class="tag host-tag" title={`Host: ${displayName(club.owner, ownerProfile)}`}>
                          <svg class="tag-icon" viewBox="0 0 24 24" aria-hidden="true">
                            <path d="m4 8 3.2 3L12 5l4.8 6L20 8l-1.6 10H5.6L4 8Z"></path>
                            <path d="M6 21h12"></path>
                          </svg>
                          <span class="tag-text">{displayName(club.owner, ownerProfile)}</span>
                        </span>
                      {/if}
                      {#if (listenerCounts[club.id] ?? 0) > 0}
                        <span class="club-player-separator" aria-hidden="true">|</span>
                        <span class="tag listener-tag" title="People listening to the stream right now">
                          <svg class="tag-icon" viewBox="0 0 24 24" aria-hidden="true">
                            <path d="M4 14v-2a8 8 0 0 1 16 0v2"></path>
                            <path d="M4 14h3v6H5.5A1.5 1.5 0 0 1 4 18.5V14ZM20 14h-3v6h1.5a1.5 1.5 0 0 0 1.5-1.5V14Z"></path>
                          </svg>
                          {listenerCounts[club.id]}
                        </span>
                      {/if}
                      <span class="club-player-separator" aria-hidden="true">|</span>
                      <span class="tag club-player-dj-tag" title={`DJ: ${displayName(live.dj, liveDjProfile)}`}>
                        <span>DJ:</span>
                        <span class="club-player-dj-accent">
                          <span class="club-player-sats-icon" aria-hidden="true">⚡︎</span>
                          <span class="club-player-dj-name">{displayName(live.dj, liveDjProfile)}</span>
                        </span>
                      </span>
                    </span>
                  </span>
                </div>
                <div class="club-player-title" use:marquee>
                  <span class="mq-inner">{track.title || 'Untitled track'}</span>
                </div>
                <div class="club-player-byline">
                  <div class="club-player-artist">{track.artist || 'Live set'}</div>
                  <div class="club-player-actions"><span class="enter-club">Enter club</span></div>
                </div>
              </div>
            </a>
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
      {@const botLive = onAirTracks.get(TELEGRAM_BOT_CLUB_ID)}
      {@const botStageDj = onStageDjs.get(TELEGRAM_BOT_CLUB_ID)}
      {@const botStatusDj = botLive?.dj || botStageDj}
      {@const botTrack = botLive ? trackParts(botLive.title) : null}
      <ul class="list">
        <li>
          <a class="club-player-row telegram-club-row" class:onstage={!!botStatusDj} href={`/club/${TELEGRAM_BOT_CLUB_ID}`} onclick={(event) => { event.preventDefault(); goClub(TELEGRAM_BOT_CLUB_ID) }}>
            <div class="pic club-player-pic">
              <img class="pic-img" src={telegramBotClub.picture || clubAvatar(telegramBotClub.owner || telegramBotClub.id)} alt="" />
            </div>
            <div class="club-player-content">
              <div class="club-player-status">
                <span class="club-player-club">
                  <span class="club-player-name">{telegramBotClub.name}</span>
                  <span class="club-player-tags">
                    {#if memberCounts[TELEGRAM_BOT_CLUB_ID]}
                      {@const memberCount = memberCounts[TELEGRAM_BOT_CLUB_ID].count}
                      <span class="club-player-separator" aria-hidden="true">|</span>
                      <span class="tag members-tag" title={`${memberCount} member${memberCount === 1 ? '' : 's'}`}>
                        <svg class="tag-icon" viewBox="0 0 24 24" aria-hidden="true">
                          <path d="M16 20v-1.5a4.5 4.5 0 0 0-4.5-4.5h-4A4.5 4.5 0 0 0 3 18.5V20"></path>
                          <circle cx="9.5" cy="7.5" r="3.5"></circle>
                          <path d="M16 11a3 3 0 1 0 0-6M18 14.5a4 4 0 0 1 3 3.87V20"></path>
                        </svg>
                        {memberCount}
                      </span>
                    {/if}
                    {#if telegramBotClub.owner}
                      {@const ownerProfile = useProfile(telegramBotClub.owner)}
                      <span class="club-player-separator" aria-hidden="true">|</span>
                      <span class="tag host-tag" title={`Host: ${displayName(telegramBotClub.owner, ownerProfile)}`}>
                        <svg class="tag-icon" viewBox="0 0 24 24" aria-hidden="true">
                          <path d="m4 8 3.2 3L12 5l4.8 6L20 8l-1.6 10H5.6L4 8Z"></path>
                          <path d="M6 21h12"></path>
                        </svg>
                        <span class="tag-text">{displayName(telegramBotClub.owner, ownerProfile)}</span>
                      </span>
                    {/if}
                    {#if (listenerCounts[TELEGRAM_BOT_CLUB_ID] ?? 0) > 0}
                      <span class="club-player-separator" aria-hidden="true">|</span>
                      <span class="tag listener-tag" title="People listening to the stream right now">
                        <svg class="tag-icon" viewBox="0 0 24 24" aria-hidden="true">
                          <path d="M4 14v-2a8 8 0 0 1 16 0v2"></path>
                          <path d="M4 14h3v6H5.5A1.5 1.5 0 0 1 4 18.5V14ZM20 14h-3v6h1.5a1.5 1.5 0 0 0 1.5-1.5V14Z"></path>
                        </svg>
                        {listenerCounts[TELEGRAM_BOT_CLUB_ID]}
                      </span>
                    {/if}
                    {#if botStatusDj}
                      {@const botDjProfile = useProfile(botStatusDj)}
                      <span class="club-player-separator" aria-hidden="true">|</span>
                      <span class="tag club-player-dj-tag" title={`DJ: ${displayName(botStatusDj, botDjProfile)}`}>
                        <span>DJ:</span>
                        <span class="club-player-dj-accent">
                          <span class="club-player-sats-icon" aria-hidden="true">⚡︎</span>
                          <span class="club-player-dj-name">{displayName(botStatusDj, botDjProfile)}</span>
                        </span>
                      </span>
                    {/if}
                  </span>
                </span>
              </div>
              {#if botTrack}
                <div class="club-player-title" use:marquee>
                  <span class="mq-inner">{botTrack.title || 'Untitled track'}</span>
                </div>
              {:else}
                <div class="club-player-title club-player-placeholder">
                  {botStageDj ? 'DJ is loading tracks' : 'No DJ on stage'}
                </div>
              {/if}
              <div class="club-player-byline">
                <div class="club-player-artist">
                  {botTrack ? (botTrack.artist || 'Live set') : (botStageDj ? 'Playback starts automatically.' : 'Take the first slot and start a set.')}
                </div>
                <div class="club-player-actions"><span class="enter-club">Enter club</span></div>
              </div>
            </div>
          </a>
        </li>
      </ul>
    {:else}
      <p class="dim">Telegram Bot Club is unavailable.</p>
    {/if}
  </section>

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
  .hero-art {
    position: absolute;
    inset: 0;
    z-index: 0;
    pointer-events: none;
    background: url('/images/home-hero-turntable-desktop.webp') right center / cover no-repeat;
    -webkit-mask-image: linear-gradient(90deg, transparent 0 43%, rgba(0, 0, 0, 0.12) 50%, #000 61%);
    mask-image: linear-gradient(90deg, transparent 0 43%, rgba(0, 0, 0, 0.12) 50%, #000 61%);
    opacity: 0.82;
  }
  .hero > :not(.hero-art) {
    position: relative;
    z-index: 1;
  }
  .eyebrow {
    margin: 0 0 0.55rem;
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
  .hero-features {
    display: grid;
    grid-template-columns: max-content max-content;
    align-items: center;
    justify-content: flex-start;
    gap: 0.12rem 0.32rem;
    width: 100%;
    margin-bottom: 1.2rem;
  }
  .hero-feature {
    display: flex;
    align-items: center;
    min-width: 0;
    gap: 0.1rem;
    color: #fff;
    font-family: var(--font);
    font-size: 16px;
    font-weight: 700;
    letter-spacing: 0;
    line-height: 1.05;
    text-shadow: var(--lcd-text-shadow);
    white-space: nowrap;
  }
  .hero-feature-nostr {
    grid-column: 1 / -1;
  }
  .hero-feature-icon {
    width: 29px;
    height: 29px;
    flex: 0 0 29px;
    overflow: visible;
    color: #fff;
    fill: none;
    stroke: currentColor;
    stroke-linecap: square;
    stroke-linejoin: miter;
    stroke-width: 1.65;
    margin-right: -3px;
  }
  .hero-feature-zap {
    display: grid;
    place-items: center;
    font-family: ui-sans-serif, system-ui, sans-serif;
    font-size: 24px;
    font-weight: 700;
    line-height: 1;
    stroke: none;
  }
  .hero-feature-sync {
    width: 26px;
    height: 26px;
    flex-basis: 26px;
    margin-right: 2px;
  }
  .hero-feature-nostrich {
    width: 51px;
    height: 41px;
    flex-basis: 51px;
    object-fit: contain;
    filter: grayscale(1) brightness(0) invert(1);
    opacity: 0.92;
    margin-left: -12px;
    margin-right: -10px;
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
    border-radius: 0;
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
  .club-player-row {
    --player-led-surface:
      radial-gradient(circle, rgba(241, 243, 244, 0.045) 0 0.55px, transparent 0.75px) 0 0 / 4px 4px;
    position: relative;
    display: grid;
    grid-template-columns: 128px minmax(0, 1fr);
    align-items: stretch;
    column-gap: 0;
    min-height: 112px;
    padding: 0;
    overflow: hidden;
    color: var(--lcd-text);
    background: var(--player-led-surface);
    border-bottom: 1px solid rgba(241, 243, 244, 0.14);
    font-family: 'DotGothic16', ui-monospace, monospace;
    text-decoration: none;
    text-shadow: var(--lcd-text-shadow);
    transition: background-color 0.15s ease, transform 0.08s ease;
  }
  .list > li:last-child .club-player-row { border-bottom: 0; }
  .club-player-row:hover { background-color: rgba(241, 243, 244, 0.035); }
  .club-player-row:focus-visible {
    z-index: 1;
    outline: 1px solid var(--lcd-text-bright);
    outline-offset: -1px;
  }
  .club-player-row:active { transform: translateY(1px); }
  .club-player-pic {
    align-self: stretch;
  }
  .club-player-content {
    display: grid;
    grid-template-rows: auto auto 1fr;
    align-self: stretch;
    min-width: 0;
    padding: 10px 15px 11px;
  }
  .club-player-status {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    min-width: 0;
    margin: 0 0 7px;
    padding: 0;
    color: var(--lcd-text-dim);
    font-size: 11px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .club-player-name {
    display: block;
    flex: 0 0 auto;
    width: 100%;
    min-width: 0;
    overflow: hidden;
    color: var(--lcd-text);
    font-size: 14px;
    letter-spacing: 0.03em;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .club-player-club {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    width: 100%;
    min-width: 0;
  }
  .club-player-tags {
    display: flex;
    flex: 0 1 auto;
    align-items: center;
    flex-wrap: nowrap;
    width: 100%;
    min-width: 0;
    margin: 4px 0 4px;
    overflow: hidden;
    color: var(--lcd-text-dim);
    font-size: 14px;
    letter-spacing: 0.03em;
    line-height: 1;
    text-transform: none;
    white-space: nowrap;
  }
  .club-player-tags > .club-player-separator:first-child { display: none; }
  .club-player-separator {
    flex: 0 0 auto;
    margin: 0 0.42rem;
    color: var(--lcd-text-soft);
  }
  .club-player-tags .tag {
    display: inline-flex;
    align-items: center;
    min-width: 0;
    gap: 0.24rem;
    color: var(--lcd-text);
    white-space: nowrap;
  }
  .club-player-tags .host-tag { overflow: hidden; }
  .club-player-tags .tag-text {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .club-player-tags .members-tag,
  .club-player-tags .listener-tag { flex: 0 0 auto; }
  .club-player-tags .club-player-dj-tag {
    flex: 0 1 auto;
    overflow: hidden;
  }
  .club-player-dj-accent {
    display: inline-flex;
    align-items: baseline;
    min-width: 0;
    gap: 0.18rem;
    overflow: hidden;
    color: #f4e04d;
    font: inherit;
    letter-spacing: inherit;
    line-height: inherit;
    text-shadow: var(--lcd-text-shadow);
    text-transform: none;
  }
  .club-player-sats-icon { flex: 0 0 auto; }
  .club-player-dj-name {
    min-width: 0;
    overflow: hidden;
    color: inherit;
    text-overflow: ellipsis;
    text-transform: none;
    white-space: nowrap;
  }
  .club-player-tags .tag-icon {
    width: 13px;
    height: 13px;
    flex: 0 0 13px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .club-player-title {
    overflow: hidden;
    color: var(--lcd-text);
    font-size: 1rem;
    line-height: 1;
    letter-spacing: 0.01em;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .club-player-title .mq-inner {
    display: inline-block;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    vertical-align: top;
  }
  .club-player-row:hover .club-player-title:global([data-mq]) .mq-inner {
    max-width: none;
    overflow: visible;
    text-overflow: clip;
    animation: club-title-scroll 6s ease-in-out infinite;
  }
  .club-player-artist {
    min-width: 0;
    overflow: hidden;
    color: var(--text-dim);
    font-size: 0.82rem;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .club-player-placeholder { color: var(--lcd-text-soft); }
  .club-player-byline {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    min-width: 0;
    margin-top: 2px;
  }
  .club-player-actions {
    display: flex;
    align-self: stretch;
    align-items: flex-end;
    flex: 0 0 auto;
    margin-left: 12px;
    transform: translateY(11px);
  }
  @keyframes club-title-scroll {
    0%, 15% { transform: translateX(0); }
    85%, 100% { transform: translateX(var(--mq-shift, 0px)); }
  }
  /* The club the user is DJing in: pinned to the top, pulsing green. */
  .club-player-row.onstage {
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
    .club-player-row.onstage {
      animation: none;
    }
    .club-player-row:hover .club-player-title:global([data-mq]) .mq-inner {
      max-width: 100%;
      overflow: hidden;
      text-overflow: ellipsis;
      animation: none;
    }
  }
  .pic {
    position: relative;
    width: 128px;
    height: auto;
    min-height: 0;
    border-radius: 0;
    overflow: hidden;
    background: var(--bg-elev-2);
  }
  .pic-img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    border-radius: 0;
    object-fit: cover;
    object-position: center;
    display: block;
  }
  .enter-club {
    flex: 0 0 auto;
    padding: 0;
    border: 0;
    border-radius: 0;
    color: #c084fc;
    background: transparent;
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 0.82rem;
    font-weight: 400;
    letter-spacing: 0.04em;
    text-shadow: 0 0 3px rgba(192, 132, 252, 0.82), 0 0 10px rgba(142, 48, 235, 0.34);
    text-transform: uppercase;
  }
  .enter-club:hover,
  .enter-club:focus-visible {
    color: #d8b4fe;
    background: transparent;
    text-shadow: 0 0 4px rgba(216, 180, 254, 0.9), 0 0 12px rgba(168, 85, 247, 0.46);
  }
  .enter-club:focus-visible {
    outline: 1px solid currentColor;
    outline-offset: 4px;
  }
  .dim {
    color: var(--text-dim);
  }
  .err {
    color: var(--danger);
    font-size: 0.85rem;
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
    justify-content: flex-start;
    margin-bottom: 0.7rem;
    padding: 1rem;
  }
  :global(body.site-led-page) .hero-title {
    max-width: 540px;
  }
  :global(body.site-led-page) .hero-sub {
    max-width: 50ch;
    color: var(--lcd-text-soft);
  }
  :global(body.site-led-page) .hero-features {
    max-width: none;
    margin-bottom: 0;
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
    gap: 0.7rem;
  }
  :global(body.site-led-page) .club-player-row.onstage {
    animation: none;
  }
  :global(body.site-led-page) .pic {
    border-color: rgba(241, 243, 244, 0.26);
    filter: saturate(0.86) contrast(1.04);
  }
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
    .club-player-row {
      grid-template-columns: 104px minmax(0, 1fr);
      min-height: 104px;
    }
    .pic {
      width: 104px;
    }
    .club-player-content { padding: 9px 9px 10px; }
    .club-player-actions { transform: translateY(10px); }
    .club-player-status { font-size: 9px; }
    .club-player-name { font-size: 12px; }
    .club-player-tags { font-size: 12px; }
    .club-player-title { font-size: 1rem; }
    .club-player-artist { font-size: 0.82rem; }
    .club-player-actions { min-height: 23px; }
    .club-player-actions .enter-club { font-size: 0.72rem; }
    :global(body.site-led-page) .wrap { padding: 0.45rem 0.45rem 3rem; }
    :global(body.site-led-page) .hero {
      min-height: 500px;
      margin-bottom: 0.55rem;
      padding: clamp(205px, 56vw, 232px) 1rem 1rem;
    }
    .hero-art {
      background-image: url('/images/home-hero-turntable-mobile.webp');
      background-position: right top;
      background-size: 100% auto;
      -webkit-mask-image: none;
      mask-image: none;
      opacity: 0.86;
    }
    :global(body.site-led-page) .hero-features { gap: 0.12rem 0.28rem; }
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
    :global(body.site-led-page) .clubs-panel { padding: 0.8rem; }
    :global(body.site-led-page) .list { gap: 0.55rem; }
    :global(body.site-led-page) .tg-block { margin-top: 0.55rem; padding: 0.8rem; }
  }

</style>

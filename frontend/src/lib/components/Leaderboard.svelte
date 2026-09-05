<script lang="ts">
  import { npubEncode } from 'nostr-tools/nip19'
  import { fetchLeaderboard, type LeaderboardEntry, type TrackLeaderboardEntry } from '../nostr/leaderboard'
  import { listClubs } from '../nostr/groups'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import { goClub, goUser } from '../router.svelte'

  let entries = $state<LeaderboardEntry[]>([])
  let tracks = $state<TrackLeaderboardEntry[]>([])
  let clubNames = $state<Record<string, string>>({})
  let total = $state(0)
  let loading = $state(true)

  $effect(() => {
    loading = true
    void fetchLeaderboard()
      .then((r) => {
        entries = r.top
        tracks = r.topTracks
        total = r.total
      })
      .finally(() => (loading = false))
    void listClubs()
      .then((clubs) => {
        clubNames = Object.fromEntries(clubs.map((club) => [club.id, club.name]))
      })
      .catch(() => {})
  })

  const medal = (rank: number) => (rank === 1 ? '🥇' : rank === 2 ? '🥈' : rank === 3 ? '🥉' : '')
  const djScore = (tenths: number) => (tenths / 10).toFixed(1)
  const signedVibe = (score: number) => `${score > 0 ? '+' : ''}${score.toLocaleString()}`
  const trackTitle = (track: TrackLeaderboardEntry) => track.title.trim() || 'Untitled track'
  const clubName = (club: string) => clubNames[club] || `Club ${club.slice(0, 8)}`
</script>

<div class="wrap leaderboard-page">
  <header class="lb-head led-zone">
    <h1 class="site-h1">TOP DJS Leaderboard</h1>
    <p class="sub">Ranked by songs played and the room’s Vibemeter. DJ SCORE rewards strong sets without letting sheer volume decide the board.</p>
    <p class="score-rule"><strong>DJ SCORE</strong> = vibe quality × experience factor. Ten songs reach 50% experience.</p>
  </header>

  <section class="track-panel led-zone" aria-labelledby="top-tracks-title" aria-live="polite">
    <div class="track-head">
      <h2 class="lcd-card-title" id="top-tracks-title">TOP 10 TRACKS</h2>
      <p>Individual plays with the most Vibemeter Bangers.</p>
    </div>
    {#if loading}
      <p class="dim">Loading…</p>
    {:else if tracks.length === 0}
      <p class="dim">No rated tracks yet — new settled plays will start this board.</p>
    {:else}
      <ol class="track-board">
        {#each tracks as track (track.club + ':' + track.startedAt + ':' + track.dj)}
          {@const profile = useProfile(track.dj)}
          {@const npub = npubEncode(track.dj)}
          <li class="track-row">
            <span class="track-rank">#{track.rank}</span>
            <span class="track-main">
              {#if track.videoId}
                <a class="track-title" href={`https://www.youtube.com/watch?v=${encodeURIComponent(track.videoId)}`} target="_blank" rel="noopener noreferrer">
                  {trackTitle(track)}
                </a>
              {:else}
                <span class="track-title">{trackTitle(track)}</span>
              {/if}
              <span class="track-source">
                <a href={`/club/${track.club}`} onclick={(event) => { event.preventDefault(); goClub(track.club) }}>{clubName(track.club)}</a>
                <span aria-hidden="true">·</span>
                <span>DJ</span>
                <a class="track-dj" href={`/user/${npub}`} onclick={(event) => { event.preventDefault(); goUser(npub) }}>{displayName(track.dj, profile)}</a>
                {#if track.skipped}<span class="skip-mark">COMMUNITY SKIP</span>{/if}
              </span>
            </span>
            <span class="track-votes"><strong>{track.bangers}</strong> {track.bangers === 1 ? 'BANGER' : 'BANGERS'}</span>
          </li>
        {/each}
      </ol>
    {/if}
  </section>

  <section class="ranking-panel led-zone" aria-live="polite">
    {#if loading}
      <p class="dim">Loading…</p>
    {:else if entries.length === 0}
      <p class="dim">No DJs ranked yet — the first settled song will start the board.</p>
    {:else}
      {#if entries.length >= 3}
        <div class="podium" aria-label="Top three DJs">
          {#each [entries[1], entries[0], entries[2]] as e (e.pubkey)}
            {@const p = useProfile(e.pubkey)}
            {@const npub = npubEncode(e.pubkey)}
            {@const isFirst = e.rank === 1}
            <button
              class="pod-slot"
              class:pod-first={isFirst}
              onclick={() => goUser(npub)}
              aria-label={`#${e.rank} ${displayName(e.pubkey, p)}, DJ SCORE ${djScore(e.score)}`}
            >
              <span class="pod-medal">{medal(e.rank)}</span>
              <img class="pod-av" src={avatarUrl(e.pubkey, p)} alt=""
                width={isFirst ? 52 : 40} height={isFirst ? 52 : 40} />
              <span class="pod-name">{displayName(e.pubkey, p)}</span>
              <span class="pod-score"><span>{djScore(e.score)}</span> DJ SCORE</span>
              <span class="pod-signals">
                {e.tracks.toLocaleString()} {e.tracks === 1 ? 'song' : 'songs'} · VIBE {signedVibe(e.vibeScore)}
              </span>
              <span class="pod-feedback">
                {e.bangers.toLocaleString()} {e.bangers === 1 ? 'banger' : 'bangers'} · {e.skipped.toLocaleString()} {e.skipped === 1 ? 'skip' : 'skips'}
              </span>
            </button>
          {/each}
        </div>
      {/if}
      <ol class="board">
        {#each entries.slice(entries.length >= 3 ? 3 : 0) as e (e.pubkey)}
          {@const p = useProfile(e.pubkey)}
          {@const npub = npubEncode(e.pubkey)}
          <li>
            <a class="row" class:top3={e.rank <= 3} href={`/user/${npub}`} onclick={(ev) => { ev.preventDefault(); goUser(npub) }}>
              <span class="rank">{medal(e.rank)}<span class="num">#{e.rank}</span></span>
              <img class="av" src={avatarUrl(e.pubkey, p)} alt="" width="40" height="40" />
              <span class="name">{displayName(e.pubkey, p)}</span>
              <span class="stats">
                <span class="score"><span class="score-value">{djScore(e.score)}</span> DJ SCORE</span>
                <span class="signals">
                  {e.tracks.toLocaleString()} {e.tracks === 1 ? 'song' : 'songs'} · VIBE {signedVibe(e.vibeScore)}
                </span>
                <span class="feedback">
                  {e.bangers.toLocaleString()} {e.bangers === 1 ? 'banger' : 'bangers'} · {e.skipped.toLocaleString()} {e.skipped === 1 ? 'skip' : 'skips'}
                </span>
              </span>
            </a>
          </li>
        {/each}
      </ol>
      {#if total > entries.length}
        <p class="dim foot">Showing the top {entries.length} of {total.toLocaleString()} ranked DJs.</p>
      {/if}
    {/if}
  </section>
</div>

<style>
  .wrap {
    width: min(960px, 100%);
    margin: 0 auto;
    padding: 0.8rem 0.8rem 4rem;
    color: var(--lcd-text);
  }
  .lb-head,
  .track-panel,
  .ranking-panel {
    margin-bottom: 0.7rem;
    border: 0;
    border-radius: 0;
  }
  .lb-head {
    min-height: 210px;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    padding: clamp(1.2rem, 4vw, 2.2rem);
  }
  .lb-head h1 {
    margin: 0 0 0.65rem;
  }
  .sub {
    max-width: 68ch;
    margin: 0;
    color: var(--lcd-text-soft);
    font-size: 0.96rem;
    line-height: 1.55;
  }
  .score-rule {
    max-width: 68ch;
    margin: 0.45rem 0 0;
    color: var(--lcd-text-dim);
    font-size: 0.76rem;
    line-height: 1.45;
  }
  .score-rule strong {
    color: var(--amber);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    letter-spacing: 0.04em;
  }
  .ranking-panel {
    padding: 1rem;
  }
  .track-panel {
    padding: 1rem;
  }
  .track-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 0.85rem;
    padding-bottom: 0.65rem;
    border-bottom: 1px solid rgba(241, 243, 244, 0.14);
  }
  .track-head h2,
  .track-head p {
    margin: 0;
  }
  .track-head p {
    color: var(--lcd-text-dim);
    font-size: 0.75rem;
  }
  .track-board {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .track-row {
    display: grid;
    grid-template-columns: 2.7rem minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.75rem;
    min-height: 62px;
    padding: 0.65rem 0.2rem;
    border-bottom: 1px solid rgba(241, 243, 244, 0.14);
  }
  .track-row:last-child { border-bottom: 0; }
  .track-rank,
  .track-votes {
    color: var(--amber);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    text-shadow: var(--lcd-text-shadow);
    font-variant-numeric: tabular-nums;
  }
  .track-rank { font-size: 0.95rem; }
  .track-main {
    display: flex;
    flex-direction: column;
    min-width: 0;
    gap: 0.16rem;
  }
  .track-title {
    overflow: hidden;
    color: var(--lcd-text-bright);
    font-size: 0.93rem;
    font-weight: 500;
    line-height: 1.25;
    text-decoration: none;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  a.track-title:hover,
  .track-source a:hover { color: var(--amber); }
  .track-source {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 0.28rem;
    color: var(--lcd-text-dim);
    font-size: 0.72rem;
    line-height: 1.35;
  }
  .track-source a {
    color: var(--lcd-text-soft);
    text-decoration: none;
  }
  .track-dj {
    font-family: 'DotGothic16', ui-monospace, monospace;
    letter-spacing: 0.03em;
  }
  .skip-mark {
    margin-left: 0.25rem;
    color: var(--danger, #ff5a67);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 0.62rem;
    letter-spacing: 0.04em;
  }
  .track-votes {
    font-size: 0.72rem;
    letter-spacing: 0.03em;
    white-space: nowrap;
  }
  .track-votes strong {
    font-size: 1.05rem;
    font-weight: 400;
  }
  .dim {
    margin: 0;
    color: var(--lcd-text-dim);
  }
  .foot {
    margin-top: 1rem;
    font-size: 0.8rem;
    text-align: center;
  }
  /* Podium: #2 left, #1 center (tallest), #3 right */
  .podium {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 1px;
    margin-bottom: 1px;
    align-items: end;
    background: transparent;
  }
  .pod-slot {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.3rem;
    min-width: 0;
    min-height: 178px;
    padding: 0.9rem 0.5rem 0.8rem;
    border: 0;
    border-radius: 0;
    color: var(--lcd-text);
    background: rgba(0, 0, 0, 0.3);
    cursor: pointer;
    text-align: center;
    transition: background-color 0.15s ease, transform 0.08s ease;
  }
  .pod-slot:hover { background: rgba(241, 243, 244, 0.055); }
  .pod-slot:focus-visible { outline: 1px solid var(--lcd-text-bright); outline-offset: -3px; }
  .pod-slot:active { transform: translateY(1px); }
  .pod-slot.pod-first {
    min-height: 194px;
    background: linear-gradient(180deg, rgba(245, 166, 35, 0.08), rgba(0, 0, 0, 0.3));
    padding-top: 1.1rem;
    padding-bottom: 0.9rem;
  }
  .pod-medal { font-size: 1.2rem; line-height: 1; }
  .pod-first .pod-medal { font-size: 1.5rem; }
  .pod-av {
    border-radius: 999px;
    object-fit: cover;
    background: rgba(0, 0, 0, 0.36);
    border: 0;
    filter: saturate(0.86) contrast(1.04);
  }
  .pod-name {
    max-width: 100%;
    overflow: hidden;
    color: var(--lcd-text-bright);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 0.86rem;
    font-weight: 400;
    letter-spacing: 0.04em;
    text-shadow: var(--lcd-text-shadow);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pod-first .pod-name { font-size: 1rem; }
  .pod-score {
    color: var(--amber);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    font-size: 0.76rem;
    letter-spacing: 0.03em;
    font-variant-numeric: tabular-nums;
  }
  .pod-score span {
    font-size: 1.05rem;
  }
  .pod-first .pod-score { font-size: 0.82rem; }
  .pod-first .pod-score span { font-size: 1.25rem; }
  .pod-signals,
  .pod-feedback {
    color: var(--lcd-text-dim);
    font-size: 0.66rem;
    line-height: 1.25;
  }
  .pod-feedback { opacity: 0.84; }
  .board {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.8rem;
    min-height: 68px;
    padding: 0.7rem 0.2rem;
    border: 0;
    border-bottom: 1px solid rgba(241, 243, 244, 0.14);
    border-radius: 0;
    background: transparent;
    cursor: pointer;
    color: var(--lcd-text);
    text-decoration: none;
    transition: background-color 0.15s ease, transform 0.08s ease;
  }
  .row:hover {
    background: rgba(241, 243, 244, 0.035);
  }
  .row:focus-visible { outline: 1px solid var(--lcd-text-bright); outline-offset: -3px; }
  .row:active {
    transform: translateY(1px);
  }
  .row.top3 {
    background: transparent;
  }
  .rank {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 0.2rem;
    min-width: 3.2rem;
    font-size: 1.1rem;
  }
  .rank .num {
    color: var(--lcd-text-dim);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    font-variant-numeric: tabular-nums;
    font-size: 0.95rem;
  }
  .row.top3 .rank .num {
    color: var(--text);
  }
  .av {
    flex: 0 0 auto;
    width: 40px;
    height: 40px;
    border-radius: 999px;
    object-fit: cover;
    background: rgba(0, 0, 0, 0.36);
    border: 0;
    filter: saturate(0.86) contrast(1.04);
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    color: var(--lcd-text-bright);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    letter-spacing: 0.04em;
    text-shadow: var(--lcd-text-shadow);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .stats {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 0.1rem;
  }
  .score {
    color: var(--amber);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    font-size: 0.72rem;
    letter-spacing: 0.03em;
    font-variant-numeric: tabular-nums;
  }
  .score-value {
    font-size: 1rem;
  }
  .signals,
  .feedback {
    color: var(--lcd-text-dim);
    font-size: 0.72rem;
  }
  .feedback {
    font-size: 0.66rem;
    opacity: 0.84;
  }
  @media (max-width: 560px) {
    .wrap { padding: 0.45rem 0.45rem 3rem; }
    .lb-head,
    .track-panel,
    .ranking-panel { margin-bottom: 0.55rem; }
    .lb-head { min-height: 190px; padding: 1rem; }
    .sub { font-size: 0.88rem; }
    .score-rule { font-size: 0.72rem; }
    .ranking-panel { padding: 0.8rem; }
    .track-panel { padding: 0.8rem; }
    .track-head {
      display: block;
      margin-bottom: 0.65rem;
    }
    .track-head p { margin-top: 0.15rem; }
    .track-row {
      grid-template-columns: 2.25rem minmax(0, 1fr);
      gap: 0.5rem;
      min-height: 70px;
      padding-block: 0.7rem;
    }
    .track-votes {
      grid-column: 2;
      margin-top: 0.08rem;
    }
    .pod-slot { min-height: 166px; padding-inline: 0.25rem; }
    .pod-slot.pod-first { min-height: 180px; }
    .pod-av { width: 38px; height: 38px; }
    .pod-first .pod-av { width: 48px; height: 48px; }
    .pod-name { font-size: 0.72rem; }
    .pod-first .pod-name { font-size: 0.82rem; }
    .pod-signals,
    .pod-feedback { font-size: 0.58rem; }
    .row { min-height: 62px; gap: 0.55rem; padding-inline: 0.1rem; }
    .rank { min-width: 2.7rem; }
    .av { width: 36px; height: 36px; }
    .name { font-size: 0.88rem; }
    .signals { font-size: 0.66rem; }
    .feedback { font-size: 0.6rem; }
  }
</style>

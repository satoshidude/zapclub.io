<script lang="ts">
  import { npubEncode } from 'nostr-tools/nip19'
  import { fetchLeaderboard, type LeaderboardEntry } from '../nostr/leaderboard'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import { goUser } from '../router.svelte'

  let entries = $state<LeaderboardEntry[]>([])
  let total = $state(0)
  let loading = $state(true)

  $effect(() => {
    loading = true
    void fetchLeaderboard()
      .then((r) => {
        entries = r.top
        total = r.total
      })
      .finally(() => (loading = false))
  })

  const medal = (rank: number) => (rank === 1 ? '🥇' : rank === 2 ? '🥈' : rank === 3 ? '🥉' : '')
</script>

<div class="wrap">
  <header class="lb-head led-zone">
    <h1>TOP DJS Leaderboard</h1>
    <p class="sub">The most-zapped DJs on zapclub — ranked by sats received. Public, live, and earned on stage.</p>
  </header>

  <section class="ranking-panel led-zone" aria-live="polite">
    {#if loading}
      <p class="dim">Loading…</p>
    {:else if entries.length === 0}
      <p class="dim">No zaps ranked yet — be the first to tip a DJ on stage. ⚡</p>
    {:else}
      {#if entries.length >= 3}
        <div class="podium" aria-label="Top three DJs">
          {#each [entries[1], entries[0], entries[2]] as e (e.pubkey)}
            {@const p = useProfile(e.pubkey)}
            {@const npub = npubEncode(e.pubkey)}
            {@const isFirst = e.rank === 1}
            <button class="pod-slot" class:pod-first={isFirst} onclick={() => goUser(npub)}>
              <span class="pod-medal">{medal(e.rank)}</span>
              <img class="pod-av" src={avatarUrl(e.pubkey, p)} alt=""
                width={isFirst ? 52 : 40} height={isFirst ? 52 : 40} />
              <span class="pod-name">{displayName(e.pubkey, p)}</span>
              <span class="pod-sats">⚡ {e.sats.toLocaleString()}</span>
            </button>
          {/each}
        </div>
      {/if}
      <ol class="board">
        {#each entries.slice(3) as e (e.pubkey)}
          {@const p = useProfile(e.pubkey)}
          {@const npub = npubEncode(e.pubkey)}
          <li>
            <a class="row" class:top3={e.rank <= 3} href={`/user/${npub}`} onclick={(ev) => { ev.preventDefault(); goUser(npub) }}>
              <span class="rank">{medal(e.rank)}<span class="num">#{e.rank}</span></span>
              <img class="av" src={avatarUrl(e.pubkey, p)} alt="" width="40" height="40" />
              <span class="name">{displayName(e.pubkey, p)}</span>
              <span class="stats">
                <span class="sats">⚡ {e.sats.toLocaleString()}</span>
                <span class="from">from {e.zappers.toLocaleString()} {e.zappers === 1 ? 'person' : 'people'}</span>
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
    color: var(--lcd-text-bright);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: clamp(2rem, 4.4vw, var(--site-h1-max));
    font-weight: 400;
    letter-spacing: 0.01em;
    line-height: 1;
    text-shadow: var(--lcd-text-shadow);
  }
  .sub {
    max-width: 68ch;
    margin: 0;
    color: var(--lcd-text-soft);
    font-size: 0.96rem;
    line-height: 1.55;
  }
  .ranking-panel {
    padding: 1rem;
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
    min-height: 142px;
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
    min-height: 158px;
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
    border: 1px solid rgba(241, 243, 244, 0.34);
    filter: saturate(0.86) contrast(1.04);
  }
  .pod-first .pod-av { border-color: color-mix(in srgb, var(--amber) 72%, white 28%); }
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
  .pod-sats {
    color: var(--amber);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    font-size: 0.76rem;
    font-variant-numeric: tabular-nums;
  }
  .pod-first .pod-sats { font-size: 0.88rem; }
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
    border: 1px solid rgba(241, 243, 244, 0.3);
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
  .sats {
    color: var(--amber);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    font-variant-numeric: tabular-nums;
  }
  .from {
    color: var(--lcd-text-dim);
    font-size: 0.72rem;
  }
  @media (max-width: 560px) {
    .wrap { padding: 0.45rem 0.45rem 3rem; }
    .lb-head,
    .ranking-panel { margin-bottom: 0.55rem; }
    .lb-head { min-height: 190px; padding: 1rem; }
    .lb-head h1 { font-size: clamp(1.75rem, 9vw, 2.4rem); }
    .sub { font-size: 0.88rem; }
    .ranking-panel { padding: 0.8rem; }
    .pod-slot { min-height: 126px; padding-inline: 0.25rem; }
    .pod-slot.pod-first { min-height: 140px; }
    .pod-av { width: 38px; height: 38px; }
    .pod-first .pod-av { width: 48px; height: 48px; }
    .pod-name { font-size: 0.72rem; }
    .pod-first .pod-name { font-size: 0.82rem; }
    .row { min-height: 62px; gap: 0.55rem; padding-inline: 0.1rem; }
    .rank { min-width: 2.7rem; }
    .av { width: 36px; height: 36px; }
    .name { font-size: 0.88rem; }
  }
</style>

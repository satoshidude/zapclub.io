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
  <header class="lb-head">
    <h1>⚡ Zap leaderboard</h1>
    <p class="sub">The most-zapped DJs on zapclub — ranked by sats received. Public, live, and earned on stage.</p>
  </header>

  {#if loading}
    <p class="dim">Loading…</p>
  {:else if entries.length === 0}
    <p class="dim">No zaps ranked yet — be the first to tip a DJ on stage. ⚡</p>
  {:else}
    {#if entries.length >= 3}
      <div class="podium">
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
    {#if entries.length > 3}
      <div class="board-rows">
        {#each entries.slice(3, 5) as e (e.pubkey)}
          {@const p = useProfile(e.pubkey)}
          {@const npub = npubEncode(e.pubkey)}
          <button class="board-row" onclick={() => goUser(npub)}>
            <span class="b-rank">#{e.rank}</span>
            <img class="b-av" src={avatarUrl(e.pubkey, p)} alt="" width="28" height="28" />
            <span class="b-name">{displayName(e.pubkey, p)}</span>
            <span class="b-from">{e.zappers} {e.zappers === 1 ? 'zapper' : 'zappers'}</span>
            <span class="b-sats">⚡ {e.sats.toLocaleString()}</span>
          </button>
        {/each}
      </div>
    {/if}
    {#if entries.length > 5}
      <h2 class="rest-head">Full ranking</h2>
    {/if}
    <ol class="board">
      {#each entries.slice(5) as e (e.pubkey)}
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
</div>

<style>
  .wrap {
    max-width: 680px;
    margin: 0 auto;
    padding: 1.4rem 1rem 4rem;
  }
  .lb-head h1 {
    margin: 0;
    font-size: 1.6rem;
  }
  .sub {
    margin: 0.4rem 0 1.2rem;
    color: var(--text-dim);
    font-size: 0.9rem;
    line-height: 1.5;
  }
  .dim {
    color: var(--text-dim);
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
    gap: 0.5rem;
    margin-bottom: 0.6rem;
    align-items: end;
  }
  .pod-slot {
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
  .pod-slot:hover { border-color: var(--accent-2); }
  .pod-slot:active { transform: translateY(1px); }
  .pod-slot.pod-first {
    border-color: color-mix(in srgb, var(--amber) 55%, var(--border));
    background: radial-gradient(120% 140% at 50% 0%, rgba(245,166,35,0.13) 0%, transparent 65%), var(--bg-elev);
    padding-top: 1.1rem;
    padding-bottom: 0.9rem;
  }
  .pod-medal { font-size: 1.2rem; line-height: 1; }
  .pod-first .pod-medal { font-size: 1.5rem; }
  .pod-av {
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    border: 2px solid var(--border);
  }
  .pod-first .pod-av { border-color: var(--amber); }
  .pod-name {
    font-weight: 700;
    font-size: 0.8rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }
  .pod-first .pod-name { font-size: 0.95rem; }
  .pod-sats {
    color: var(--amber);
    font-weight: 800;
    font-size: 0.76rem;
    font-variant-numeric: tabular-nums;
  }
  .pod-first .pod-sats { font-size: 0.88rem; }
  /* Compact rows #4–#5 */
  .board-rows {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin-bottom: 0.6rem;
  }
  .board-row {
    display: flex;
    align-items: center;
    gap: 0.7rem;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.45rem 0.9rem 0.45rem 0.6rem;
    cursor: pointer;
    color: var(--text);
    transition: border-color 0.15s ease, transform 0.08s ease;
  }
  .board-row:hover { border-color: var(--accent-2); }
  .board-row:active { transform: translateY(1px); }
  .b-rank {
    flex: 0 0 auto;
    min-width: 1.8rem;
    text-align: center;
    font-size: 0.9rem;
    font-weight: 800;
    color: var(--text-dim);
  }
  .b-av {
    flex: 0 0 auto;
    width: 28px;
    height: 28px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
  }
  .b-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 700;
    font-size: 0.88rem;
  }
  .b-from {
    flex: 0 0 auto;
    color: var(--text-dim);
    font-size: 0.74rem;
  }
  .b-sats {
    flex: 0 0 auto;
    color: var(--amber);
    font-weight: 800;
    font-size: 0.85rem;
    font-variant-numeric: tabular-nums;
    min-width: 4.5rem;
    text-align: right;
  }
  @media (max-width: 460px) { .b-from { display: none; } }
  .rest-head {
    margin: 1.2rem 0 0.6rem;
    font-size: 1rem;
    font-weight: 700;
    color: var(--text-dim);
  }
  .board {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .row {
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
  .row:hover {
    border-color: var(--accent-2);
  }
  .row:active {
    transform: translateY(1px);
  }
  .row.top3 {
    border-color: var(--amber);
    background: linear-gradient(135deg, var(--bg-elev) 0%, var(--bg-elev-2) 100%);
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
    font-weight: 800;
    font-variant-numeric: tabular-nums;
    color: var(--text-dim);
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
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
  }
  .name {
    flex: 1;
    min-width: 0;
    font-weight: 700;
    overflow: hidden;
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
    font-weight: 800;
    font-variant-numeric: tabular-nums;
  }
  .from {
    color: var(--text-dim);
    font-size: 0.72rem;
  }
</style>

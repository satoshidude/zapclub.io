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
    <ol class="board">
      {#each entries as e (e.pubkey)}
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

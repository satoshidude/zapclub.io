<script lang="ts">
  import { sync, targetPosition } from '../../nostr/sync.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'

  // Reactive clock so the progress bar advances between now_playing events.
  let nowMs = $state(Date.now())
  $effect(() => {
    const t = setInterval(() => (nowMs = Date.now()), 500)
    return () => clearInterval(t)
  })

  const np = $derived(sync.live)
  const dj = $derived(np?.dj ?? '')
  const profile = $derived(dj ? useProfile(dj) : null)
  const pos = $derived.by(() => {
    void nowMs // re-evaluate on tick
    return np ? targetPosition() : 0
  })
  const pct = $derived(np && np.duration > 0 ? Math.min(100, (pos / np.duration) * 100) : 0)

  function fmt(s: number): string {
    if (!s || s < 0) return '0:00'
    const m = Math.floor(s / 60)
    const sec = Math.floor(s % 60)
    return `${m}:${sec.toString().padStart(2, '0')}`
  }
</script>

<div class="np card">
  {#if np}
    <div class="row">
      <span class="eq" aria-hidden="true"><i></i><i></i><i></i></span>
      <div class="info">
        <div class="title">{np.title || np.videoId}</div>
        <a class="dj" href={`/user/${dj}`} onclick={(e) => e.preventDefault()}>
          <img class="avatar" src={avatarUrl(dj, profile)} alt="" width="18" height="18" />
          {displayName(dj, profile)}
        </a>
      </div>
      <div class="time">{fmt(pos)}{np.duration ? ' / ' + fmt(np.duration) : ''}</div>
    </div>
    <div class="bar"><div class="fill" style:width="{pct}%"></div></div>
  {:else}
    <div class="idle">No DJ on stage — lobby is playing.</div>
  {/if}
</div>

<style>
  .np {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.8rem 1rem;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }
  .info {
    flex: 1;
    min-width: 0;
  }
  .title {
    font-weight: 700;
    font-size: 0.98rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dj {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    margin-top: 0.2rem;
    font-size: 0.78rem;
    color: var(--text-dim);
    text-decoration: none;
  }
  .avatar {
    width: 18px;
    height: 18px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
  }
  .time {
    flex: 0 0 auto;
    font-size: 0.78rem;
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
  }
  .bar {
    margin-top: 0.6rem;
    height: 4px;
    background: var(--border);
    border-radius: 999px;
    overflow: hidden;
  }
  .fill {
    height: 100%;
    background: linear-gradient(90deg, var(--accent), var(--accent-2));
    transition: width 0.4s linear;
  }
  .idle {
    color: var(--text-dim);
    font-size: 0.88rem;
    text-align: center;
  }
  /* Tiny equalizer animation. */
  .eq {
    display: inline-flex;
    align-items: flex-end;
    gap: 2px;
    height: 18px;
    flex: 0 0 auto;
  }
  .eq i {
    width: 3px;
    background: var(--accent);
    border-radius: 2px;
    animation: eq 0.9s ease-in-out infinite;
  }
  .eq i:nth-child(1) { height: 40%; animation-delay: 0s; }
  .eq i:nth-child(2) { height: 90%; animation-delay: 0.2s; }
  .eq i:nth-child(3) { height: 60%; animation-delay: 0.4s; }
  @keyframes eq {
    0%, 100% { transform: scaleY(0.4); }
    50% { transform: scaleY(1); }
  }
  @media (prefers-reduced-motion: reduce) {
    .eq i { animation: none; }
  }
</style>

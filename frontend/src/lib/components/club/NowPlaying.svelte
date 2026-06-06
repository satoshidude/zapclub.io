<script lang="ts">
  import { sync, targetPosition } from '../../nostr/sync.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { goUser } from '../../router.svelte'
  import { npubEncode } from 'nostr-tools/nip19'
  import { likes, likeTrack, unlikeTrack } from '../../nostr/likes.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import ZapButton from './ZapButton.svelte'

  // Optional "go on stage" action shown in the idle (lobby) state.
  let {
    onGoStage,
    stageLabel = '',
    clubId = '',
    clubName = '',
  }: {
    onGoStage?: () => void
    stageLabel?: string
    clubId?: string
    clubName?: string
  } = $props()

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

  const liked = $derived(!!np && likes.has(np.videoId))
  // Toggle: like, or un-like if already liked (removes the reaction via NIP-09).
  function toggleLike() {
    if (!np) return
    if (liked) void unlikeTrack(np.videoId)
    else void likeTrack({ videoId: np.videoId, title: np.title || np.videoId, clubId, clubName })
  }

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
        <div class="dj-row">
          <a class="dj" href={`/user/${npubEncode(dj)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(dj)) }}>
            <img class="avatar" src={avatarUrl(dj, profile)} alt="" width="18" height="18" />
            {displayName(dj, profile)}
          </a>
        </div>
      </div>
      <div class="right">
        <div class="right-actions">
          <button
            class="like"
            class:on={liked}
            onclick={toggleLike}
            disabled={!auth.canSign}
            title={liked ? 'Liked — tap to remove' : 'Like this track'}
          >🔥</button>
          <ZapButton club={clubId} />
        </div>
        <div class="time">{fmt(pos)}{np.duration ? ' / ' + fmt(np.duration) : ''}</div>
      </div>
    </div>
    <div class="bar"><div class="fill" style:width="{pct}%"></div></div>
  {:else}
    <div class="idle">
      No DJ on stage — lobby is playing.
      {#if onGoStage && stageLabel}
        <button class="stage-link" onclick={onGoStage}>{stageLabel}</button>
      {/if}
    </div>
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
  .dj-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-top: 0.2rem;
  }
  .dj {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    font-size: 0.78rem;
    color: var(--text-dim);
    text-decoration: none;
    min-width: 0;
  }
  .avatar {
    width: 18px;
    height: 18px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
  }
  .right {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 0.3rem;
  }
  .right-actions {
    display: flex;
    align-items: center;
    gap: 0.35rem;
  }
  .like {
    flex: 0 0 auto;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.12rem 0.4rem;
    font-size: 0.8rem;
    cursor: pointer;
    filter: grayscale(1);
    opacity: 0.7;
  }
  .like:hover:not(:disabled) {
    filter: none;
    opacity: 1;
    border-color: var(--amber);
  }
  .like.on {
    filter: none;
    opacity: 1;
    border-color: var(--amber);
  }
  .like:disabled {
    cursor: default;
  }
  .time {
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
  .stage-link {
    background: none;
    border: none;
    color: var(--accent);
    font-weight: 700;
    font-size: 0.88rem;
    cursor: pointer;
    padding: 0 0 0 0.25rem;
  }
  .stage-link:hover {
    text-decoration: underline;
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

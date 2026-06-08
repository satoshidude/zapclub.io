<script lang="ts">
  import { sync, targetPosition } from '../../nostr/sync.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { goUser } from '../../router.svelte'
  import { npubEncode } from 'nostr-tools/nip19'
  import { likes, likeTrack, unlikeTrack } from '../../nostr/likes.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import ZapButton from './ZapButton.svelte'
  import Player from './Player.svelte'
  import ComingNext from './ComingNext.svelte'

  // The video lives INSIDE this card: small on the left by default to save space, with a zoom
  // toggle that expands it to full content-width. The Player instance is never remounted on
  // toggle (only CSS changes) → no reload, playback continues.
  let {
    onGoStage,
    stageLabel = '',
    clubId = '',
    clubName = '',
    canHear = false,
    ctaText = '',
    onCta,
    onended,
    onerror,
  }: {
    onGoStage?: () => void
    stageLabel?: string
    clubId?: string
    clubName?: string
    canHear?: boolean
    ctaText?: string
    onCta?: () => void
    onended?: () => void
    onerror?: (videoId: string) => void
  } = $props()

  const ZOOM_KEY = 'zapclub:videoZoom'
  let zoomed = $state.raw((() => {
    try {
      return localStorage.getItem(ZOOM_KEY) === '1'
    } catch {
      return false
    }
  })())
  function toggleZoom() {
    zoomed = !zoomed
    try {
      localStorage.setItem(ZOOM_KEY, zoomed ? '1' : '0')
    } catch {
      /* ignore */
    }
  }

  // Reactive clock so the progress bar advances between now_playing events.
  let nowMs = $state(Date.now())
  $effect(() => {
    const t = setInterval(() => (nowMs = Date.now()), 500)
    return () => clearInterval(t)
  })

  const np = $derived(sync.live)
  // Show the artist as its own line. Server enrichment writes "Artist – Title" (en-dash), but
  // many raw YouTube titles use "Artist - Title" (hyphen) or an em-dash. Split on the FIRST
  // spaced dash of any kind. Requiring surrounding spaces avoids splitting hyphenated words
  // ("Hip-Hop", "Toni-L"); a bare song name (no spaced dash) stays whole, no artist line.
  const track = $derived.by(() => {
    const full = np?.title || np?.videoId || ''
    const m = full.match(/ [–—-] /)
    return m && m.index !== undefined && m.index > 0
      ? { artist: full.slice(0, m.index), title: full.slice(m.index + m[0].length) }
      : { artist: '', title: full }
  })
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

<div class="np card" class:zoomed>
  {#if np}<div class="zap-corner"><ZapButton club={clubId} /></div>{/if}
  <div class="np-main">
    <div class="video">
      <Player {canHear} {ctaText} {onCta} {onended} {onerror} compact={!zoomed} />
      <button class="zoom" onclick={toggleZoom} title={zoomed ? 'Shrink video' : 'Expand video to full width'} aria-label={zoomed ? 'Shrink video' : 'Expand video'}>
        {zoomed ? '⤡' : '⤢'}
      </button>
    </div>
    <div class="meta">
      {#if np}
        <div class="info">
          <div class="title-row">
            <span class="eq" aria-hidden="true"><i></i><i></i><i></i></span>
            <span class="title">{track.title}</span>
          </div>
          {#if track.artist}<div class="artist">{track.artist}</div>{/if}
          <div class="dj-row">
            <a class="dj" href={`/user/${npubEncode(dj)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(dj)) }}>
              <img class="avatar" src={avatarUrl(dj, profile)} alt="" width="18" height="18" />
              {displayName(dj, profile)}
            </a>
          </div>
        </div>
        <div class="meta-foot">
          <div class="right-actions">
            <button
              class="like"
              class:on={liked}
              onclick={toggleLike}
              disabled={!auth.canSign}
              title={liked ? 'Liked — tap to remove' : 'Like this track'}
            >🔥</button>
          </div>
          <div class="time">{fmt(pos)}{np.duration ? ' / ' + fmt(np.duration) : ''}</div>
        </div>
      {:else}
        <div class="idle">
          No DJ on stage — lobby is playing.
          {#if onGoStage && stageLabel}
            <button class="stage-link" onclick={onGoStage}>{stageLabel}</button>
          {/if}
        </div>
      {/if}
    </div>
  </div>
  {#if np}<div class="bar"><div class="fill" style:width="{pct}%"></div></div>{/if}
  <ComingNext />
</div>

<style>
  .np {
    position: relative;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.8rem 1rem;
  }
  /* Zap chip pinned to the top-right corner of the card. */
  .zap-corner {
    position: absolute;
    top: 0.55rem;
    right: 0.65rem;
    z-index: 5;
  }
  /* Keep the title/artist/dj clear of the corner chip (non-zoomed: meta sits top-right). */
  .np:not(.zoomed) .info {
    padding-right: 4.2rem;
  }
  .np-main {
    display: flex;
    gap: 0.8rem;
    align-items: stretch;
  }
  /* Small video on the left (default). Player fills the container width at 16:9. */
  .video {
    position: relative;
    flex: 0 0 40%;
    max-width: 190px;
    align-self: flex-start;
  }
  /* Zoomed: video on top, full content-width; meta below. */
  .np.zoomed .np-main {
    flex-direction: column;
  }
  .np.zoomed .video {
    flex-basis: auto;
    max-width: none;
    width: 100%;
  }
  .zoom {
    position: absolute;
    top: 5px;
    left: 5px;
    z-index: 3;
    width: 26px;
    height: 26px;
    display: grid;
    place-items: center;
    background: rgba(0, 0, 0, 0.6);
    border: 1px solid rgba(255, 255, 255, 0.25);
    border-radius: 7px;
    color: #fff;
    font-size: 0.85rem;
    cursor: pointer;
    line-height: 1;
  }
  .zoom:hover {
    background: rgba(0, 0, 0, 0.8);
    border-color: var(--accent);
  }
  .meta {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 0.4rem;
  }
  .np.zoomed .meta {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
  }
  .title-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    min-width: 0;
  }
  .info {
    min-width: 0;
  }
  .meta-foot {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .title {
    font-weight: 700;
    font-size: 0.98rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .artist {
    margin-top: 0.12rem;
    font-size: 0.82rem;
    color: var(--text-dim);
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

<script lang="ts">
  import { sync, targetPosition } from '../../nostr/sync.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { goUser } from '../../router.svelte'
  import { npubEncode } from 'nostr-tools/nip19'
  import { likes, likeTrack, unlikeTrack } from '../../nostr/likes.svelte'
  import { enrichMyTrackTitle, enrichMyTrackDuration, queues } from '../../nostr/queue.svelte'
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
  // Custom cover for the live track — looked up from the DJ's queue (the client already holds all
  // DJ queues for the round-robin preview), so no relay/now_playing change is needed. Shown over
  // the video for everyone in the club.
  const coverImage = $derived.by(() => {
    if (!np?.dj || !np?.videoId) return undefined
    return queues.get(np.dj)?.tracks.find((t) => t.videoId === np.videoId)?.image
  })

  // Channel name reported live by the YouTube embed (getVideoData) — no extraction, no bot gate.
  // Keyed to the videoId it belongs to, so it persists across now_playing heartbeats (same track,
  // new object) and is ignored the moment the track changes — until the player reports the new one.
  let ytMeta = $state({ vid: '', author: '' })

  // Derive the artist from a YouTube channel ONLY when it carries a music marker ("Artist -
  // Topic", "ArtistVEVO", "Artist Official") — mirrors the relay's artistFromChannel. A plain
  // uploader channel yields "" (better no artist than a random uploader name).
  function artistFromChannel(ch: string): string {
    const c = (ch ?? '').trim()
    if (!c || c === 'NA') return ''
    const low = c.toLowerCase()
    for (const m of [' - topic', ' official', ' officiel', 'vevo']) {
      if (low.endsWith(m)) return c.slice(0, c.length - m.length).trim()
    }
    return ''
  }

  // The artist belongs IN the title ("Artist - Title"), shown the same everywhere (card, Live
  // Set, playlists). Server-enriched titles already carry a spaced dash → shown as-is. For a
  // bare title we prepend the artist the embed reports (getVideoData channel, e.g. "X - Topic").
  const channelArtist = $derived(
    np?.videoId && ytMeta.vid === np.videoId ? artistFromChannel(ytMeta.author) : '',
  )
  const displayTitle = $derived.by(() => {
    const full = np?.title || np?.videoId || ''
    if (/ [–—-] /.test(full)) return full // already has an artist
    return channelArtist ? `${channelArtist} - ${full}` : full
  })

  // Marquee: when the title is wider than its box, scroll it slowly back and forth instead of
  // ellipsizing. Measure the overflow (re-measured on title/layout change via ResizeObserver).
  let titleEl = $state<HTMLElement>()
  let scrollPx = $state(0)
  const marqueeDur = $derived(Math.max(9, Math.round(scrollPx / 14) + 6)) // slower for longer titles
  $effect(() => {
    void displayTitle // re-measure when the title changes
    const el = titleEl
    if (!el) return
    const measure = () => {
      if (titleEl) scrollPx = Math.max(0, titleEl.scrollWidth - titleEl.clientWidth)
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
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
  {#if np}
    <div class="np-head">
      <ZapButton club={clubId} />
      <button
        class="like"
        class:on={liked}
        onclick={toggleLike}
        disabled={!auth.canSign}
        title={liked ? 'Liked — tap to remove' : 'Like this track'}
      >🔥</button>
    </div>
  {/if}
  <div class="np-main">
    <div class="video">
      <Player {canHear} {ctaText} {onCta} {onended} {onerror} compact={!zoomed} onmeta={(author) => {
        if (!np) return
        ytMeta = { vid: np.videoId, author }
        // If this is MY track and its stored title is still bare, fold the artist in so it
        // persists into the Live Set / playlists too (not just live in this card).
        const a = artistFromChannel(author)
        if (a && np.dj === auth.pubkey && np.title && !/ [–—-] /.test(np.title)) {
          void enrichMyTrackTitle(clubId, np.videoId, `${a} - ${np.title}`)
        }
      }} onduration={(secs) => {
        if (np && np.dj === auth.pubkey) void enrichMyTrackDuration(clubId, np.videoId, secs)
      }} />
      {#if coverImage}<img class="cover-img" src={coverImage} alt="" />{/if}
      <button class="zoom" onclick={toggleZoom} title={zoomed ? 'Shrink video' : 'Expand video to full width'} aria-label={zoomed ? 'Shrink video' : 'Expand video'}>
        {zoomed ? '⤡' : '⤢'}
      </button>
    </div>
    <div class="meta">
      {#if np}
        <div class="info">
          <div class="title-row">
            <span class="eq" aria-hidden="true"><i></i><i></i><i></i></span>
            <span class="title" bind:this={titleEl} class:scroll={scrollPx > 0} style:--scroll="{scrollPx}px" style:--marquee-dur="{marqueeDur}s">
              <span class="title-text">{displayTitle}</span>
            </span>
          </div>
        </div>
        <div class="dj-row">
          <a class="dj" href={`/user/${npubEncode(dj)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(dj)) }}>
            <img class="avatar" src={avatarUrl(dj, profile)} alt="" width="18" height="18" />
            {displayName(dj, profile)}
          </a>
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
  {#if np}
    <div class="bar-row">
      <div class="bar"><div class="fill" style:width="{pct}%"></div></div>
      <span class="time">{fmt(pos)}{np.duration ? ' / ' + fmt(np.duration) : ''}</span>
    </div>
  {/if}
  <ComingNext />
</div>

<style>
  /* No border/padding/bg of its own — it sits inside the stage card; this saves vertical space
     (the stage card already provides the frame + padding). */
  .np {
    background: transparent;
    border: none;
    padding: 0;
  }
  /* Actions top-right of the block: zap (left) + like (right). */
  .np-head {
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: 0.4rem;
    margin-bottom: 0.4rem;
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
  /* Custom cover shown over the video (the YouTube embed keeps playing the audio underneath).
     pointer-events:none → taps still reach the player (tap-for-sound); below the zoom button. */
  .cover-img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: var(--radius-sm);
    pointer-events: none;
    z-index: 1;
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
  .dj-row {
    display: flex;
    align-items: center;
    margin-top: 0.2rem;
  }
  .dj {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    font-size: 0.8rem;
    color: var(--text-dim);
    text-decoration: none;
    min-width: 0;
    overflow: hidden;
  }
  .dj:hover {
    color: var(--accent-2);
  }
  .avatar {
    flex: 0 0 auto;
    width: 18px;
    height: 18px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
  }
  .title {
    flex: 1;
    min-width: 0;
    font-weight: 700;
    font-size: 0.98rem;
    overflow: hidden;
    white-space: nowrap;
  }
  .title-text {
    display: inline-block;
    white-space: nowrap;
  }
  /* Title wider than its box → scroll it slowly back and forth (marquee) instead of clipping. */
  .title.scroll .title-text {
    animation: title-marquee var(--marquee-dur, 12s) ease-in-out infinite;
  }
  @keyframes title-marquee {
    0%, 12% { transform: translateX(0); }
    50%, 62% { transform: translateX(calc(-1 * var(--scroll, 0px))); }
    100% { transform: translateX(0); }
  }
  @media (prefers-reduced-motion: reduce) {
    .title.scroll .title-text {
      animation: none;
      max-width: 100%;
      overflow: hidden;
      text-overflow: ellipsis;
    }
  }
  .like {
    flex: 0 0 auto;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.3rem 0.6rem;
    min-height: 36px;
    font-size: 0.95rem;
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
    flex: 0 0 auto;
    font-size: 0.78rem;
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
  }
  /* Progress bar at the bottom, with the runtime attached on the right. */
  .bar-row {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    margin-top: 0.6rem;
  }
  .bar {
    flex: 1;
    min-width: 0;
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

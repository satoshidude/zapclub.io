<script lang="ts">
  import { onDestroy } from 'svelte'
  import { sync, targetPosition } from '../../nostr/sync.svelte'
  import { enrichMyTrackTitle, enrichMyTrackDuration } from '../../nostr/queue.svelte'
  import { useProfile, displayName } from '../../nostr/profiles.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import { likes, likeTrack, unlikeTrack } from '../../nostr/likes.svelte'
  import Player from './Player.svelte'
  import ZapButton from './ZapButton.svelte'
  import {
    nextAutoVideoPreviewPhase,
    type AutoVideoPreviewPhase,
  } from './autoVideoPreview'

  let {
    onGoStage,
    stageLabel = '',
    clubId = '',
    clubName = '',
    clubImage = '',
    canHear = false,
    ctaText = '',
    onCta,
    onended,
    onerror,
    hasDjOnStage = false,
  }: {
    onGoStage?: () => void
    stageLabel?: string
    clubId?: string
    clubName?: string
    clubImage?: string
    canHear?: boolean
    ctaText?: string
    onCta?: () => void
    onended?: () => void
    onerror?: (videoId: string) => void
    hasDjOnStage?: boolean
  } = $props()

  type PlayerControls = {
    toggleAudio: () => void
    setAudioVolume: (value: number) => void
    toggleVideoFullscreen: () => void
  }

  type PlayerControlState = {
    ready: boolean
    muted: boolean
    volume: number
    fullscreen: boolean
    playing: boolean
  }

  const MINI_VIDEO_MASK_MS = 8000

  let playerRef = $state<PlayerControls>()
  let controls = $state<PlayerControlState>({ ready: false, muted: true, volume: 70, fullscreen: false, playing: false })
  let liking = $state(false)
  let failedVideo = $state('')
  let videoReady = $state(false)
  let videoWide = $state(false)
  let autoPreviewTrack = $state('')
  let autoPreviewPhase = $state<AutoVideoPreviewPhase>('done')
  let miniVideoMaskVisible = $state(false)
  let miniVideoMaskTrack = $state('')
  let miniVideoMaskTimer: ReturnType<typeof setTimeout> | undefined

  const np = $derived(sync.live)
  const djProfile = $derived(np?.dj ? useProfile(np.dj) : null)
  const djName = $derived(np?.dj ? displayName(np.dj, djProfile) : '')

  function showMiniVideoMask() {
    miniVideoMaskVisible = true
    if (miniVideoMaskTimer) clearTimeout(miniVideoMaskTimer)
    miniVideoMaskTimer = setTimeout(() => {
      miniVideoMaskVisible = false
      miniVideoMaskTimer = undefined
    }, MINI_VIDEO_MASK_MS)
  }

  function handleControlState(state: PlayerControlState) {
    controls = state
  }

  function toggleAudio() {
    if (controls.muted) showMiniVideoMask()
    playerRef?.toggleAudio()
  }

  $effect(() => {
    const videoId = np?.videoId || ''
    if (!videoId || videoId === miniVideoMaskTrack) return
    miniVideoMaskTrack = videoId
    showMiniVideoMask()
  })

  onDestroy(() => {
    if (miniVideoMaskTimer) clearTimeout(miniVideoMaskTimer)
  })

  let nowMs = $state(Date.now())
  $effect(() => {
    const t = setInterval(() => (nowMs = Date.now()), 500)
    return () => clearInterval(t)
  })

  const pos = $derived.by(() => {
    void nowMs
    return np ? targetPosition() : 0
  })

  // Show one short automatic video preview per track. The shared playback position keeps late
  // joiners and paused tracks correct; a manual toggle takes control for the rest of that track.
  $effect(() => {
    const trackKey = np ? `${np.videoId}:${np.startedAt}` : ''
    const position = pos
    const reduceMotion = typeof window !== 'undefined'
      && window.matchMedia('(prefers-reduced-motion: reduce)').matches

    if (trackKey !== autoPreviewTrack) {
      autoPreviewTrack = trackKey
      videoWide = false
      autoPreviewPhase = trackKey && !reduceMotion ? 'waiting' : 'done'
    }

    if (!trackKey || reduceMotion) return

    const nextPhase = nextAutoVideoPreviewPhase(autoPreviewPhase, position)
    if (nextPhase === autoPreviewPhase) return
    autoPreviewPhase = nextPhase
    videoWide = nextPhase === 'open'
  })

  function toggleVideoWide() {
    autoPreviewPhase = 'done'
    videoWide = !videoWide
  }

  const remaining = $derived(np?.duration ? Math.max(0, np.duration - pos) : 0)
  const liked = $derived(!!np?.videoId && likes.has(np.videoId))
  const audioOnly = $derived(!!np && failedVideo === np.videoId)
  const miniArtworkVisible = $derived(!videoWide && (miniVideoMaskVisible || audioOnly))
  const currentCover = $derived.by(() => {
    if (!np) return ''
    if (miniArtworkVisible) return clubImage
    if (failedVideo === np.videoId) {
      return clubImage || `https://i.ytimg.com/vi/${np.videoId}/mqdefault.jpg`
    }
    return !videoReady ? `https://i.ytimg.com/vi/${np.videoId}/mqdefault.jpg` : ''
  })

  $effect(() => {
    const videoId = np?.videoId
    const isPlaying = controls.playing
    videoReady = false
    if (!videoId || !isPlaying) return
    const timer = setTimeout(() => (videoReady = true), 1600)
    return () => clearTimeout(timer)
  })

  let ytMeta = $state({ vid: '', author: '' })

  function artistFromChannel(channel: string): string {
    const value = (channel ?? '').trim()
    if (!value || value === 'NA') return ''
    const lower = value.toLowerCase()
    for (const marker of [' - topic', ' official', ' officiel', 'vevo']) {
      if (lower.endsWith(marker)) return value.slice(0, value.length - marker.length).trim()
    }
    return ''
  }

  const channelArtist = $derived(
    np?.videoId && ytMeta.vid === np.videoId ? artistFromChannel(ytMeta.author) : '',
  )
  const parts = $derived.by(() => {
    const full = np?.title || np?.videoId || ''
    const match = full.match(/^(.+?) [–—-] (.+)$/)
    if (match) return { artist: match[1], title: match[2] }
    return { artist: channelArtist, title: full }
  })

  function fmt(seconds: number): string {
    if (!seconds || seconds < 0) return '0:00'
    const minutes = Math.floor(seconds / 60)
    const rest = Math.floor(seconds % 60)
    return `${minutes}:${rest.toString().padStart(2, '0')}`
  }

  async function toggleLike() {
    if (!np || !auth.canSign || liking) return
    liking = true
    try {
      if (liked) await unlikeTrack(np.videoId)
      else await likeTrack({ videoId: np.videoId, title: np.title, clubId, clubName })
    } finally {
      liking = false
    }
  }
</script>

<section class="lcd-shell led-zone" class:idle={!np} class:video-wide={videoWide} aria-label="Club player">
  {#if np}
    <div class="lcd-status lcd-card-heading player-status">
      <span class="live-label lcd-card-title">ON AIR</span>
      <span>-{fmt(remaining)} / {fmt(np.duration)}</span>
    </div>
  {/if}

  <div class="lcd-media">
    <div class="video-surface">
      <Player
        bind:this={playerRef}
        {canHear}
        {onended}
        onerror={(videoId) => {
          failedVideo = videoId
          onerror?.(videoId)
        }}
        compact={true}
        embedded={true}
        cover={currentCover}
        ledCover={miniArtworkVisible}
        poster={clubImage}
        oncontrolstate={handleControlState}
        onmeta={(author) => {
          if (!np) return
          ytMeta = { vid: np.videoId, author }
          const artist = artistFromChannel(author)
          if (artist && np.dj === auth.pubkey && np.title && !/ [–—-] /.test(np.title)) {
            void enrichMyTrackTitle(clubId, np.videoId, `${artist} - ${np.title}`)
          }
        }}
        onduration={(seconds) => {
          if (np && np.dj === auth.pubkey) void enrichMyTrackDuration(clubId, np.videoId, seconds)
        }}
      />
    </div>
    {#if np && !videoWide && (miniVideoMaskVisible || audioOnly)}
      <div
        class="mini-video-status"
        class:audio-only={audioOnly}
        role="status"
        aria-label={audioOnly ? 'Audio only' : 'Video syncing'}
      >
        <span class="mini-video-status-label">{audioOnly ? 'AUDIO ONLY' : 'VIDEO SYNC'}</span>
        <span class="mini-video-status-bars" aria-hidden="true">
          {#each [0, 1, 2, 3, 4, 5, 6, 7] as index}
            <span style={`--sync-index: ${index}`}></span>
          {/each}
        </span>
      </div>
    {/if}
  </div>

  <div class="lcd-content" class:lobby-content={!np}>
    <div class="scanlines" aria-hidden="true"></div>
    {#if np}
      <div class="lcd-track-wrap">
        <div class="lcd-track" class:scroll={parts.title.length > 28}>
          <span>{parts.title}</span>
          {#if parts.title.length > 28}<span aria-hidden="true">{parts.title}</span>{/if}
        </div>
      </div>

      <div class="lcd-byline">
        {#if parts.artist}<span>{parts.artist}</span>{/if}
        <span class="lcd-separator">{parts.artist ? '—' : ''}</span>
        <span>{np.auto ? 'Auto DJ' : 'Live set'}</span>
      </div>

      <div class="lcd-controls">
        <div class="lcd-dj-line">
          <ZapButton
            club={clubId}
            iconOnly={true}
            showName={true}
            showSelf={true}
            allowSelfZap={true}
            hideIcon={true}
            iconLabel={`⚡ ${djName}`}
          />
        </div>
        <div class="volume-cluster">
          <button
            class="lcd-audio"
            class:muted={controls.muted}
            onclick={toggleAudio}
            disabled={!controls.ready || !canHear}
            aria-label={controls.muted ? 'Unmute' : 'Mute'}
            title={controls.muted ? 'Unmute' : 'Mute'}
          >
            {#if controls.muted}
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 9v6h4l5 4V5L7 9H3z"></path><path d="m16 9 5 5m0-5-5 5"></path></svg>
            {:else}
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M3 9v6h4l5 4V5L7 9H3z"></path>
                {#if controls.volume > 0}<path d="M16 8.8a4.5 4.5 0 0 1 0 6.4"></path>{/if}
                {#if controls.volume >= 50}<path d="M18.7 6a8 8 0 0 1 0 12"></path>{/if}
              </svg>
            {/if}
          </button>
          <input
            class="lcd-volume"
            type="range"
            min="0"
            max="100"
            value={controls.volume}
            style={`--volume: ${controls.muted ? 0 : controls.volume}%`}
            oninput={(event) => playerRef?.setAudioVolume(+(event.currentTarget as HTMLInputElement).value)}
            disabled={!controls.ready || !canHear}
            aria-label="Volume"
          />
        </div>

        <div class="right-controls">
          <button class="lcd-icon" onclick={toggleVideoWide} aria-label={videoWide ? 'Hide video' : 'Show video'} title={videoWide ? 'Hide video' : 'Show video'}>
            {#if videoWide}
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 5h18v14H3z"></path><path d="m8.5 8.5 7 7m0-7-7 7"></path></svg>
            {:else}
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 5h18v14H3z"></path><path d="m10 9 5 3-5 3z"></path></svg>
            {/if}
          </button>
          <button class="lcd-icon" onclick={() => playerRef?.toggleVideoFullscreen()} disabled={!controls.ready} aria-label="Show video fullscreen" title="Show video fullscreen">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5"></path></svg>
          </button>
          <button class="lcd-icon like" class:active={liked} onclick={toggleLike} disabled={!auth.canSign || liking} aria-label={liked ? 'Unlike track' : 'Like track'} title={liked ? 'Unlike track' : 'Like track'}>
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 20.5c-4-2.6-9-6.8-9-11.2C3 6.1 5.1 4 7.8 4c1.5 0 3 .8 4.2 2.1C13.2 4.8 14.7 4 16.2 4 18.9 4 21 6.1 21 9.3c0 4.4-5 8.6-9 11.2z"></path></svg>
          </button>
          <a class="lcd-icon youtube-link" href={`https://youtu.be/${np.videoId}`} target="_blank" rel="noopener noreferrer" aria-label="Open video on YouTube" title="Open on YouTube">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M14 4h6v6M20 4l-9 9"></path><path d="M18 13v6H5V6h6"></path></svg>
          </a>
        </div>
      </div>
    {:else}
      <div class="lcd-status lcd-card-heading">
        <span><strong class="lcd-card-title">{clubName || 'Zapclub'}</strong></span>
      </div>
      <div class="lobby-copy">
        <strong>{hasDjOnStage ? 'DJ is loading tracks' : 'No DJ on stage'}</strong>
        <span>{hasDjOnStage ? 'Playback starts automatically.' : 'Take the first slot and start a set.'}</span>
      </div>
      {#if onGoStage && stageLabel}
        <button class="enter-stage" onclick={onGoStage}>{stageLabel.replace(' →', '')}<span aria-hidden="true">→</span></button>
      {/if}
    {/if}
  </div>

  {#if !canHear && ctaText}
    <button class="lcd-cta" onclick={() => onCta?.()}>{ctaText}</button>
  {/if}
</section>

<style>
  .lcd-shell {
    --player-led-surface:
      radial-gradient(circle, rgba(241, 243, 244, 0.045) 0 0.55px, transparent 0.75px) 0 0 / 4px 4px;
    position: relative;
    display: grid;
    grid-template-areas:
      'status status'
      'media content';
    grid-template-columns: minmax(170px, 220px) minmax(0, 1fr);
    grid-template-rows: 36px auto;
    overflow: hidden;
    height: auto;
    border: 0;
    border-radius: 0;
    color: var(--lcd-text);
    background: var(--player-led-surface);
    box-shadow: none;
    font-family: 'DotGothic16', ui-monospace, monospace;
    text-shadow: var(--lcd-text-shadow);
  }
  .lcd-shell.idle {
    grid-template-areas: 'content';
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr);
    height: 158px;
    color: var(--lcd-text);
    border-color: transparent;
    box-shadow: none;
    font-family: 'DotGothic16', ui-monospace, monospace;
    text-shadow: var(--lcd-text-shadow);
  }
  .scanlines {
    position: absolute;
    inset: 0;
    z-index: 0;
    display: none;
    pointer-events: none;
  }
  .idle .scanlines { display: none; }
  .lcd-media {
    grid-area: media;
    position: relative;
    z-index: 2;
    display: block;
    width: 100%;
    height: auto;
    overflow: hidden;
    opacity: 1;
    pointer-events: auto;
    background:
      linear-gradient(90deg, color-mix(in srgb, var(--lcd-text) 4%, transparent), transparent 82%),
      var(--player-led-surface);
  }
  .idle .lcd-media { display: none; }
  .video-surface {
    display: block;
    width: 100%;
    height: 100%;
    min-height: 0;
  }
  .mini-video-status {
    position: absolute;
    inset: 0;
    z-index: 3;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 9px;
    color: var(--lcd-text);
    background:
      repeating-linear-gradient(0deg, rgba(143, 197, 255, 0.025) 0 1px, transparent 1px 4px),
      radial-gradient(circle at 50% 50%, rgba(50, 118, 181, 0.14), transparent 62%),
      linear-gradient(180deg, rgba(2, 5, 10, 0.08), rgba(2, 5, 10, 0.44));
    pointer-events: none;
  }
  .mini-video-status-label {
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 11px;
    font-weight: 400;
    line-height: 1;
    letter-spacing: 0.14em;
    text-shadow: var(--lcd-text-shadow);
  }
  .mini-video-status.audio-only {
    background:
      repeating-linear-gradient(0deg, rgba(143, 197, 255, 0.025) 0 1px, transparent 1px 4px),
      linear-gradient(180deg, rgba(2, 5, 10, 0.08), rgba(2, 5, 10, 0.52));
  }
  .mini-video-status-bars {
    display: grid;
    grid-template-columns: repeat(8, 6px);
    align-items: end;
    gap: 3px;
    height: 19px;
  }
  .mini-video-status-bars span {
    width: 6px;
    height: 5px;
    background: currentColor;
    opacity: 0.18;
    animation: mini-video-sync 1.2s steps(1, end) infinite;
    animation-delay: calc(var(--sync-index) * -0.12s);
  }
  .video-wide .video-surface { display: block; }
  .lcd-shell.video-wide {
    height: auto;
    grid-template-areas:
      'status'
      'media'
      'content';
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: 36px auto auto;
    background: #03050a;
  }
  .video-wide .lcd-media {
    position: relative;
    z-index: 2;
    width: 100%;
    height: auto;
    aspect-ratio: 16 / 9;
    opacity: 1;
    pointer-events: auto;
    border-right: 0;
    border-bottom: 1px solid rgba(207, 233, 255, 0.24);
  }
  .video-wide .video-surface { display: block; }
  .video-wide .lcd-content {
    display: flex;
    height: 160px;
    padding-bottom: 28px;
    background: transparent;
  }
  .lcd-content {
    grid-area: content;
    position: relative;
    z-index: 2;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    height: auto;
    padding: 13px 15px 22px;
    overflow: hidden;
    background: var(--player-led-surface);
  }
  .lcd-content > :not(.scanlines) { position: relative; z-index: 1; }
  .lcd-status { display: flex; align-items: baseline; justify-content: space-between; color: var(--lcd-text-dim); font-size: 11px; letter-spacing: 0.06em; text-transform: uppercase; }
  .lcd-status.lcd-card-heading { margin-bottom: 4px; padding-bottom: 3px; }
  .player-status.lcd-card-heading {
    grid-area: status;
    position: relative;
    z-index: 3;
    height: 100%;
    margin: 0;
    padding: 7px 15px 5px;
  }
  .lcd-status > span:first-child { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .lcd-status strong { color: var(--lcd-text); font-weight: 400; }
  .live-label { color: var(--accent); }
  .lcd-status > span:last-child { flex: 0 0 auto; margin-left: 12px; color: var(--lcd-text); font-variant-numeric: tabular-nums; }
  .lcd-track-wrap { flex: 0 0 auto; width: 100%; overflow: hidden; white-space: nowrap; }
  .lcd-track { display: inline-flex; min-width: 100%; margin-top: 0; font-size: clamp(20px, 2.4vw, 27px); line-height: 1; letter-spacing: 0.01em; }
  .lcd-track span { padding-right: 48px; }
  .lcd-track.scroll { animation: lcd-marquee 10s linear infinite; }
  .lcd-byline { display: flex; gap: 7px; min-height: 20px; margin-top: 9px; color: var(--accent); font-size: 17px; }
  .lcd-dj-line {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    margin-right: auto;
  }
  .lcd-dj-line :global(.zap-mini.icon-only.with-name) {
    width: auto;
    max-width: min(100%, 190px);
    height: auto;
    padding: 0;
    overflow: hidden;
    color: #f4e04d;
  }
  .lcd-dj-line :global(.icon-dj-copy),
  .lcd-dj-line :global(.icon-dj-name) {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .lcd-separator { color: var(--lcd-text-dim); }
  .lcd-controls { display: flex; align-items: center; justify-content: flex-end; gap: 18px; margin-top: 10px; }
  .volume-cluster, .right-controls { display: flex; align-items: center; gap: 10px; }
  .volume-cluster {
    color: var(--lcd-text);
    filter: drop-shadow(0 0 2px rgba(90, 160, 255, 0.42));
    text-shadow: var(--lcd-text-shadow);
  }
  .right-controls { gap: 14px; }
  .lcd-audio {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    padding: 0;
    border: 0;
    color: inherit;
    background: transparent;
  }
  .lcd-audio.muted {
    color: var(--lcd-text-bright);
  }
  .lcd-audio:hover:not(:disabled) { color: #fff; }
  .lcd-audio:disabled { opacity: 0.4; cursor: default; }
  .lcd-audio svg { width: 26px; height: 26px; fill: none; stroke: currentColor; stroke-width: 2.45; stroke-linecap: round; stroke-linejoin: round; }
  .lcd-icon { display: grid; place-items: center; width: 30px; height: 30px; padding: 2px; border: 0; color: currentColor; background: transparent; filter: drop-shadow(0 0 2px rgba(235, 241, 244, 0.38)); }
  .lcd-icon:hover:not(:disabled), .lcd-icon.active { color: #fff; }
  .lcd-icon:disabled { opacity: 0.38; cursor: default; }
  .lcd-icon.like:disabled { opacity: 1; color: inherit; }
  .lcd-icon svg { width: 24px; height: 24px; fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }
  .youtube-link { text-decoration: none; }
  .lcd-icon.like.active svg { fill: currentColor; }
  .lcd-volume {
    width: 104px;
    height: 20px;
    margin: 0;
    appearance: none;
    background:
      repeating-linear-gradient(90deg, color-mix(in srgb, var(--lcd-text) 96%, transparent) 0 7px, transparent 7px 10px) left center / var(--volume) 12px no-repeat,
      repeating-linear-gradient(90deg, color-mix(in srgb, var(--lcd-text) 16%, transparent) 0 7px, transparent 7px 10px) left center / 100% 12px no-repeat;
    cursor: pointer;
  }
  .lcd-volume:focus-visible { outline: 1px solid var(--lcd-text); outline-offset: 3px; }
  .lcd-volume::-webkit-slider-thumb { appearance: none; width: 3px; height: 18px; border: 1px solid var(--lcd-text); background: rgba(27, 31, 35, 0.78); }
  .lcd-volume::-moz-range-thumb { width: 3px; height: 18px; border: 1px solid var(--lcd-text); border-radius: 0; background: rgba(27, 31, 35, 0.78); }
  .idle .lcd-status { border-color: var(--border); color: var(--text-dim); }
  .idle .lcd-status strong { color: var(--text); }
  .lobby-copy { display: flex; flex-direction: column; gap: 2px; min-width: 0; margin-top: 10px; }
  .lobby-copy strong { color: var(--text); font-size: 1rem; line-height: 1.2; }
  .lobby-copy span { overflow: hidden; color: var(--text-dim); font-size: 0.76rem; text-overflow: ellipsis; white-space: nowrap; }
  .enter-stage { display: inline-flex; align-items: center; justify-content: center; gap: 11px; align-self: flex-start; min-height: 32px; margin-top: auto; padding: 0 16px; border: 1px solid var(--accent); border-radius: 8px; color: var(--accent); background: rgba(74, 222, 94, 0.06); font-weight: 600; }
  .enter-stage:hover { background: rgba(74, 222, 94, 0.12); }
  .lcd-cta { position: absolute; z-index: 4; right: 13px; bottom: 11px; min-height: 34px; padding: 0 13px; border: 1px solid #cfe9ff; border-radius: 7px; color: #cfe9ff; background: rgba(9, 28, 60, 0.88); font-family: inherit; }

  @keyframes lcd-marquee {
    from { transform: translateX(0); }
    to { transform: translateX(-50%); }
  }

  @keyframes mini-video-sync {
    0%, 100% { height: 5px; opacity: 0.18; }
    25% { height: 19px; opacity: 1; }
    50% { height: 11px; opacity: 0.52; }
  }

  @media (max-width: 560px) {
    .lcd-shell {
      grid-template-columns: minmax(72px, 22%) minmax(0, 1fr);
      grid-template-rows: 32px auto;
      height: auto;
    }
    .lcd-shell.idle {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: minmax(0, 1fr);
      height: 126px;
    }
    .lcd-shell.video-wide { grid-template-rows: 32px auto auto; }
    .lcd-content { padding: 13px 7px 16px; }
    .video-wide .lcd-content { height: 130px; padding-bottom: 22px; }
    .lcd-status { font-size: 9px; }
    .player-status.lcd-card-heading { padding: 5px 9px 4px; }
    .lcd-track { margin-top: 0; font-size: 17px; }
    .lcd-byline { font-size: 16px; margin-top: 8px; overflow: hidden; white-space: nowrap; }
    .lcd-controls { justify-content: space-between; gap: 0; margin-top: 8px; }
    .volume-cluster, .right-controls { gap: 0; }
    .lcd-audio { width: 44px; height: 44px; }
    .lcd-audio svg { width: 25px; height: 25px; }
    .lcd-icon { width: 44px; height: 44px; padding: 8px; }
    .lcd-icon svg { width: 24px; height: 24px; }
    .lcd-volume { display: none; }
    .lobby-copy { margin-top: 7px; }
    .lobby-copy strong { font-size: 0.84rem; }
    .lobby-copy span { display: none; }
    .enter-stage { min-height: 28px; padding: 0 10px; font-size: 0.72rem; }
    .lcd-cta { right: 8px; bottom: 8px; min-height: 28px; padding: 0 9px; font-size: 0.7rem; }
  }

  @media (prefers-reduced-motion: reduce) {
    .lcd-track.scroll { max-width: 100%; overflow: hidden; text-overflow: ellipsis; animation: none; }
    .lcd-track.scroll span + span { display: none; }
    .mini-video-status-bars span { height: 11px; opacity: 0.72; animation: none; }
  }
</style>

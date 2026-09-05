<script lang="ts">
  import { sync, targetPosition } from '../../nostr/sync.svelte'
  import { enrichMyTrackTitle, enrichMyTrackDuration } from '../../nostr/queue.svelte'
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

  let playerRef = $state<PlayerControls>()
  let controls = $state({ ready: false, muted: true, volume: 70, fullscreen: false, playing: false })
  let liking = $state(false)
  let failedVideo = $state('')
  let videoReady = $state(false)
  let videoWide = $state(false)
  let autoPreviewTrack = $state('')
  let autoPreviewPhase = $state<AutoVideoPreviewPhase>('done')

  const np = $derived(sync.live)

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
  const currentCover = $derived.by(() => {
    if (!np || !videoWide) return ''
    return failedVideo === np.videoId || !videoReady
      ? `https://i.ytimg.com/vi/${np.videoId}/mqdefault.jpg`
      : ''
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
  <div class="lcd-media">
    <div class="video-surface" aria-hidden={!videoWide}>
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
        poster={clubImage}
        oncontrolstate={(state) => (controls = state)}
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
  </div>

  <div class="lcd-content" class:lobby-content={!np}>
    <div class="scanlines" aria-hidden="true"></div>
    {#if np}
      <div class="lcd-status lcd-card-heading">
        <span class="dj-status"><ZapButton club={clubId} iconOnly={true} showName={true} showSelf={true} allowSelfZap={true} /><span class="live-label">ON AIR</span></span>
        <span>-{fmt(remaining)} / {fmt(np.duration)}</span>
      </div>

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
        <div class="volume-cluster">
          <button
            class="lcd-audio"
            class:muted={controls.muted}
            onclick={() => playerRef?.toggleAudio()}
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
    grid-template-columns: minmax(0, 1fr);
    overflow: hidden;
    height: 158px;
    border: 0;
    border-radius: 0;
    color: var(--lcd-text);
    background: transparent;
    box-shadow: none;
    font-family: 'DotGothic16', ui-monospace, monospace;
    text-shadow: var(--lcd-text-shadow);
  }
  .lcd-shell.idle {
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
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    opacity: 0;
    pointer-events: none;
    background: transparent;
  }
  .video-surface {
    width: 100%;
    height: 100%;
  }
  .lcd-shell.video-wide {
    height: auto;
    grid-template-columns: minmax(0, 1fr);
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
  .video-wide .lcd-content {
    display: flex;
    height: 160px;
    padding-bottom: 28px;
    background: var(--player-led-surface);
  }
  .lcd-content {
    position: relative;
    z-index: 2;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    height: 100%;
    padding: 13px 15px 22px;
    overflow: hidden;
    background: var(--player-led-surface);
  }
  .lcd-content > :not(.scanlines) { position: relative; z-index: 1; }
  .lcd-status { display: flex; align-items: baseline; justify-content: space-between; color: var(--lcd-text-dim); font-size: 11px; letter-spacing: 0.06em; text-transform: uppercase; }
  .lcd-status.lcd-card-heading { margin-bottom: 4px; padding-bottom: 3px; }
  .lcd-status > span:first-child { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .lcd-status strong { color: var(--lcd-text); font-weight: 400; }
  .dj-status { display: flex; align-items: center; gap: 0; }
  .dj-status :global(.zap-mini.icon-only.with-name) { max-width: 220px; height: 24px; padding: 0; gap: 0.35rem; color: var(--lcd-text); }
  .dj-status :global(.bolt-icon) { width: 24px; height: 24px; }
  .dj-status :global(.icon-dj-name) { max-width: 180px; }
  .live-label { margin-left: 1.25em; color: var(--accent); }
  .lcd-status > span:last-child { flex: 0 0 auto; margin-left: 12px; color: var(--lcd-text); font-variant-numeric: tabular-nums; }
  .lcd-track-wrap { flex: 0 0 auto; width: 100%; overflow: hidden; white-space: nowrap; }
  .lcd-track { display: inline-flex; min-width: 100%; margin-top: 0; font-size: clamp(20px, 2.4vw, 27px); line-height: 1; letter-spacing: 0.01em; }
  .lcd-track span { padding-right: 48px; }
  .lcd-track.scroll { animation: lcd-marquee 10s linear infinite; }
  .lcd-byline { display: flex; gap: 7px; min-height: 20px; margin-top: 2px; color: var(--lcd-text-soft); font-size: 17px; }
  .lcd-separator { color: var(--lcd-text-dim); }
  .lcd-controls { display: flex; align-items: center; justify-content: flex-end; gap: 18px; margin-top: 10px; }
  .volume-cluster, .right-controls { display: flex; align-items: center; gap: 10px; }
  .right-controls { gap: 14px; }
  .lcd-audio {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    padding: 0;
    border: 0;
    color: var(--lcd-text);
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
      repeating-linear-gradient(90deg, rgba(241, 243, 244, 0.96) 0 7px, transparent 7px 10px) left center / var(--volume) 12px no-repeat,
      repeating-linear-gradient(90deg, rgba(241, 243, 244, 0.16) 0 7px, transparent 7px 10px) left center / 100% 12px no-repeat;
    cursor: pointer;
  }
  .lcd-volume:focus-visible { outline: 1px solid rgba(241, 243, 244, 0.78); outline-offset: 3px; }
  .lcd-volume::-webkit-slider-thumb { appearance: none; width: 3px; height: 18px; border: 1px solid rgba(241, 243, 244, 0.82); background: rgba(27, 31, 35, 0.78); }
  .lcd-volume::-moz-range-thumb { width: 3px; height: 18px; border: 1px solid rgba(241, 243, 244, 0.82); border-radius: 0; background: rgba(27, 31, 35, 0.78); }
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

  @media (max-width: 560px) {
    .lcd-shell { height: 126px; }
    .lcd-content { padding: 13px 9px 16px; }
    .video-wide .lcd-content { height: 130px; padding-bottom: 22px; }
    .lcd-status { font-size: 9px; }
    .live-label { display: none; }
    .lcd-track { margin-top: 0; font-size: 17px; }
    .lcd-byline { font-size: 16px; overflow: hidden; white-space: nowrap; }
    .lcd-controls { gap: 6px; }
    .volume-cluster, .right-controls { gap: 5px; }
    .right-controls { gap: 1px; }
    .lcd-audio { width: 24px; height: 25px; }
    .lcd-audio svg { width: 20px; height: 20px; }
    .lcd-icon { width: 18px; height: 23px; padding-inline: 1px; }
    .lcd-icon svg { width: 16px; height: 16px; }
    .dj-status :global(.zap-mini.icon-only.with-name) { max-width: 130px; height: 18px; gap: 0.2rem; }
    .dj-status :global(.bolt-icon) { width: 16px; height: 16px; }
    .dj-status :global(.icon-dj-name) { max-width: 108px; }
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
  }
</style>

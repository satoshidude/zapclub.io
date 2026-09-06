<script lang="ts">
  import { onDestroy, untrack } from 'svelte'
  import { createPlayer, type YouTubePlayer } from '../../player/youtube'
  import { sync, targetPosition } from '../../nostr/sync.svelte'
  import { stage } from '../../nostr/stage.svelte'
  import { LOBBY_VIDEO_ID } from '../../nostr/pool'

  interface Props {
    onended?: () => void
    onerror?: (videoId: string) => void
    /** May the user hear (= club member)? Otherwise muted + join/login hint. */
    canHear?: boolean
    /** Overlay text for non-members (sign in / join to listen). */
    ctaText?: string
    /** Click on the overlay (trigger login or join). */
    onCta?: () => void
    /** Compact (small thumbnail) mode → hide the full control bar; tap-to-mute still works. */
    compact?: boolean
    /** Keep the YouTube engine active but expose it through a custom external control surface. */
    headless?: boolean
    /** Fit the video flush into a parent-owned media frame. */
    embedded?: boolean
    /** Optional artwork shown instead of the live video. */
    cover?: string
    /** Render cover artwork as a subdued LED display rather than a plain image. */
    ledCover?: boolean
    /** Artwork shown while the club is in the lobby. */
    poster?: string
    /** Optional action for the transparent shield above an embedded video. */
    onvideotoggle?: () => void
    videoExpanded?: boolean
    oncontrolstate?: (state: { ready: boolean; muted: boolean; volume: number; fullscreen: boolean; playing: boolean }) => void
    /** Live embed metadata (channel + title) once a real track plays — no extraction, no bot
     *  gate. Lets the card show the artist (from a "Artist - Topic" channel) for bare titles. */
    onmeta?: (author: string, title: string) => void
    onduration?: (seconds: number) => void
  }
  let {
    onended,
    onerror,
    canHear = false,
    ctaText = '',
    onCta,
    compact = false,
    headless = false,
    embedded = false,
    cover = '',
    ledCover = false,
    poster = '',
    onvideotoggle,
    videoExpanded = false,
    oncontrolstate,
    onmeta,
    onduration,
  }: Props = $props()

  const elementId = 'yt-player'
  let player: YouTubePlayer | null = null
  let destroyed = false
  let ready = $state(false)

  // The iframe starts muted so autoplay can begin. Once a freshly loaded video is
  // actually playing, Zapclub always asks YouTube to unmute it — Safari included.
  let muted = $state(true)
  let lastCanHear = untrack(() => canHear)
  let volume = $state(70)
  let isFullscreen = $state(false)
  let playing = $state(false)
  let playerEl: HTMLDivElement
  let loadedVideoId: string | null = null
  let unmuteAfterNextLoad = false
  let idleMode = false
  // True when a DJ is on stage but has no tracks yet — plays the lobby video as background audio.
  const lobbyPlaying = $derived(!sync.live && stage.djs.length > 0 && !!LOBBY_VIDEO_ID)
  let driftTimer: ReturnType<typeof setInterval> | null = null

  createPlayer(elementId, {
    controls: false,
    muted: true,
    onStateChange(s) {
      if (s === 1) playing = true
      else if (s === -1 || s === 0 || s === 2 || s === 5) playing = false
      if (s === 1) {
        if (unmuteAfterNextLoad) {
          unmuteAfterNextLoad = false
          if (canHear) unmuteLoadedVideo()
        } else if (canHear && !muted && player) {
          // Re-assert the chosen audible state after buffering without overriding
          // a deliberate mute made after the video loaded.
          player.setVolume(volume)
          player.unMute()
        }
        // Surface the embed's channel + title (no extraction → no bot gate) for a real track.
        if (!idleMode && player) {
          const d = player.getVideoData()
          if (d && (d.author || d.title)) onmeta?.(d.author ?? '', d.title ?? '')
          const dur = Math.round(player.getDuration())
          if (dur > 0) onduration?.(dur)
        }
      }
      if (s !== 0) return // 0 = ended
      if (idleMode) {
        // Only loop when lobby video should play (DJ on stage, no tracks).
        if (lobbyPlaying) loadVideo(LOBBY_VIDEO_ID, 0)
        return
      }
      onended?.()
    },
    onError() {
      // Unplayable video (deleted, region-locked, embedding off).
      if (idleMode) return // no lobby track to reload
      const id = sync.live?.videoId
      if (id) onerror?.(id) // conductor advances
    },
  }).then((p) => {
    if (destroyed) {
      // Component unmounted before the player finished initializing → don't leak it.
      p.destroy()
      return
    }
    player = p
    ready = true
    apply(true)
  })

  /** Applies the current now_playing state to the player. */
  function loadVideo(videoId: string, startSeconds: number) {
    if (!player) return
    unmuteAfterNextLoad = true
    player.load(videoId, startSeconds)
  }

  function unmuteLoadedVideo() {
    if (!player) return
    if (volume === 0) volume = 70
    player.setVolume(volume)
    player.unMute()
    muted = false
  }

  function apply(force: boolean) {
    if (!player || !ready) return
    const np = sync.live

    // Nothing playing → idle mode.
    // lobbyPlaying=true  → DJ on stage, no tracks → load lobby video (audio behind overlay)
    // lobbyPlaying=false → no DJ on stage          → pause, full opaque overlay takes over
    if (!np) {
      idleMode = true
      loadedVideoId = null
      if (lobbyPlaying) {
        loadVideo(LOBBY_VIDEO_ID, 0)
      } else {
        if (player.getState() === 1) player.pause()
      }
      return
    }
    idleMode = false

    // Set-and-forget: on a NEW track (or force) load once at the right position — then let
    // it play through, NO re-adjusting/seeking. Each track change re-syncs by itself; in
    // between it runs smoothly.
    if (np.videoId !== loadedVideoId || force) {
      loadedVideoId = np.videoId
      loadVideo(np.videoId, targetPosition())
      return
    }
    if (np.status === 'paused') {
      if (player.getState() === 1) player.pause()
      return
    }
    // Just keeps playing (no seek). Only ensure it doesn't stall.
    player.setPlaybackRate(1)
    const st = player.getState()
    if (st !== 1 && st !== 3) player.play()
  }

  // trackKey encodes every state that should trigger a player command:
  // • real track: videoId+startedAt+status (heartbeat doesn't change startedAt → no restarts)
  // • lobby mode: 'lobby' or 'idle' — changes when lobbyPlaying or sync.live changes, so
  //   DJ joining/leaving stage (which changes stage.djs → lobbyPlaying) always calls apply().
  let trackKey = $derived(
    sync.live
      ? sync.live.videoId + sync.live.startedAt + sync.live.status
      : lobbyPlaying
        ? 'lobby'
        : 'idle',
  )
  $effect(() => {
    void trackKey
    apply(false)
  })

  function toggleMute() {
    if (!player) return
    if (muted) {
      player.unMute()
      muted = false
      if (volume === 0) {
        volume = 70
        player.setVolume(70)
      }
    } else {
      player.mute()
      muted = true
    }
  }

  /** Manual fallback: unmute inside a user gesture and re-sync to the live position. */
  function enableSound() {
    if (!player) return
    unmuteLoadedVideo()
    if (sync.live) {
      player.seekTo(targetPosition())
      player.play()
    } else if (lobbyPlaying) {
      // Lobby video plays behind the overlay — ensure it's running after unmute.
      if (player.getState() !== 1) player.play()
    }
  }

  $effect(() => {
    oncontrolstate?.({ ready, muted, volume, fullscreen: isFullscreen, playing })
  })

  /** Volume slider: sets volume, unmutes (0 = muted). */
  function applyVolume(v: number) {
    volume = v
    if (!player) return
    player.setVolume(v)
    if (v === 0) {
      if (!muted) {
        player.mute()
        muted = true
      }
    } else if (muted) {
      player.unMute()
      muted = false
    }
  }

  // Revoking access mutes immediately. Gaining access makes the already loaded
  // stream audible as well; join/sign-in supplies the user interaction Safari expects.
  $effect(() => {
    void ready
    const allowed = canHear
    if (!player || !ready || allowed === lastCanHear) return
    lastCanHear = allowed
    if (!allowed) {
      player.mute()
      muted = true
    } else {
      unmuteLoadedVideo()
    }
  })

  function toggleFullscreen() {
    if (document.fullscreenElement) void document.exitFullscreen()
    else void playerEl?.requestFullscreen?.()
  }

  /** Control hooks used by the LCD surface in NowPlaying. */
  export function toggleAudio() {
    if (muted) enableSound()
    else toggleMute()
  }

  export function setAudioVolume(value: number) {
    applyVolume(Math.max(0, Math.min(100, value)))
  }

  export function toggleVideoFullscreen() {
    toggleFullscreen()
  }
  function onFsChange() {
    isFullscreen = !!document.fullscreenElement
  }
  if (typeof document !== 'undefined') document.addEventListener('fullscreenchange', onFsChange)

  // Drift correction: every 5 s, seek if we're >3 s off the relay-calibrated position.
  // Only fires while actually playing (state=1) — never fights a buffer or a fresh load.
  // Threshold 3 s keeps drift <5 s (relay requirement) without seeking during normal playback.
  driftTimer = setInterval(() => {
    if (!player || !sync.live || sync.live.status === 'paused') return
    if (player.getState() !== 1) {
      // Stalled/paused — just ensure it's running (original stall-recovery behavior).
      apply(false)
      return
    }
    const drift = player.getCurrentTime() - targetPosition()
    if (Math.abs(drift) > 3) {
      player.seekTo(targetPosition() + 0.4)
    }
  }, 5000)

  onDestroy(() => {
    destroyed = true
    if (driftTimer) clearInterval(driftTimer)
    if (typeof document !== 'undefined') document.removeEventListener('fullscreenchange', onFsChange)
    player?.destroy()
  })
</script>

<div class="player-wrap" class:headless class:embedded bind:this={playerEl}>
  <div class="player">
    <div class="frame">
      <div id={elementId}></div>
    </div>

    {#if sync.live && cover}
      <div class="cover" class:led-cover={ledCover} aria-hidden="true">
        <img src={cover} alt="" />
      </div>
    {/if}

    <!-- Shield catches pointer events so YouTube never adds hover chrome. The embedded LCD can
         use it to resize the video; standalone players retain video-tap mute. -->
    {#if embedded}
      {#if onvideotoggle}
        <button
          type="button"
          class="shield video-toggle"
          onclick={onvideotoggle}
          aria-label={videoExpanded ? 'Collapse video' : 'Expand video'}
          aria-expanded={videoExpanded}
          title={videoExpanded ? 'Collapse video' : 'Expand video'}
        ></button>
      {:else}
        <div class="shield" aria-hidden="true"></div>
      {/if}
    {:else}
      <button
        class="shield"
        class:clickable={canHear}
        onclick={() => {
          if (!canHear) return
          // Muted → tapping turns sound on (inside the gesture, re-syncs); unmuted → mute.
          if (muted) enableSound()
          else toggleMute()
        }}
        aria-label={canHear ? (muted ? 'Unmute' : 'Mute') : ''}
        tabindex={canHear ? 0 : -1}
      ></button>
    {/if}

    <!-- Lobby overlay: always full opaque when no live track.
         When a DJ is on stage without tracks the lobby video plays behind it (audio only). -->
    {#if !sync.live}
      <div class="lobby" class:has-poster={!!poster} aria-hidden="true">
        {#if poster}<img class="poster" src={poster} alt="" />{/if}
        {#if !poster}
          <span class="lobby-icon">🎧</span>
          <span class="lobby-text">{stage.djs.length > 0 ? 'DJ is loading tracks…' : 'Lobby — no DJ on stage'}</span>
        {/if}
      </div>
    {/if}

    <!-- Non-members don't hear: overlay with a login/join prompt. -->
    {#if !canHear && ctaText}
      <button class="cta-listen" onclick={() => onCta?.()}>🔒 {ctaText}</button>
    {/if}
  </div>

  {#if ready && canHear && !compact}
    <!-- Control bar BELOW the video (no overlay) — members only; hidden in compact mode. -->
    <div class="controls">
      <button class="ctrl" onclick={toggleMute} title={muted ? 'Unmute' : 'Mute'}>
        {muted ? '🔇' : '🔊'}
      </button>
      <input
        class="vol"
        type="range"
        min="0"
        max="100"
        value={volume}
        oninput={(e) => applyVolume(+(e.currentTarget as HTMLInputElement).value)}
        aria-label="Volume"
      />
      <span class="ctrl-spacer"></span>
      <button class="ctrl" onclick={toggleFullscreen} title="Fullscreen">
        {isFullscreen ? '🡻' : '⛶'}
      </button>
    </div>
  {/if}
</div>

<style>
  .player-wrap {
    width: 100%;
  }
  .player-wrap.headless {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    opacity: 0;
    pointer-events: none;
    clip-path: inset(50%);
  }
  .player-wrap.embedded,
  .player-wrap.embedded .player {
    height: 100%;
  }
  .player-wrap.embedded .player {
    aspect-ratio: auto;
    border: 0;
    border-radius: 0;
  }
  .player {
    position: relative;
    width: 100%;
    aspect-ratio: 16 / 9;
    background: #000;
    border-radius: var(--radius);
    overflow: hidden;
    border: 1px solid var(--border);
  }
  .frame {
    position: absolute;
    inset: 0;
    overflow: hidden;
  }
  .frame :global(iframe) {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    border: 0;
  }
  .cover {
    position: absolute;
    inset: 0;
  }
  .cover.led-cover {
    isolation: isolate;
    overflow: hidden;
    background: #02050a;
  }
  .cover.led-cover img {
    transform: scale(1.015);
    filter: saturate(0.82) contrast(1.18) brightness(0.76);
  }
  .cover.led-cover::before,
  .cover.led-cover::after {
    content: '';
    position: absolute;
    inset: 0;
    z-index: 1;
    pointer-events: none;
  }
  .cover.led-cover::before {
    background:
      radial-gradient(circle at 46% 42%, rgba(116, 180, 255, 0.12), transparent 58%),
      linear-gradient(180deg, rgba(2, 5, 10, 0.04), rgba(2, 5, 10, 0.26));
  }
  .cover.led-cover::after {
    background:
      repeating-linear-gradient(0deg, rgba(207, 233, 255, 0.045) 0 1px, transparent 1px 4px),
      radial-gradient(circle, rgba(224, 238, 250, 0.1) 0 0.55px, transparent 0.8px) 0 0 / 4px 4px;
    opacity: 0.52;
  }
  .cover img,
  .poster {
    width: 100%;
    height: 100%;
    display: block;
    object-fit: cover;
  }
  .shield {
    position: absolute;
    inset: 0;
    z-index: 1;
    background: transparent;
    border: none;
    padding: 0;
    cursor: default;
  }
  .shield.clickable {
    cursor: pointer;
  }
  .shield.video-toggle {
    cursor: pointer;
  }
  .shield.video-toggle:focus-visible {
    outline: 1px solid var(--lcd-text);
    outline-offset: -3px;
  }
  /* Lobby overlay covers the idle stream with a calm placeholder. */
  .lobby {
    position: absolute;
    inset: 0;
    z-index: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.6rem;
    /* Opaque so a non-embeddable lobby video never shows through. */
    background:
      radial-gradient(600px 300px at 50% 40%, rgba(177, 77, 255, 0.22), transparent 70%),
      #07070a;
    pointer-events: none;
  }
  .lobby.has-poster {
    display: block;
    background: #07070a;
  }
  .lobby-icon {
    font-size: 2.4rem;
    animation: lobby-pulse 2s ease-in-out infinite;
  }
  @keyframes lobby-pulse {
    0%,
    100% {
      transform: scale(1);
      opacity: 0.85;
    }
    50% {
      transform: scale(1.1);
      opacity: 1;
    }
  }
  .lobby-text {
    font-size: 0.9rem;
    color: var(--text-dim);
    letter-spacing: 0.02em;
  }
  .cta-listen {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    z-index: 3;
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    background: rgba(11, 10, 16, 0.82);
    backdrop-filter: blur(6px);
    border: 1px solid var(--accent-2);
    border-radius: 999px;
    color: var(--text);
    padding: 0.7rem 1.3rem;
    font-size: 1rem;
    font-weight: 700;
    cursor: pointer;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
  }
  .cta-listen:hover {
    border-color: var(--accent);
    color: var(--accent);
  }
  .controls {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    margin-top: 0.5rem;
    padding: 0.4rem 0.7rem;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .ctrl {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: none;
    color: var(--text);
    font-size: 1.05rem;
    cursor: pointer;
    padding: 0.1rem;
    line-height: 1;
  }
  .ctrl:hover {
    color: var(--accent);
  }
  .ctrl-spacer {
    flex: 1;
  }
  .vol {
    width: 90px;
    height: 4px;
    -webkit-appearance: none;
    appearance: none;
    background: var(--border);
    border-radius: 999px;
    cursor: pointer;
    flex: none;
  }
  .vol::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 13px;
    height: 13px;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
  }
  .vol::-moz-range-thumb {
    width: 13px;
    height: 13px;
    border: none;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
  }
  @media (max-width: 560px) {
    .vol {
      width: 64px;
    }
  }
  .player-wrap:fullscreen {
    display: flex;
    flex-direction: column;
    background: #000;
  }
  .player-wrap.headless:fullscreen {
    width: 100%;
    height: 100%;
    overflow: visible;
    opacity: 1;
    pointer-events: auto;
    clip-path: none;
  }
  .player-wrap:fullscreen .player {
    flex: 1;
    aspect-ratio: auto;
    border: none;
    border-radius: 0;
    min-height: 0;
  }
  .player-wrap:fullscreen .controls {
    margin: 0;
    border: none;
    border-radius: 0;
    justify-content: center;
    gap: 1rem;
  }
</style>

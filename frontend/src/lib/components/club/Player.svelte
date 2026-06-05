<script lang="ts">
  import { onDestroy } from 'svelte'
  import { createPlayer, type YouTubePlayer } from '../../player/youtube'
  import { sync, targetPosition } from '../../nostr/sync.svelte'

  interface Props {
    onended?: () => void
    onerror?: (videoId: string) => void
    /** May the user hear (= club member)? Otherwise muted + join/login hint. */
    canHear?: boolean
    /** Overlay text for non-members (sign in / join to listen). */
    ctaText?: string
    /** Click on the overlay (trigger login or join). */
    onCta?: () => void
  }
  let { onended, onerror, canHear = false, ctaText = '', onCta }: Props = $props()

  let lobbyFailed = false

  /** Lobby track: loops (muted) while nothing is playing in the club. The lobby overlay
   *  covers it visually; if it's not embeddable, onError just stops it (clean lobby).
   *  playlister's placeholder tune (Christian Bruhn). */
  const IDLE_VIDEO = 'w8NRrAOS6s0'

  const elementId = 'yt-player'
  let player: YouTubePlayer | null = null
  let destroyed = false
  let ready = $state(false)
  let muted = $state(true)
  let volume = $state(70)
  let isFullscreen = $state(false)
  let playerEl: HTMLDivElement
  let loadedVideoId: string | null = null
  let idleMode = false
  let driftTimer: ReturnType<typeof setInterval> | null = null

  // Muted autostart (browsers allow it without a gesture) — no tap overlay needed.
  createPlayer(elementId, {
    controls: false,
    muted: true,
    onStateChange(s) {
      if (s === 1) lobbyFailed = false // playing → reset lobby error flag
      if (s !== 0) return // 0 = ended
      if (idleMode) {
        // Loop the lobby track.
        loadedVideoId = null
        apply(true)
      } else {
        onended?.()
      }
    },
    onError() {
      // Unplayable video (deleted, region-locked, embedding off).
      if (idleMode) {
        lobbyFailed = true // lobby track dead → don't endlessly reload
        return
      }
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
  function apply(force: boolean) {
    if (!player || !ready) return
    const np = sync.live

    // Nothing playing → loop the lobby track.
    if (!np) {
      if ((loadedVideoId !== IDLE_VIDEO || force) && !lobbyFailed) {
        idleMode = true
        loadedVideoId = IDLE_VIDEO
        player.load(IDLE_VIDEO, 0)
      } else if (player.getState() !== 1 && !lobbyFailed) {
        player.play()
      }
      return
    }
    idleMode = false

    // Set-and-forget: on a NEW track (or force) load once at the right position — then let
    // it play through, NO re-adjusting/seeking. Each track change re-syncs by itself; in
    // between it runs smoothly.
    if (np.videoId !== loadedVideoId || force) {
      loadedVideoId = np.videoId
      player.load(np.videoId, targetPosition())
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

  let trackKey = $derived(sync.live ? sync.live.videoId + sync.live.startedAt + sync.live.status : '')
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

  // Non-members don't hear: mute the player as soon as canHear is false (and it stays —
  // they don't get the control bar to undo it).
  $effect(() => {
    if (!canHear && player && !muted) {
      player.mute()
      muted = true
    }
  })

  function toggleFullscreen() {
    if (document.fullscreenElement) void document.exitFullscreen()
    else void playerEl?.requestFullscreen?.()
  }
  function onFsChange() {
    isFullscreen = !!document.fullscreenElement
  }
  if (typeof document !== 'undefined') document.addEventListener('fullscreenchange', onFsChange)

  // Only ensure playback doesn't silently stall (no seek).
  driftTimer = setInterval(() => apply(false), 5000)

  onDestroy(() => {
    destroyed = true
    if (driftTimer) clearInterval(driftTimer)
    if (typeof document !== 'undefined') document.removeEventListener('fullscreenchange', onFsChange)
    player?.destroy()
  })
</script>

<div class="player-wrap" bind:this={playerEl}>
  <div class="player">
    <div class="frame">
      <div id={elementId}></div>
    </div>

    <!-- Click shield: catches mouse events → no YouTube hover overlays / clicks.
         For members a click on the video toggles mute. -->
    <button
      class="shield"
      class:clickable={canHear}
      onclick={() => { if (canHear) toggleMute() }}
      aria-label={canHear ? (muted ? 'Unmute' : 'Mute') : ''}
      tabindex={canHear ? 0 : -1}
    ></button>

    <!-- Lobby: when no DJ is playing, overlay a hint over the muted idle stream. -->
    {#if !sync.live}
      <div class="lobby" aria-hidden="true">
        <span class="lobby-icon">🎧</span>
        <span class="lobby-text">Lobby — no DJ on stage</span>
      </div>
    {/if}

    <!-- Non-members don't hear: overlay with a login/join prompt. -->
    {#if !canHear && ctaText}
      <button class="cta-listen" onclick={() => onCta?.()}>🔒 {ctaText}</button>
    {/if}
  </div>

  {#if ready && canHear}
    <!-- Control bar BELOW the video (no overlay) — members only. -->
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
  /* Lobby overlay covers the (muted) idle stream with a calm placeholder. */
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

<script lang="ts">
  import { onDestroy } from 'svelte'
  import { createPlayer, type YouTubePlayer } from '../../player/youtube'
  import { sync, targetPosition } from '../../nostr/sync.svelte'
  import { liveSession } from '../../nostr/livesession.svelte'
  import { connectLivekit, attachTrack, type LivekitClient } from '../../player/livekit'
  import { Track } from 'livekit-client'

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
    /** Live embed metadata (channel + title) once a real track plays — no extraction, no bot
     *  gate. Lets the card show the artist (from a "Artist - Topic" channel) for bare titles. */
    onmeta?: (author: string, title: string) => void
    onduration?: (seconds: number) => void
    /** Club group-id — needed for the LiveKit token endpoint. */
    groupId?: string
  }
  let { onended, onerror, canHear = false, ctaText = '', onCta, compact = false, onmeta, onduration, groupId }: Props = $props()

  let lobbyFailed = false

  /** Lobby track: loops (muted) while nothing is playing in the club. The lobby overlay
   *  covers it visually; if it's not embeddable, onError just stops it (clean lobby).
   *  playlister's placeholder tune (Christian Bruhn). */
  const IDLE_VIDEO = 'w8NRrAOS6s0'

  const elementId = 'yt-player'
  let player: YouTubePlayer | null = null
  let destroyed = false
  let ready = $state(false)

  // Start MUTED everywhere. Muted autoplay is allowed by every browser without a user gesture
  // (unlike unmuted, which iOS Safari / Chrome's autoplay policy block), so the synced stream
  // always starts visually. A persistent "🔊 Tap for sound" overlay shows whenever we're muted;
  // one tap unmutes inside the gesture (the only way iOS lets audio start) and re-syncs.
  let muted = $state(true)
  let volume = $state(70)
  let isFullscreen = $state(false)
  let playerEl: HTMLDivElement
  let loadedVideoId: string | null = null
  let idleMode = false
  let driftTimer: ReturnType<typeof setInterval> | null = null

  // ── LiveKit live A/V integration ────────────────────────────────────────────

  let lkClient = $state<LivekitClient | null>(null)
  let lkConnectedGroup = $state<string | null>(null) // which group we're connected to
  let liveVideoEl = $state<HTMLVideoElement | null>(null)
  let liveAudioEl = $state<HTMLAudioElement | null>(null)
  let lkError = $state('')
  let isTakeover = $derived(liveSession.current?.mode === 'takeover')
  let isTalkover = $derived(liveSession.current?.mode === 'talkover')
  const TALKOVER_DUCK_VOLUME = 15

  // React to live session changes.
  $effect(() => {
    const session = liveSession.current
    const gid = groupId
    if (!gid) return

    if (session && (session.mode === 'takeover' || session.mode === 'talkover')) {
      // New session or session continued — connect if not already connected to this group.
      if (lkConnectedGroup !== gid) {
        void (async () => {
          try {
            lkError = ''
            const client = await connectLivekit(gid)
            if (destroyed) { void client.disconnect(); return }
            lkClient = client
            lkConnectedGroup = gid
            client.onRemoteTrack(({ track }) => {
              if (track.kind === Track.Kind.Video && liveVideoEl) {
                attachTrack(track, liveVideoEl)
                void liveVideoEl.play().catch(() => {})
              } else if (track.kind === Track.Kind.Audio && liveAudioEl) {
                attachTrack(track, liveAudioEl)
                void liveAudioEl.play().catch(() => {})
                // LiveKit audio is live — mute YT to avoid double audio.
                if (player) player.mute()
              }
            })
          } catch (e) {
            lkError = String((e as Error)?.message ?? e)
          }
        })()
      }
      // Duck YT for talkover.
      if (session.mode === 'talkover' && player) {
        player.setVolume(TALKOVER_DUCK_VOLUME)
      }
    } else {
      // Session ended — disconnect and restore volume.
      if (lkClient) {
        const c = lkClient
        lkClient = null
        lkConnectedGroup = null
        void c.disconnect()
      }
      // Restore YT audio (unmute + volume if user hadn't manually muted).
      if (player && !muted) {
        player.unMute()
        player.setVolume(volume)
      }
    }
  })

  createPlayer(elementId, {
    controls: false,
    muted: true, // muted autoplay (always allowed); the "Tap for sound" overlay unmutes.
    onStateChange(s) {
      if (s === 1) {
        lobbyFailed = false // playing → reset lobby error flag
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

  /** Sound-tap: unmute INSIDE the user gesture (the only way iOS lets audio start) and re-sync
   *  to the live position (the muted autoplay kept the clock running). */
  function enableSound() {
    if (!player) return
    if (volume === 0) volume = 70
    player.unMute()
    player.setVolume(volume)
    muted = false
    if (sync.live) {
      player.seekTo(targetPosition())
      player.play()
    }
  }

  // Show the "Tap for sound" prompt whenever we're muted (and the user may hear) — i.e. the
  // muted autostart, or after the user mutes again.
  const needsSoundTap = $derived(canHear && ready && muted)

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
    if (lkClient) { void lkClient.disconnect(); lkClient = null }
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
      onclick={() => {
        if (!canHear) return
        // Muted → tapping turns sound on (inside the gesture, re-syncs); unmuted → mute.
        if (muted) enableSound()
        else toggleMute()
      }}
      aria-label={canHear ? (needsSoundTap ? 'Tap for sound' : muted ? 'Unmute' : 'Mute') : ''}
      tabindex={canHear ? 0 : -1}
    ></button>

    <!-- iOS: muted autoplay is running in sync; one tap (anywhere on the video) turns on sound.
         pointer-events:none so the full-area shield button underneath catches the tap. -->
    {#if needsSoundTap}
      <div class="sound-tap" aria-hidden="true">
        <span class="sound-pill">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M4 9v6h4l5 4V5L8 9H4z" fill="currentColor" stroke="none" />
            <path d="M16 8.8a4.5 4.5 0 0 1 0 6.4" />
            <path d="M18.7 6a8 8 0 0 1 0 12" />
          </svg>
        </span>
      </div>
    {/if}

    <!-- Lobby: when no DJ is playing (and no live takeover), overlay a hint. -->
    {#if !sync.live && !isTakeover}
      <div class="lobby" aria-hidden="true">
        <span class="lobby-icon">🎧</span>
        <span class="lobby-text">Lobby — no DJ on stage</span>
      </div>
    {/if}

    <!-- Takeover: the DJ's live A/V feed replaces the YT frame. -->
    {#if isTakeover}
      <div class="live-frame">
        <!-- svelte-ignore a11y_media_has_caption -->
        <video bind:this={liveVideoEl} class="live-video" autoplay playsinline></video>
        <!-- svelte-ignore a11y_media_has_caption -->
        <audio bind:this={liveAudioEl} autoplay></audio>
        <div class="live-badge" aria-hidden="true">● LIVE</div>
      </div>
    {:else}
      <!-- Talkover: keep the YT frame, add an invisible audio overlay + a small badge. -->
      <!-- svelte-ignore a11y_media_has_caption -->
      <audio bind:this={liveAudioEl} autoplay style="display:none"></audio>
      {#if isTalkover}
        <div class="live-badge live-badge-talkover" aria-hidden="true">● LIVE</div>
      {/if}
    {/if}

    {#if lkError && (isTakeover || isTalkover)}
      <div class="lk-error" aria-hidden="true">{lkError}</div>
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
  /* iOS "tap for sound" prompt — visible hint over the (muted) synced stream; the tap is
     handled by the full-area shield button beneath it. */
  /* Subtle, finer "tap for sound": just a small speaker icon (no heavy overlay / text). */
  .sound-tap {
    position: absolute;
    inset: 0;
    z-index: 2;
    display: grid;
    place-items: center;
    pointer-events: none;
  }
  .sound-pill {
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    border-radius: 999px;
    line-height: 1;
    color: #0b0a10;
    background: #fff;
    box-shadow: 0 2px 10px rgba(0, 0, 0, 0.45), 0 0 0 0 rgba(255, 255, 255, 0.5);
    animation: sound-pulse 2.2s ease-in-out infinite;
  }
  @keyframes sound-pulse {
    0%,
    100% {
      box-shadow: 0 2px 10px rgba(0, 0, 0, 0.45), 0 0 0 0 rgba(255, 255, 255, 0.55);
    }
    50% {
      box-shadow: 0 2px 10px rgba(0, 0, 0, 0.45), 0 0 0 8px rgba(255, 255, 255, 0);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .sound-pill {
      animation: none;
    }
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
  /* Live A/V (LiveKit) overlay */
  .live-frame {
    position: absolute;
    inset: 0;
    z-index: 4;
    background: #000;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .live-video {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .live-badge {
    position: absolute;
    top: 0.5rem;
    left: 0.5rem;
    z-index: 5;
    background: rgba(220, 38, 38, 0.92);
    color: #fff;
    font-size: 0.7rem;
    font-weight: 700;
    letter-spacing: 0.06em;
    padding: 0.15rem 0.45rem;
    border-radius: 999px;
    pointer-events: none;
  }
  .live-badge-talkover {
    background: rgba(245, 158, 11, 0.92);
  }
  .lk-error {
    position: absolute;
    bottom: 0.6rem;
    left: 50%;
    transform: translateX(-50%);
    z-index: 6;
    background: rgba(0,0,0,0.75);
    color: var(--danger, #ef4444);
    font-size: 0.75rem;
    padding: 0.25rem 0.6rem;
    border-radius: var(--radius-sm, 4px);
    pointer-events: none;
    white-space: nowrap;
    max-width: 90%;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>

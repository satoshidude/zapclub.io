<script lang="ts">
  import { createPlayer, type YouTubePlayer } from '../player/youtube'
  import { miniplay, miniPosition, stopMiniPlay } from '../nostr/miniplay.svelte'
  import { router, goClub } from '../router.svelte'

  // Global persistent mini-player: keeps the active club's audio alive while the user is
  // on any NON-club page. On a club page the club's own hero player handles audio, so the
  // mini hides — that also avoids a flash when navigating club → club.
  const show = $derived(miniplay.active && router.route.name !== 'club')

  let player: YouTubePlayer | null = null
  let creating = false
  let ready = false
  let curVid = ''
  // Start UNMUTED — the mini-player continues the club's audio across navigation; muting it
  // would silence the music the user was listening to. Browser may require a tap first.
  let muted = $state(false)
  let driftTimer: ReturnType<typeof setInterval> | null = null

  function applyTrack(reseek: boolean) {
    if (!player || !ready) return
    const np = miniplay.np
    if (!np?.videoId) return
    if (np.videoId !== curVid) {
      curVid = np.videoId
      player.load(np.videoId, miniPosition())
      // Honor the current mute state across track loads (loadVideoById can reset it).
      if (muted) player.mute()
      else player.unMute()
      return
    }
    if (reseek) {
      const drift = Math.abs(player.getCurrentTime() - miniPosition())
      if (drift > 2.5) player.seekTo(miniPosition())
    }
  }

  // Create the player when the bar becomes visible; tear it down when it hides.
  $effect(() => {
    if (show && !player && !creating) {
      creating = true
      void createPlayer('yt-mini', {
        controls: false,
        muted: false, // continue the club's audio with sound
        onReady: () => {
          ready = true
          applyTrack(false)
        },
        // Keep the player's mute in sync with our state across YouTube's autoplay/load resets.
        onStateChange: () => {
          if (player) muted ? player.mute() : player.unMute()
        },
        onError: () => {},
      }).then((p) => {
        creating = false
        if (!show) {
          // Hidden again before the player finished initializing → don't leak it.
          p.destroy()
          return
        }
        player = p
        driftTimer = setInterval(() => applyTrack(true), 5000)
      })
    } else if (!show && player) {
      if (driftTimer) {
        clearInterval(driftTimer)
        driftTimer = null
      }
      player.destroy()
      player = null
      ready = false
      curVid = ''
      muted = false
    }
  })

  // Follow track changes pushed by the conductor.
  $effect(() => {
    void miniplay.np?.videoId
    if (show && player && ready) applyTrack(false)
  })

  function toggleMute() {
    if (!player) return
    if (muted) {
      player.unMute()
      player.setVolume(100)
      muted = false
    } else {
      player.mute()
      muted = true
    }
  }
</script>

{#if show}
  <div class="mini">
    <div class="vid"><div id="yt-mini"></div></div>
    <button class="info" onclick={() => miniplay.clubId && goClub(miniplay.clubId)} title="Back to club">
      <span class="title">{miniplay.np?.title || 'Playing…'}</span>
      <span class="club">▶ {miniplay.clubName || 'club'}</span>
    </button>
    <button class="ctrl" onclick={toggleMute} title={muted ? 'Unmute' : 'Mute'}>{muted ? '🔇' : '🔊'}</button>
    <button class="ctrl" onclick={stopMiniPlay} title="Stop">✕</button>
  </div>
{/if}

<style>
  .mini {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.4rem 0.6rem;
    background: color-mix(in srgb, var(--bg-elev) 96%, transparent);
    backdrop-filter: blur(10px);
    border-top: 1px solid var(--border);
  }
  .vid {
    flex: 0 0 auto;
    width: 88px;
    height: 50px;
    border-radius: 6px;
    overflow: hidden;
    background: #000;
  }
  .vid :global(#yt-mini),
  .vid :global(iframe) {
    width: 88px;
    height: 50px;
    border: 0;
    display: block;
  }
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.1rem;
    background: none;
    border: none;
    color: var(--text);
    cursor: pointer;
    text-align: left;
    padding: 0;
  }
  .title {
    font-size: 0.84rem;
    font-weight: 600;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .club {
    font-size: 0.72rem;
    color: var(--accent);
    font-weight: 600;
  }
  .info:hover .club {
    text-decoration: underline;
  }
  .ctrl {
    flex: 0 0 auto;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 7px;
    width: 32px;
    height: 32px;
    cursor: pointer;
    font-size: 0.9rem;
  }
  .ctrl:hover {
    color: var(--text);
    border-color: var(--accent-2);
  }
  /* Sit above the fixed mobile bottom nav. */
  @media (max-width: 560px) {
    .mini {
      bottom: 3.4rem;
    }
  }
</style>

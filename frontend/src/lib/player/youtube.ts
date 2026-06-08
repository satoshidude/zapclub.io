// Thin wrapper around the YouTube IFrame Player API. Exactly one instance per app.
// Docs: https://developers.google.com/youtube/iframe_api_reference

let apiReady: Promise<void> | null = null

/** Loads the IFrame API once and resolves as soon as `YT` is available. */
function loadApi(): Promise<void> {
  if (apiReady) return apiReady
  apiReady = new Promise<void>((resolve) => {
    if (window.YT?.Player) {
      resolve()
      return
    }
    const prev = window.onYouTubeIframeAPIReady
    window.onYouTubeIframeAPIReady = () => {
      prev?.()
      resolve()
    }
    const tag = document.createElement('script')
    tag.src = 'https://www.youtube.com/iframe_api'
    document.head.appendChild(tag)
  })
  return apiReady
}

export interface YouTubePlayer {
  load(videoId: string, startSeconds: number): void
  play(): void
  pause(): void
  seekTo(seconds: number): void
  setPlaybackRate(rate: number): void
  getCurrentTime(): number
  getDuration(): number
  mute(): void
  unMute(): void
  /** Set volume (0–100). Auto-unmutes. */
  setVolume(v: number): void
  /** The underlying iframe — for fullscreen requests. */
  getIframe(): HTMLIFrameElement | null
  /** YT state: -1 unstarted, 0 ended, 1 playing, 2 paused, 3 buffering, 5 cued */
  getState(): number
  /** Live video metadata from the embed (no extraction → no bot gate): channel + title. */
  getVideoData(): { author?: string; title?: string; video_id?: string }
  destroy(): void
}

/**
 * Creates a player in the element with the given id. `onReady` fires once commands are
 * possible; `onStateChange` reports e.g. track end (0).
 */
export async function createPlayer(
  elementId: string,
  opts: {
    controls: boolean
    muted?: boolean
    onReady?: () => void
    onStateChange?: (state: number) => void
    /** YouTube error code (2/5/100/101/150): video unplayable/not embeddable. */
    onError?: (code: number) => void
  },
): Promise<YouTubePlayer> {
  await loadApi()

  return new Promise<YouTubePlayer>((resolve) => {
    const yt = new window.YT!.Player(elementId, {
      width: '100%',
      height: '100%',
      playerVars: {
        controls: opts.controls ? 1 : 0,
        disablekb: 1,
        modestbranding: 1,
        rel: 0,
        playsinline: 1,
        // Muted autostart (browsers allow that without a gesture); clean UI.
        autoplay: 1,
        mute: opts.muted ? 1 : 0,
        iv_load_policy: 3, // annotations off
        fs: 0, // no fullscreen button
      },
      events: {
        onReady: () => {
          opts.onReady?.()
          resolve(wrap(yt))
        },
        onStateChange: (e: { data: number }) => opts.onStateChange?.(e.data),
        onError: (e: { data: number }) => opts.onError?.(e.data),
      },
    })
  })
}

function wrap(yt: YTPlayerInstance): YouTubePlayer {
  return {
    load: (videoId, startSeconds) => yt.loadVideoById({ videoId, startSeconds }),
    play: () => yt.playVideo(),
    pause: () => yt.pauseVideo(),
    seekTo: (seconds) => yt.seekTo(seconds, true),
    setPlaybackRate: (rate) => yt.setPlaybackRate(rate),
    getCurrentTime: () => yt.getCurrentTime() ?? 0,
    getDuration: () => yt.getDuration() ?? 0,
    mute: () => yt.mute(),
    unMute: () => yt.unMute(),
    setVolume: (v) => yt.setVolume(Math.max(0, Math.min(100, v))),
    getIframe: () => (yt.getIframe ? yt.getIframe() : null),
    getState: () => yt.getPlayerState() ?? -1,
    getVideoData: () => (yt.getVideoData ? yt.getVideoData() : {}),
    destroy: () => yt.destroy(),
  }
}

// ── Minimal type declarations for the IFrame API ──────────────────────────

interface YTPlayerInstance {
  loadVideoById(opts: { videoId: string; startSeconds?: number }): void
  playVideo(): void
  pauseVideo(): void
  seekTo(seconds: number, allowSeekAhead: boolean): void
  setPlaybackRate(rate: number): void
  getCurrentTime(): number
  getDuration(): number
  getPlayerState(): number
  mute(): void
  unMute(): void
  setVolume(v: number): void
  getIframe?: () => HTMLIFrameElement | null
  getVideoData?: () => { author?: string; title?: string; video_id?: string }
  destroy(): void
}

interface YTNamespace {
  Player: new (
    el: string | HTMLElement,
    config: Record<string, unknown>,
  ) => YTPlayerInstance
}

declare global {
  interface Window {
    YT?: YTNamespace
    onYouTubeIframeAPIReady?: () => void
  }
}

export interface SearchHit {
  videoId: string
  title: string
  duration: number
}

/** YouTube search via the self-hosted yt-dlp proxy (same-origin via Caddy → relay). */
export async function searchYouTube(query: string): Promise<SearchHit[]> {
  try {
    const res = await fetch(`/yt-search?q=${encodeURIComponent(query)}`)
    if (!res.ok) return []
    const data = (await res.json()) as { id: string; title: string; duration: number }[]
    return data.map((d) => ({ videoId: d.id, title: d.title, duration: d.duration || 0 }))
  } catch {
    return []
  }
}

/** Extracts the playlist id (`list=`) from a YouTube URL, if present. */
export function parseYouTubePlaylistId(input: string): string | null {
  const m = input.trim().match(/[?&]list=([\w-]{10,64})/)
  return m ? m[1] : null
}

/** Loads a YouTube playlist via the self-hosted yt-dlp proxy (same-origin). */
export async function fetchYouTubePlaylist(listId: string): Promise<SearchHit[]> {
  try {
    const res = await fetch(`/yt-playlist?list=${encodeURIComponent(listId)}`)
    if (!res.ok) return []
    const data = (await res.json()) as { id: string; title: string; duration: number }[]
    return data.map((d) => ({ videoId: d.id, title: d.title, duration: d.duration || 0 }))
  } catch {
    return []
  }
}

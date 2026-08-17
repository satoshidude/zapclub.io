// Small, dependency-free safety helpers for foreign-controlled data
// (other users' Nostr events: profile pictures, club images).

/**
 * Only lets http/https URLs through; anything else (javascript:, data:, file:, …)
 * falls back to `fallback`. Guards against scheme injection in foreign-controlled
 * image URLs and against tracking/deanonymization pixels to arbitrary hosts.
 */
export function safeImageUrl(url: string | undefined | null, fallback: string): string {
  if (!url) return fallback
  try {
    const u = new URL(url, location.href)
    return u.protocol === 'http:' || u.protocol === 'https:' ? url : fallback
  } catch {
    return fallback
  }
}

/** YouTube video id: exactly 11 chars from [A-Za-z0-9_-]. */
const VIDEO_ID_RE = /^[A-Za-z0-9_-]{11}$/
export function isValidVideoId(id: string): boolean {
  return VIDEO_ID_RE.test(id)
}

/** Extracts a YouTube video id from a URL or bare id, or null. */
export function parseVideoId(input: string): string | null {
  const s = input.trim()
  if (isValidVideoId(s)) return s
  try {
    const u = new URL(s)
    if (u.hostname === 'youtu.be') {
      const id = u.pathname.slice(1)
      return isValidVideoId(id) ? id : null
    }
    if (u.hostname.endsWith('youtube.com')) {
      const v = u.searchParams.get('v')
      if (v && isValidVideoId(v)) return v
    }
  } catch {
    /* not a url */
  }
  return null
}

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

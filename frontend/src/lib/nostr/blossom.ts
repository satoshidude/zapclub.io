import { signEvent } from './nostrLogin'

// Custom track images live on a Blossom server (nostr-native blob storage, BUD-01/02) — NOT on
// the club relay and not in any central zapclub DB. For now we use an existing public server;
// a self-hosted zapclub Blossom is a follow-up (see tasks/track-preview-feature.md). Override
// the server per-user via localStorage `zapclub:blossom`.
const DEFAULT_BLOSSOM = 'https://blossom.band'
const SERVER_KEY = 'zapclub:blossom'

export function blossomServer(): string {
  try {
    return (localStorage.getItem(SERVER_KEY) || DEFAULT_BLOSSOM).replace(/\/+$/, '')
  } catch {
    return DEFAULT_BLOSSOM
  }
}

async function sha256hex(buf: ArrayBuffer): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', buf)
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('')
}

/**
 * Uploads a blob to the user's Blossom server (BUD-02 PUT /upload) and returns its public URL.
 * Auth is a NIP-signed kind-24242 event (the user's own Nostr key) — base64'd in the
 * `Authorization: Nostr …` header. Throws on failure (caller shows the message).
 */
export async function uploadToBlossom(file: Blob): Promise<string> {
  const server = blossomServer()
  const buf = await file.arrayBuffer()
  const hash = await sha256hex(buf)
  const now = Math.floor(Date.now() / 1000)
  const auth = await signEvent({
    kind: 24242,
    created_at: now,
    tags: [
      ['t', 'upload'],
      ['x', hash],
      ['expiration', String(now + 300)],
    ],
    content: 'Upload zapclub track image',
  })
  const res = await fetch(`${server}/upload`, {
    method: 'PUT',
    headers: {
      Authorization: `Nostr ${btoa(JSON.stringify(auth))}`,
      'Content-Type': file.type || 'application/octet-stream',
    },
    body: file,
  })
  if (!res.ok) {
    throw new Error(`Upload failed (${res.status}). Try a different image or Blossom server.`)
  }
  const desc = (await res.json()) as { url?: string }
  if (!desc?.url) throw new Error('Blossom returned no URL.')
  return desc.url
}

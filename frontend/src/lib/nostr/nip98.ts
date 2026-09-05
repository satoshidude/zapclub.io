import { signEvent } from './nostrLogin'

/** Builds a single-use NIP-98 Authorization header for the exact HTTP operation. */
export async function nip98Header(url: string, method: string): Promise<string> {
  const event = await signEvent({
    kind: 27235,
    created_at: Math.floor(Date.now() / 1000),
    tags: [
      ['u', url],
      ['method', method],
    ],
    content: '',
  })
  return 'Nostr ' + btoa(JSON.stringify(event))
}

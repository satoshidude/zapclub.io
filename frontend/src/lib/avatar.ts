import { createAvatar } from '@dicebear/core'
import { rings } from '@dicebear/collection'

// Generated club image derived from the owner's pubkey (or the club id as fallback).
// DiceBear "rings" — concentric colored rings, evoking a vinyl record. Memoized.
const cache = new Map<string, string>()

export function clubAvatar(seed: string): string {
  if (!seed) return ''
  let uri = cache.get(seed)
  if (!uri) {
    uri = createAvatar(rings, { seed }).toDataUri()
    cache.set(seed, uri)
  }
  return uri
}

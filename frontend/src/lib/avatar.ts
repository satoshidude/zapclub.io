import { createAvatar } from '@dicebear/core'
import { rings } from '@dicebear/collection'

// Generated club image derived from the owner's pubkey (or the club id as fallback).
// DiceBear "rings" — concentric colored rings, evoking a vinyl record. Memoized.
// Palette constrained to the zapclub Lightning theme: amber / orange / yellow.
const RING_COLORS = [
  'ffd54f', // amber
  'ffb300', // amber dark
  'ff8f00', // orange
  'ff6f00', // deep orange
  'ffca28', // yellow-amber
  'fb8c00', // orange 600
  'ffa726', // orange 400
  'ffe082', // light amber
  'f57c00', // orange 700
  'fff176', // yellow
]

const cache = new Map<string, string>()

export function clubAvatar(seed: string): string {
  if (!seed) return ''
  let uri = cache.get(seed)
  if (!uri) {
    uri = createAvatar(rings, { seed, ringColor: RING_COLORS }).toDataUri()
    cache.set(seed, uri)
  }
  return uri
}

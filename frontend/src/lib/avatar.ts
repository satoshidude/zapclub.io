import { createAvatar } from '@dicebear/core'
import { rings } from '@dicebear/collection'

// Generated club image derived from the owner's pubkey (or the club id as fallback).
// DiceBear "rings" — concentric colored rings, evoking a vinyl record. Memoized.
// Palette sampled from the turntable artwork and extended with Bitcoin orange + complementary tones.
const BACKGROUND_COLORS = ['060405']

const RING_COLORS = [
  '752bf0', // electric violet
  '973df9', // bright violet
  'a454fe', // neon violet
  'bd76fd', // light violet
  'f1d12d', // turntable LED gold
  'ffeb63', // yellow highlight
  'f7931a', // Bitcoin orange
  '1a8bf7', // blue complement
  '4cff6a', // lime complement
]

const cache = new Map<string, string>()

export function clubAvatar(seed: string): string {
  if (!seed) return ''
  let uri = cache.get(seed)
  if (!uri) {
    uri = createAvatar(rings, {
      seed,
      backgroundColor: BACKGROUND_COLORS,
      ringColor: RING_COLORS,
    }).toDataUri()
    cache.set(seed, uri)
  }
  return uri
}

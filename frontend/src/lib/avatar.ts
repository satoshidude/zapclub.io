// Generated club artwork derived from the owner's pubkey (or the club id as fallback).
// The centered LED waveform remains recognisable in both square list crops and the wide club hero.
const BACKGROUND_COLOR = '060405'
const COVER_COLORS = [
  '752bf0',
  '973df9',
  'a454fe',
  'bd76fd',
  'f4e04d',
  'ffeb63',
  'f7931a',
  '1a8bf7',
  '4cff6a',
]

const cache = new Map<string, string>()

function seedHash(value: string): number {
  let hash = 2166136261
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return hash >>> 0
}

function seededRandom(seed: number): () => number {
  let state = seed
  return () => {
    state += 0x6d2b79f5
    let value = state
    value = Math.imul(value ^ (value >>> 15), value | 1)
    value ^= value + Math.imul(value ^ (value >>> 7), value | 61)
    return ((value ^ (value >>> 14)) >>> 0) / 4294967296
  }
}

function waveformSvg(seed: string): string {
  const random = seededRandom(seedHash(seed))
  const palette = [...COVER_COLORS]
  for (let index = palette.length - 1; index > 0; index -= 1) {
    const swapIndex = Math.floor(random() * (index + 1))
    const current = palette[index]
    palette[index] = palette[swapIndex]
    palette[swapIndex] = current
  }
  const accentColumn = Math.floor(random() * 11)
  const bars = Array.from({ length: 11 }, (_, index) => {
    const height = 20 + Math.floor(random() * 57)
    const x = 3 + index * 9
    const y = Math.round((100 - height) / 2)
    const color = index === accentColumn ? 'f4e04d' : palette[index % palette.length]
    const opacity = (0.58 + random() * 0.38).toFixed(2)
    return `<rect x="${x}" y="${y}" width="6" height="${height}" fill="#${color}" opacity="${opacity}"/>`
  }).join('')
  const railY = 18 + Math.floor(random() * 65)
  const railColor = palette[1] ?? COVER_COLORS[0]

  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100" preserveAspectRatio="xMidYMid slice" shape-rendering="crispEdges"><rect width="100" height="100" fill="#${BACKGROUND_COLOR}"/><path d="M0 20H100M0 40H100M0 60H100M0 80H100" stroke="#f1f3f4" stroke-opacity=".055"/><path d="M20 0V100M40 0V100M60 0V100M80 0V100" stroke="#f1f3f4" stroke-opacity=".035"/>${bars}<rect y="${railY}" width="100" height="2" fill="#${railColor}" opacity=".42"/><rect y="50" width="100" height="1" fill="#f1f3f4" opacity=".2"/></svg>`
}

export function clubAvatar(seed: string): string {
  if (!seed) return ''
  let uri = cache.get(seed)
  if (!uri) {
    uri = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(waveformSvg(seed))}`
    cache.set(seed, uri)
  }
  return uri
}

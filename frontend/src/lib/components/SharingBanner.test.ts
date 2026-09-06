import { describe, expect, it } from 'vitest'
import generator from '../../../scripts/gen-assets.mjs?raw'
import index from '../../../index.html?raw'

describe('Open Graph sharing banner', () => {
  it('carries the homepage promise, all three features and the club CTA', () => {
    expect(generator).toContain("textPath(jersey, 'DROP IN.'")
    expect(generator).toContain("textPath(jersey, 'TAKE THE STAGE.'")
    expect(generator).toContain("textPath(jersey, 'OWN THE NIGHT.'")
    expect(generator).toContain("textPath(plexMono, 'deck conductor'")
    expect(generator).toContain("textPath(plexMono, 'Zap the DJ'")
    expect(generator).toContain("textPath(plexMono, 'nostr driven experience'")
    expect(generator).toContain("const ctaText = 'ENTER CLUB'")
    expect(generator).toContain('textPath(dotGothic, ctaText, ctaTextX')
  })

  it('uses the same local font families as the website', () => {
    expect(generator).toContain("fontFile('jersey-25', 'jersey-25-latin-400-normal.woff')")
    expect(generator).toContain("fontFile('ibm-plex-mono', 'ibm-plex-mono-latin-500-normal.woff')")
    expect(generator).toContain("fontFile('dotgothic16', 'dotgothic16-latin-400-normal.woff')")
  })

  it('uses the website LED blue and right-aligns the CTA block', () => {
    expect(generator).toContain("const ledBlue = '#cfe9ff'")
    expect(generator).toContain('const cta = { x: 836, y: 506, width: 344, height: 64 }')
    expect(generator).toContain('const ctaTextX = cta.x + (cta.width - dotGothic.width')
    expect(generator).toContain("const ctaTextColor = '#0d1f42'")
  })

  it('publishes the standard large-card dimensions and descriptive alt text', () => {
    expect(index).toContain('https://zapclub.io/og-share-v3.png')
    expect(index).toContain('<meta property="og:image:width" content="1200" />')
    expect(index).toContain('<meta property="og:image:height" content="630" />')
    expect(index).toContain('Deck conductor, Zap the DJ, Nostr driven experience. Enter club.')
  })
})

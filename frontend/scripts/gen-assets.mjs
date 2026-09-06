// Generates all brand raster assets from the turntable artwork — none with the old white box.
//   • og-share-v3.png (1200×630)   link/social preview card, generated hero + exact vector copy
//   • favicon.svg / -32            browser-tab favicon, TRANSPARENT (adapts to any tab theme)
//   • apple-touch-icon.png (180)   iOS home screen — SOLID dark (iOS composites alpha on black)
//   • icon-192 / icon-512.png      Android / PWA install — SOLID dark (maskable needs a bg)
// Link-preview cards & tab favicons render a STATIC image (they don't animate); the in-app
// logo (Turntable.svelte) keeps spinning. Run: node scripts/gen-assets.mjs   (dev dep: sharp)
import sharp from 'sharp'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { readFileSync, writeFileSync } from 'node:fs'
import { loadWoffFont } from './lib/woff-path.mjs'

const scripts = dirname(fileURLToPath(import.meta.url))
const pub = join(scripts, '..', 'public')

// The turntable from the logo (Turntable.svelte geometry: vinyl centred at 16,20 r13; tonearm
// top-right), with NO background so it can sit on any backdrop. cx/cy/s position + scale it.
const turntable = (cx, cy, s) => `
  <g transform="translate(${cx},${cy}) scale(${s}) translate(-16,-20)">
    <circle cx="16" cy="20" r="13" fill="#1b0b33" stroke="#8e30eb" stroke-width="1.6"/>
    <circle cx="16" cy="20" r="9.5" fill="none" stroke="#a855f7" stroke-width="0.5" opacity="0.4"/>
    <circle cx="16" cy="20" r="6.5" fill="none" stroke="#a855f7" stroke-width="0.5" opacity="0.3"/>
    <circle cx="16" cy="20" r="3.6" fill="#22c55e"/>
    <circle cx="16" cy="11.5" r="1.1" fill="#d8b4fe"/>
    <circle cx="16" cy="20" r="1" fill="#1b0b33"/>
    <line x1="29" y1="7" x2="20.5" y2="15.5" stroke="#c084fc" stroke-width="1.7" stroke-linecap="round"/>
    <circle cx="29" cy="7" r="1.9" fill="#c084fc"/>
  </g>`

// ── social/link-preview card (1200×630) ─────────────────────────────────────
// The Webfonts are converted to SVG outlines in-process. This uses the exact site typography
// without relying on host-installed fonts, and keeps generation identical on macOS and Linux.
const fontFile = (family, file) => join(scripts, '..', 'node_modules', '@fontsource', family, 'files', file)
const jersey = loadWoffFont(fontFile('jersey-25', 'jersey-25-latin-400-normal.woff'))
const plexMono = loadWoffFont(fontFile('ibm-plex-mono', 'ibm-plex-mono-latin-500-normal.woff'))
const dotGothic = loadWoffFont(fontFile('dotgothic16', 'dotgothic16-latin-400-normal.woff'))
const textPath = (font, text, x, baseline, size, fill, letterSpacing = 0, glow = false) =>
  `<path d="${font.path(text, x, baseline, size, letterSpacing)}" fill="${fill}"${glow ? ' filter="url(#soft-glow)"' : ''}/>`
const ledBlue = '#cfe9ff'
const ctaText = 'ENTER CLUB'
const cta = { x: 836, y: 506, width: 344, height: 64 }
const ctaTextX = cta.x + (cta.width - dotGothic.width(ctaText, 31, 0.5)) / 2
const ctaBg = '#f4e04d'
const ctaTextColor = '#0d1f42'
// Website-native metrics, uniformly scaled for the social card.
const featureScale = 1.5
const featureSecondX = 26 + 2 + 1.6 + plexMono.width('deck conductor', 16, 0) + 5.12
const featureBaseline = 20.5
const featureRow2 = 29 + 1.92
const clubListSource = readFileSync(join(scripts, '..', 'src/lib/components/ClubList.svelte'), 'utf8')
const syncIcon = clubListSource.match(/<svg class="hero-feature-icon hero-feature-sync"[^>]*>([\s\S]*?)<\/svg>/)[1]

const nostrich = readFileSync(join(pub, 'nostrich.png')).toString('base64')
const cardOverlay = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630">
  <defs>
  <linearGradient id="copy-shade" x1="0" x2="1">
      <stop offset="0" stop-color="#050508" stop-opacity="0.88"/>
      <stop offset="0.68" stop-color="#050508" stop-opacity="0.7"/>
      <stop offset="1" stop-color="#050508" stop-opacity="0"/>
    </linearGradient>
    <filter id="soft-glow" x="-20%" y="-20%" width="140%" height="140%">
      <feGaussianBlur stdDeviation="2.2" result="blur"/>
      <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
    <filter id="copy-glow" x="-30%" y="-100%" width="160%" height="300%">
      <feGaussianBlur in="SourceAlpha" stdDeviation="1.5" result="near"/>
      <feFlood flood-color="#b3deff" flood-opacity="0.8"/>
      <feComposite in2="near" operator="in" result="near-color"/>
      <feGaussianBlur in="SourceAlpha" stdDeviation="5" result="far"/>
      <feFlood flood-color="#5aa0ff" flood-opacity="0.25"/>
      <feComposite in2="far" operator="in"/>
      <feMerge><feMergeNode/><feMergeNode in="near-color"/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
    <filter id="logo-glow" x="-50%" y="-50%" width="200%" height="200%">
      <feDropShadow dx="0" dy="0" stdDeviation="3" flood-color="#8e30eb" flood-opacity="0.55"/>
    </filter>
    <mask id="nostr-led-mask" mask-type="alpha">
    <image href="data:image/png;base64,${nostrich}" x="0" y="0" width="51" height="41"/>
    </mask>
  </defs>
  <rect width="790" height="630" fill="url(#copy-shade)"/>
  <g transform="translate(58 40) scale(1.5)">
    <g filter="url(#logo-glow)">${turntable(16 * 32 / 36, 20 * 32 / 36, 32 / 36)}</g>
    <g stroke="#f4e04d" stroke-width="0.25" filter="url(#copy-glow)">
      ${textPath(jersey, 'ZAPCLUB.IO', 40, 23, 20, '#f4e04d', 1.6)}
    </g>
  </g>
  ${textPath(jersey, 'DROP IN.', 58, 159, 82, ledBlue, 0, true)}
  ${textPath(jersey, 'TAKE THE STAGE.', 58, 225, 82, ledBlue, 0, true)}
  ${textPath(jersey, 'OWN THE NIGHT.', 58, 291, 82, ledBlue, 0, true)}

  <g transform="translate(58 340) scale(${featureScale})">
    <svg width="26" height="26" y="1.5" viewBox="0 0 24 24" fill="none" stroke="${ledBlue}" stroke-width="1.65" stroke-linecap="square" stroke-linejoin="miter">${syncIcon}</svg>
    <g filter="url(#copy-glow)">
      ${textPath(plexMono, 'deck conductor', 29.6, featureBaseline, 16, ledBlue)}
      <text x="${featureSecondX + 14.5}" y="20.5" fill="${ledBlue}" font-size="24" font-weight="700" font-family="ui-sans-serif, system-ui, sans-serif" text-anchor="middle">⚡︎</text>
      ${textPath(plexMono, 'Zap the DJ', featureSecondX + 27.6, featureBaseline, 16, ledBlue)}
      ${textPath(plexMono, 'nostr driven experience', 30.6, featureRow2 + 26.5, 16, ledBlue)}
    </g>
    <g transform="translate(-12 ${featureRow2})">
      <rect width="51" height="41" fill="${ledBlue}" mask="url(#nostr-led-mask)"/>
    </g>
  </g>

  <rect x="${cta.x}" y="${cta.y}" width="${cta.width}" height="${cta.height}" rx="10" ry="10" fill="${ctaBg}" fill-opacity="1"/>
  ${textPath(dotGothic, ctaText, ctaTextX, 553, 31, ctaTextColor, 0.5, true)}
</svg>`

// ── browser-tab favicon: TRANSPARENT, just the turntable (no box) ────────────
const faviconSvg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 36 36" width="36" height="36" role="img" aria-label="zapclub turntable">${turntable(16, 20, 1)}
</svg>`

// ── solid dark square (full-bleed, no rounding so iOS/Android apply their own mask) ──
const darkSquare = (size) => `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
  <rect width="${size}" height="${size}" fill="#0b0a10"/>
  ${turntable(size / 2, size * 0.547, (size / 512) * 15)}
</svg>`
writeFileSync(join(pub, 'favicon.svg'), faviconSvg)
await sharp(join(scripts, 'assets', 'og-sharing-background.png'))
  .resize(1200, 630, { fit: 'cover', position: 'centre' })
  .composite([{ input: Buffer.from(cardOverlay) }])
  .png({ compressionLevel: 9 })
  .toFile(join(pub, 'og-share-v3.png'))
await sharp(Buffer.from(faviconSvg)).resize(32).png().toFile(join(pub, 'favicon-32.png'))
await sharp(Buffer.from(darkSquare(180))).png().toFile(join(pub, 'apple-touch-icon.png'))
await sharp(Buffer.from(darkSquare(192))).png().toFile(join(pub, 'icon-192.png'))
await sharp(Buffer.from(darkSquare(512))).png().toFile(join(pub, 'icon-512.png'))
console.log('wrote og-share-v3.png, favicon.svg/-32, apple-touch-icon.png, icon-192/512.png')

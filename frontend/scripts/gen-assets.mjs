// Generates all brand raster assets from the turntable artwork — none with the old white box.
//   • og.png (1200×630)            link/social preview card, dark brand background
//   • favicon.svg / -32 / -96      browser-tab favicon, TRANSPARENT (adapts to any tab theme)
//   • apple-touch-icon.png (180)   iOS home screen — SOLID dark (iOS composites alpha on black)
//   • icon-192 / icon-512.png      Android / PWA install — SOLID dark (maskable needs a bg)
//   • turntable-dark.png (512)     generic dark square icon
// Link-preview cards & tab favicons render a STATIC image (they don't animate); the in-app
// logo (Turntable.svelte) keeps spinning. Run: node scripts/gen-assets.mjs   (dev dep: sharp)
import sharp from 'sharp'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { writeFileSync } from 'node:fs'

const pub = join(dirname(fileURLToPath(import.meta.url)), '..', 'public')

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

// ── social/link-preview card (1200×630, dark) ────────────────────────────────
const card = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630">
  <defs>
    <radialGradient id="g" cx="28%" cy="42%" r="80%">
      <stop offset="0%" stop-color="#1a0c2b"/><stop offset="60%" stop-color="#0d0913"/><stop offset="100%" stop-color="#07070a"/>
    </radialGradient>
  </defs>
  <rect width="1200" height="630" fill="url(#g)"/>
  ${turntable(300, 315, 13)}
  <text x="560" y="300" font-family="Helvetica, Arial, sans-serif" font-size="92" font-weight="800" letter-spacing="-3"><tspan fill="#ffffff">zapclub</tspan><tspan fill="#8e30eb">.io</tspan></text>
  <text x="562" y="360" font-family="Helvetica, Arial, sans-serif" font-size="34" font-weight="600" fill="#b9a9d4">Decentralized social music streaming</text>
  <text x="562" y="408" font-family="Helvetica, Arial, sans-serif" font-size="27" font-weight="400" fill="#8a7da6">Open a club that belongs to you · zap your crew</text>
</svg>`

// ── browser-tab favicon: TRANSPARENT, just the turntable (no box) ────────────
const faviconSvg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 36 36" width="36" height="36" role="img" aria-label="zapclub turntable">${turntable(16, 20, 1)}
</svg>`

// ── solid dark square (full-bleed, no rounding so iOS/Android apply their own mask) ──
const darkSquare = (size) => `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
  <rect width="${size}" height="${size}" fill="#0b0a10"/>
  ${turntable(size / 2, size * 0.547, (size / 512) * 15)}
</svg>`
// rounded variant for the generic standalone icon
const darkRounded = `<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 512 512">
  <rect width="512" height="512" rx="110" fill="#0b0a10"/>${turntable(256, 280, 15)}
</svg>`

writeFileSync(join(pub, 'favicon.svg'), faviconSvg)
await sharp(Buffer.from(card)).png().toFile(join(pub, 'og.png'))
await sharp(Buffer.from(faviconSvg)).resize(96).png().toFile(join(pub, 'favicon-96.png'))
await sharp(Buffer.from(faviconSvg)).resize(32).png().toFile(join(pub, 'favicon-32.png'))
await sharp(Buffer.from(darkSquare(180))).png().toFile(join(pub, 'apple-touch-icon.png'))
await sharp(Buffer.from(darkSquare(192))).png().toFile(join(pub, 'icon-192.png'))
await sharp(Buffer.from(darkSquare(512))).png().toFile(join(pub, 'icon-512.png'))
await sharp(Buffer.from(darkRounded)).png().toFile(join(pub, 'turntable-dark.png'))
console.log('wrote og.png, favicon.svg/-32/-96, apple-touch-icon.png, icon-192/512.png, turntable-dark.png')

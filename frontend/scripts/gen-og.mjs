// Generates the social/link-preview card (og.png) and a dark square icon (turntable-dark.png)
// from the brand turntable artwork — on the dark brand background, NOT the white app-icon square.
// Link-preview cards render a STATIC image (they never animate), so this is the still we ship.
// Run: node scripts/gen-og.mjs   (needs the dev dependency `sharp`)
import sharp from 'sharp'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const pub = join(dirname(fileURLToPath(import.meta.url)), '..', 'public')

// The turntable from the logo (Turntable.svelte geometry: vinyl centred at 16,20 r13; tonearm
// top-right). Drawn with no background so we can place it on any backdrop.
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

const bg = (w, h) => `
  <defs>
    <radialGradient id="g" cx="28%" cy="42%" r="80%">
      <stop offset="0%" stop-color="#1a0c2b"/>
      <stop offset="60%" stop-color="#0d0913"/>
      <stop offset="100%" stop-color="#07070a"/>
    </radialGradient>
  </defs>
  <rect width="${w}" height="${h}" fill="url(#g)"/>`

// 1200×630 landscape card (summary_large_image) — turntable left, wordmark + tagline right.
const card = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630">
  ${bg(1200, 630)}
  ${turntable(300, 315, 13)}
  <text x="560" y="300" font-family="Helvetica, Arial, sans-serif" font-size="92" font-weight="800" letter-spacing="-3">
    <tspan fill="#ffffff">zapclub</tspan><tspan fill="#8e30eb">.io</tspan>
  </text>
  <text x="562" y="360" font-family="Helvetica, Arial, sans-serif" font-size="34" font-weight="600" fill="#b9a9d4">Decentralized social music streaming</text>
  <text x="562" y="408" font-family="Helvetica, Arial, sans-serif" font-size="27" font-weight="400" fill="#8a7da6">Open a club that belongs to you · zap your crew</text>
</svg>`

// 512×512 dark square icon (summary card / anywhere a square is wanted) — turntable on the
// brand-dark rounded square instead of white.
const square = `<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 512 512">
  <rect width="512" height="512" rx="110" fill="#0b0a10"/>
  ${turntable(256, 280, 15)}
</svg>`

await sharp(Buffer.from(card)).png().toFile(join(pub, 'og.png'))
await sharp(Buffer.from(square)).png().toFile(join(pub, 'turntable-dark.png'))
console.log('wrote public/og.png (1200×630) and public/turntable-dark.png (512×512)')

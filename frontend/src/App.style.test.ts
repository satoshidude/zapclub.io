import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import app from './App.svelte?raw'

const styles = readFileSync(new URL('./app.css', import.meta.url), 'utf8')

describe('topbar wordmark typography', () => {
  it('shares the card-title treatment and is exactly two pixels larger', () => {
    expect(app).toContain('class="topbar-wordmark lcd-card-title"')
    expect(styles).toMatch(
      /body\.site-led-page :is\(h2, h3\)\.lcd-card-title,\s*body\.site-led-page \.topbar-wordmark\.lcd-card-title,/,
    )
    expect(styles).toMatch(
      /body\.site-led-page \.topbar-wordmark\.lcd-card-title\s*\{\s*font-size: 20px !important;/,
    )
    expect(styles).toMatch(
      /:is\(html\[data-zap-led-theme='blue'\], html\[data-zap-led-theme='black'\]\) body\.site-led-page \.topbar-wordmark\.lcd-card-title\s*\{\s*--lcd-text-shadow: var\(--blue-lcd-shadow\);/,
    )
    expect(styles).toMatch(/\.lcd-card-title\s*\{[\s\S]*?font-family: var\(--font-headline\);[\s\S]*?font-size: 18px;[\s\S]*?-webkit-text-stroke: 0\.25px currentColor;/)
  })
})

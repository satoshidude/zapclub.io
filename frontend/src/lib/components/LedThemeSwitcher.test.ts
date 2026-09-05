/// <reference types="node" />

import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import switcher from './LedThemeSwitcher.svelte?raw'

const appCss = readFileSync(new URL('../../app.css', import.meta.url), 'utf8')

describe('LED card themes', () => {
  it('keeps the blue switch mapped and black as the default theme', () => {
    expect(switcher).toContain("type LedTheme = 'red' | 'green' | 'blue' | 'portal' | 'black'")
    expect(switcher).toContain("{ id: 'blue', label: 'Blue'")
    expect(switcher).toContain("let active = $state<LedTheme>('black')")
    expect(switcher).toContain("applyTheme(isTheme(saved) ? saved : 'black', false)")
    expect(switcher).toContain('document.documentElement.dataset.zapLedTheme = theme')
    expect(switcher).toContain('localStorage.setItem(STORAGE_KEY, theme)')
  })

  it('uses the reference LCD gradient on LED zones and legacy cards only in blue', () => {
    expect(appCss).toContain("html[data-zap-led-theme='blue'] {")
    expect(appCss).toContain('--card-led-a: #2f5aa8;')
    expect(appCss).toContain('--card-led-b: #1c3e78;')
    expect(appCss).toContain('--card-led-c: #0d1f42;')
    expect(appCss).toContain("html[data-zap-led-theme='blue'] body.site-led-page :is(.led-zone, .card) {")
    expect(appCss).toContain('repeating-linear-gradient(180deg, rgba(0, 0, 0, 0.12) 0 1px, transparent 1px 3px)')
    expect(appCss).toContain('radial-gradient(120% 140% at 30% 15%, var(--card-led-a) 0%, var(--card-led-b) 45%, var(--card-led-c) 100%) !important;')
  })

  it('shares readable LCD copy between blue and default black without changing typography metrics', () => {
    const sharedCopyRule = appCss.match(
      /:is\(html\[data-zap-led-theme='blue'\], html\[data-zap-led-theme='black'\]\) body\.site-led-page :is\(\.led-zone, \.card\) \{([\s\S]*?)\n\}/,
    )?.[1]
    expect(sharedCopyRule).toBeDefined()
    expect(sharedCopyRule).toContain('--lcd-text: #cfe9ff;')
    expect(sharedCopyRule).toContain('--lcd-text-soft: #b7d8ff;')
    expect(sharedCopyRule).toContain('--lcd-text-dim: #b7d8ff;')
    expect(sharedCopyRule).toContain('--text: var(--lcd-text);')
    expect(sharedCopyRule).toContain('--text-dim: var(--lcd-text-dim);')
    expect(sharedCopyRule).toContain('--lcd-copy-shadow: var(--blue-lcd-shadow);')
    expect(appCss).toContain('--blue-lcd-shadow: 0 0 3px rgba(179, 222, 255, 0.8), 0 0 10px rgba(90, 160, 255, 0.25);')
    expect(sharedCopyRule).toContain('color: var(--lcd-text);')
    expect(sharedCopyRule).toContain('text-shadow: var(--lcd-copy-shadow);')
    expect(sharedCopyRule).not.toMatch(/font-(?:family|size|style|weight)|letter-spacing|line-height|margin|padding|border-radius|background/)

    expect(appCss).toContain("html[data-zap-led-theme='black'] {")
    expect(appCss).toContain('--card-led-a: transparent;')
  })
})

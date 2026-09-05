/// <reference types="node" />

import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import clubList from './ClubList.svelte?raw'
import clubView from './ClubView.svelte?raw'

const appCss = readFileSync(new URL('../../app.css', import.meta.url), 'utf8')

function selectorsUsing(declaration: string): string {
  return [...appCss.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
    .filter(([, , body]) => body.includes(declaration))
    .map(([, selectors]) => selectors)
    .join('\n')
}

describe('card grid spacing', () => {
  it('matches the gap below the club player to the adjacent card gap', () => {
    expect(clubView).toMatch(/\.player-section \{[\s\S]*?margin-top: 0\.7rem;[\s\S]*?margin-bottom: 0\.7rem;/)
    expect(clubView).toMatch(/\.stage-grid \{[\s\S]*?gap: 0\.7rem;/)
    expect(clubView).toMatch(/@media \(max-width: 560px\)[\s\S]*?\.player-section \{[\s\S]*?margin-top: 0\.55rem;[\s\S]*?margin-bottom: 0\.55rem;/)
  })

  it('keeps the home page cards on the same compact rhythm', () => {
    expect(clubList).toMatch(/:global\(body\.site-led-page\) \.hero \{[\s\S]*?margin-bottom: 0\.7rem;/)
    expect(clubList).toMatch(/:global\(body\.site-led-page\) \.tg-block \{[\s\S]*?margin-top: 0\.7rem;/)
    expect(clubList).toMatch(/@media \(max-width: 560px\)[\s\S]*?:global\(body\.site-led-page\) \.hero \{[\s\S]*?margin-bottom: 0\.55rem;/)
    expect(clubList).toMatch(/@media \(max-width: 560px\)[\s\S]*?:global\(body\.site-led-page\) \.tg-block \{[\s\S]*?margin-top: 0\.55rem;/)
  })

  it('does not let the roomy text-page rhythm override card grids', () => {
    const wideSpacingTargets = [
      selectorsUsing('margin-top: 64px !important;'),
      selectorsUsing('gap: 64px !important;'),
      selectorsUsing('margin-top: 40px !important;'),
      selectorsUsing('gap: 40px !important;'),
    ].join('\n')

    expect(wideSpacingTargets).not.toContain('.home-page')
    expect(wideSpacingTargets).not.toContain('.club-page')
  })
})

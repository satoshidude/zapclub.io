import { describe, expect, it } from 'vitest'
import main from '../../main.ts?raw'
import about from './About.svelte?raw'
import admin from './AdminDashboard.svelte?raw'
import clubList from './ClubList.svelte?raw'
import clubView from './ClubView.svelte?raw'
import disclaimer from './Disclaimer.svelte?raw'
import howTo from './HowTo.svelte?raw'
import leaderboard from './Leaderboard.svelte?raw'
import profile from './UserProfile.svelte?raw'

describe('site typography contract', () => {
  it('loads both local font families in the application entry point', () => {
    expect(main).toContain("@fontsource/dotgothic16/latin.css")
    expect(main).toContain("@fontsource/ibm-plex-mono/latin-400.css")
    expect(main).toContain("@fontsource/ibm-plex-mono/latin-500.css")
    expect(main).toContain("@fontsource/jersey-25/latin.css")
  })

  it('marks every page h1 for the shared display treatment', () => {
    for (const source of [about, admin, clubList, clubView, disclaimer, howTo, leaderboard, profile]) {
      expect(source).toMatch(/<h1 class="[^"]*site-h1[^"]*">/)
    }
  })

  it('keeps the existing home and club compositions while applying the shared title hook', () => {
    expect(clubList).toContain('<h1 class="hero-title site-h1">')
    expect(clubView).toContain('<h1 class="site-h1">{club?.name')
    expect(clubView).toContain('<div class="player-section">')
  })
})

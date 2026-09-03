import { describe, expect, it } from 'vitest'
import app from './App.svelte?raw'
import router from './lib/router.svelte.ts?raw'

describe('global project footer', () => {
  it('renders once after the route-dependent main content', () => {
    expect(app.match(/<SiteFooter\s*\/>/g)).toHaveLength(1)
    expect(app).not.toMatch(/support-bar|Tip zapclub|DONATE_LUD16|donate\(/)

    const mainEnd = app.indexOf('</main>')
    const footer = app.indexOf('<SiteFooter />')
    const overlays = app.indexOf('<LoginDialog />')

    expect(mainEnd).toBeGreaterThan(-1)
    expect(footer).toBeGreaterThan(mainEnd)
    expect(overlays).toBeGreaterThan(footer)
  })

  it('uses the consolidated information page for all former footer routes', () => {
    expect(app).not.toMatch(/Credits\.svelte|Disclaimer\.svelte|Privacy\.svelte|Terms\.svelte|LegalNotice\.svelte/)
    expect(app.match(/router\.route\.name === '(?:about|credits|disclaimer|privacy|terms|legal)'[\s\S]*?<About \/>/g)).toHaveLength(6)
  })

  it('covers every route rendered by the app shell', () => {
    const routeNames = [...router.matchAll(/\| \{ name: '([^']+)'/g)].map(([, name]) => name)
    const conditionalRoutes = [...app.matchAll(/router\.route\.name === '([^']+)'/g)].map(([, name]) => name)

    expect(routeNames).toEqual([
      'home',
      'club',
      'user',
      'admin',
      'howto',
      'about',
      'credits',
      'disclaimer',
      'leaderboard',
      'privacy',
      'terms',
      'legal',
    ])
    expect(conditionalRoutes).toEqual(routeNames.filter((name) => name !== 'home'))
  })
})

import { describe, expect, it } from 'vitest'
import app from './App.svelte?raw'
import router from './lib/router.svelte.ts?raw'

describe('global project footer', () => {
  it('renders once after the route-dependent main content', () => {
    expect(app.match(/<SiteFooter\s*\/>/g)).toHaveLength(1)

    const mainEnd = app.indexOf('</main>')
    const footer = app.indexOf('<SiteFooter />')
    const overlays = app.indexOf('<LoginDialog />')

    expect(mainEnd).toBeGreaterThan(-1)
    expect(footer).toBeGreaterThan(mainEnd)
    expect(overlays).toBeGreaterThan(footer)
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

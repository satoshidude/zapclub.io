// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { parseRoute } from './router.svelte'

describe('footer content routes', () => {
  it.each([
    ['/leaderboard', 'leaderboard'],
    ['/about', 'about'],
    ['/credits', 'about'],
    ['/disclaimer', 'disclaimer'],
  ] as const)('maps %s to %s', (path, name) => {
    expect(parseRoute(path)).toEqual({ name })
    expect(parseRoute(`${path}/`)).toEqual({ name })
  })
})

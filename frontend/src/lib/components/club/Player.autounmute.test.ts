import { describe, expect, it } from 'vitest'
import playerSource from './Player.svelte?raw'

describe('player automatic unmute', () => {
  it('starts autoplay muted and unmutes each freshly loaded video once it plays', () => {
    expect(playerSource).toContain('muted: true')
    expect(playerSource).toContain('unmuteAfterNextLoad = true')
    expect(playerSource).toMatch(
      /if \(s === 1\)[\s\S]*?if \(unmuteAfterNextLoad\)[\s\S]*?if \(canHear\) unmuteLoadedVideo\(\)/,
    )
    expect(playerSource).toContain('player.unMute()')
  })

  it('also unmutes an already loaded stream when listening access is granted', () => {
    expect(playerSource).toMatch(
      /if \(!allowed\)[\s\S]*?player\.mute\(\)[\s\S]*?else \{[\s\S]*?unmuteLoadedVideo\(\)/,
    )
  })
})

import { describe, expect, it } from 'vitest'
import nowPlaying from './NowPlaying.svelte?raw'
import zapButton from './ZapButton.svelte?raw'

describe('player DJ zap action', () => {
  it('opens the zap dialog for the displayed DJ even when it is the signed-in user', () => {
    expect(nowPlaying).toContain('showSelf={true} allowSelfZap={true}')
    expect(zapButton).toContain('const canOpen = $derived(!isSelf || allowSelfZap)')
    expect(zapButton).toContain('if (canOpen) open = !open')
  })
})

import { describe, expect, it } from 'vitest'
import nowPlaying from './NowPlaying.svelte?raw'
import zapButton from './ZapButton.svelte?raw'

describe('player DJ zap action', () => {
  it('opens the zap dialog for the displayed DJ even when it is the signed-in user', () => {
    expect(nowPlaying).toContain('showSelf={true} allowSelfZap={true}')
    expect(zapButton).toContain('const canOpen = $derived(!isSelf || allowSelfZap)')
    expect(zapButton).toContain('if (canOpen) open = !open')
  })

  it('renders a dedicated collapsed-player action and keeps the DJ name actionable', () => {
    expect(nowPlaying).toContain('class="zap-stage-action"')
    expect(nowPlaying).toContain('iconLabel="ZAP THE DJ"')
    expect(nowPlaying).toContain('showRecipientName={true}')
    expect(nowPlaying).toContain('<span>DJ:</span>')
    expect(nowPlaying).toContain('hideIcon={true}')
    expect(zapButton).toContain('iconLabel || displayName(dj, djProfile)')
    expect(zapButton).toContain('class="icon-recipient-name"')
    expect(nowPlaying).toContain('animation: zap-breathe 2.4s ease-in-out infinite')
    expect(nowPlaying).toContain('color: #f7931a')
  })
})

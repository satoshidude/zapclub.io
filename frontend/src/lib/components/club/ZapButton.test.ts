import { describe, expect, it } from 'vitest'
import nowPlaying from './NowPlaying.svelte?raw'
import zapButton from './ZapButton.svelte?raw'

describe('player DJ zap action', () => {
  it('opens the zap dialog for the displayed DJ even when it is the signed-in user', () => {
    expect(nowPlaying).toMatch(/showSelf=\{true\}[\s\S]*?allowSelfZap=\{true\}/)
    expect(zapButton).toContain('const canOpen = $derived(!isSelf || allowSelfZap)')
    expect(zapButton).toContain('if (canOpen) open = !open')
  })

  it('renders a dedicated collapsed-player action with the DJ name centered there', () => {
    expect(nowPlaying).toContain('class="zap-stage-action"')
    expect(nowPlaying).toContain('iconLabel="ZAP THE DJ"')
    expect(nowPlaying).toContain('showRecipientName={true}')
    expect(nowPlaying).toContain('<span class="live-label lcd-card-title">ON AIR</span>')
    expect(nowPlaying).not.toContain('<span>DJ:</span>')
    expect(nowPlaying).not.toContain('hideIcon={true}')
    expect(zapButton).toContain('iconLabel || displayName(dj, djProfile)')
    expect(zapButton).toContain('class="icon-recipient-name" use:marquee')
    expect(zapButton).toContain('class="mq-inner"')
    expect(nowPlaying).toMatch(/\.zap-stage-action :global\(\.icon-dj-copy\) \{[\s\S]*?display: contents;/)
    expect(nowPlaying).toMatch(/\.zap-stage-action :global\(\.bolt-icon\) \{[\s\S]*?grid-row: 1;[\s\S]*?justify-self: center;/)
    expect(nowPlaying).toMatch(/\.zap-stage-action :global\(\.icon-dj-name\) \{[\s\S]*?grid-row: 2;[\s\S]*?justify-self: center;/)
    expect(nowPlaying).toMatch(/\.zap-stage-action :global\(\.icon-recipient-name\) \{[\s\S]*?grid-column: 1 \/ -1;[\s\S]*?grid-row: 3;[\s\S]*?font-size: 20px;[\s\S]*?font-weight: 700;[\s\S]*?text-align: center;/)
    expect(zapButton).toContain('.zap-mini:hover .icon-recipient-name:global([data-mq]) .mq-inner')
    expect(nowPlaying).toContain('animation: zap-breathe 2.4s ease-in-out infinite')
    expect(nowPlaying).toContain('color: #f7931a')
  })
})

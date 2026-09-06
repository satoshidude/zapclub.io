import { describe, expect, it } from 'vitest'
import nowPlaying from './NowPlaying.svelte?raw'
import zapButton from './ZapButton.svelte?raw'

describe('player DJ zap action', () => {
  it('opens the zap dialog for the displayed DJ even when it is the signed-in user', () => {
    expect(nowPlaying).toMatch(/showSelf=\{true\}[\s\S]*?allowSelfZap=\{true\}/)
    expect(zapButton).toContain('const canOpen = $derived(!isSelf || allowSelfZap)')
    expect(zapButton).toContain('if (canOpen) open = !open')
  })

  it('keeps the compact video free of permanent overlays and moves the zap trigger to the DJ line', () => {
    expect(nowPlaying).not.toContain('class="zap-stage-action"')
    expect(nowPlaying).not.toContain('iconLabel="ZAP THE DJ"')
    expect(nowPlaying).not.toContain('showRecipientName={true}')
    expect(nowPlaying).toContain('iconLabel={`⚡ ${djName}`}')
    expect(nowPlaying).toContain('hideIcon={true}')
    expect(nowPlaying).toContain('<span class="live-label lcd-card-title">ON AIR</span>')
    expect(nowPlaying).not.toContain('<span>DJ:</span>')
    expect(zapButton).toContain('iconLabel || displayName(dj, djProfile)')
    expect(zapButton).toContain('<path d="M13.2 2 5.5 13h5.7L10.8 22l7.7-11h-5.7L13.2 2z"></path>')
    expect(nowPlaying).toMatch(/<div class="lcd-media">[\s\S]*?<div\s+class="video-surface"[\s\S]*?<Player[\s\S]*?<\/div>[\s\S]*?\{#if np && !videoWide && \(miniVideoMaskVisible \|\| audioOnly\)\}[\s\S]*?<\/div>\s*<div class="lcd-content"/)
    expect(nowPlaying).toMatch(/\.video-surface\s*\{[\s\S]*?width: 100%;[\s\S]*?height: 100%;/)
    expect(nowPlaying).toMatch(/\.lcd-dj-line :global\(\.zap-mini\.icon-only\.with-name\) \{[\s\S]*?color: #f4e04d;/)
  })

  it('places the full-width player title above the two-column media row', () => {
    expect(nowPlaying).toMatch(/<section class="lcd-shell[\s\S]*?\{#if np\}[\s\S]*?<div class="lcd-status lcd-card-heading player-status">[\s\S]*?<span class="live-label lcd-card-title">ON AIR<\/span>[\s\S]*?\{\/if\}[\s\S]*?<div class="lcd-media">/)
    expect(nowPlaying.match(/<span class="live-label lcd-card-title">ON AIR<\/span>/g)).toHaveLength(1)
    expect(nowPlaying).toMatch(/<div class="lcd-status lcd-card-heading player-status">[\s\S]*?<span>-\{fmt\(remaining\)\} \/ \{fmt\(np\.duration\)\}<\/span>[\s\S]*?<\/div>/)
    expect(nowPlaying).toMatch(/grid-template-areas:\s*'status status'\s*'media content';/)
    expect(nowPlaying).toMatch(/\.lcd-shell \{[^}]*grid-template-rows: 36px auto;[^}]*height: auto;/)
    expect(nowPlaying).toMatch(/\.player-status\.lcd-card-heading \{[\s\S]*?grid-area: status;[\s\S]*?height: 100%;[\s\S]*?margin: 0;/)
    expect(nowPlaying).toMatch(/\.lcd-media \{[\s\S]*?grid-area: media;/)
    expect(nowPlaying).toMatch(/\.lcd-content \{[^}]*grid-area: content;[^}]*height: auto;/)
    expect(nowPlaying).toMatch(/\.lcd-shell\.video-wide \{[\s\S]*?grid-template-areas:\s*'status'\s*'media'\s*'content';/)
    expect(nowPlaying).toMatch(/\.lcd-shell\.idle \{[^}]*height: 158px;/)
    expect(nowPlaying).toMatch(/\.video-wide \.lcd-content \{[^}]*height: 160px;/)
    expect(nowPlaying).toMatch(/@media \(max-width: 560px\)[\s\S]*?\.lcd-shell \{[^}]*grid-template-rows: 32px auto;[^}]*height: auto;/)
    expect(nowPlaying).toMatch(/@media \(max-width: 560px\)[\s\S]*?\.lcd-shell\.idle \{[^}]*height: 126px;/)
    expect(nowPlaying).toMatch(/@media \(max-width: 560px\)[\s\S]*?\.video-wide \.lcd-content \{ height: 130px;/)
  })

  it('keeps every mobile player control large enough to touch', () => {
    expect(nowPlaying).toMatch(/@media \(max-width: 560px\)[\s\S]*?\.lcd-audio \{ width: 44px; height: 44px; \}/)
    expect(nowPlaying).toMatch(/@media \(max-width: 560px\)[\s\S]*?\.lcd-icon \{ width: 44px; height: 44px;/)
    expect(nowPlaying).toMatch(/@media \(max-width: 560px\)[\s\S]*?\.lcd-icon svg \{ width: 24px; height: 24px; \}/)
  })

  it('uses the shared blue control treatment for the volume cluster', () => {
    expect(nowPlaying).toMatch(/\.volume-cluster\s*\{[\s\S]*?color: var\(--lcd-text\);[\s\S]*?text-shadow: var\(--lcd-text-shadow\);/)
    expect(nowPlaying).toContain('drop-shadow(0 0 2px rgba(90, 160, 255, 0.42))')
    expect(nowPlaying).toContain('color-mix(in srgb, var(--lcd-text) 96%, transparent)')
  })

  it('uses the complete yellow card-title treatment for the current DJ', () => {
    expect(nowPlaying).toContain('class="lcd-dj-line"')
    expect(zapButton).toContain('class="icon-dj-name lcd-card-title"')
    expect(nowPlaying).not.toMatch(/\.lcd-dj-line\s*\{[^}]*color:/)
  })

  it('masks the compact YouTube surface for eight seconds on load and after unmuting', () => {
    expect(nowPlaying).toContain('const MINI_VIDEO_MASK_MS = 8000')
    expect(nowPlaying).toMatch(/function showMiniVideoMask\(\)[\s\S]*?setTimeout\([\s\S]*?MINI_VIDEO_MASK_MS/)
    expect(nowPlaying).toMatch(/const videoId = np\?\.videoId \|\| ''[\s\S]*?showMiniVideoMask\(\)/)
    expect(nowPlaying).toMatch(/function toggleAudio\(\)[\s\S]*?if \(controls\.muted\) showMiniVideoMask\(\)[\s\S]*?playerRef\?\.toggleAudio\(\)/)
    expect(nowPlaying).toContain('oncontrolstate={handleControlState}')
    expect(nowPlaying).toContain('onclick={toggleAudio}')
    expect(nowPlaying).toContain('const audioOnly = $derived(!!np && failedVideo === np.videoId)')
    expect(nowPlaying).toContain('{#if np && !videoWide && (miniVideoMaskVisible || audioOnly)}')
    expect(nowPlaying).toContain('class="mini-video-status"')
    expect(nowPlaying).toContain("{audioOnly ? 'AUDIO ONLY' : 'VIDEO SYNC'}")
    expect(nowPlaying).toContain('class:audio-only={audioOnly}')
    expect(nowPlaying).toMatch(/\.mini-video-status \{[\s\S]*?linear-gradient\(180deg, rgba\(2, 5, 10, 0\.08\), rgba\(2, 5, 10, 0\.44\)\);/)
    expect(nowPlaying).toMatch(/if \(failedVideo === np\.videoId\)[\s\S]*?return clubImage \|\| `https:\/\/i\.ytimg\.com\/vi\/\$\{np\.videoId\}\/mqdefault\.jpg`/)
    expect(nowPlaying).toMatch(/\.mini-video-status\.audio-only \{[\s\S]*?linear-gradient\(180deg, rgba\(2, 5, 10, 0\.08\), rgba\(2, 5, 10, 0\.52\)\);/)
    expect(nowPlaying).toMatch(/@media \(prefers-reduced-motion: reduce\)[\s\S]*?\.mini-video-status-bars span \{ height: 11px; opacity: 0\.72; animation: none; \}/)
  })
})

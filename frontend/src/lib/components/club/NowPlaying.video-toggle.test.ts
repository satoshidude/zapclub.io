import { describe, expect, it } from 'vitest'
import nowPlaying from './NowPlaying.svelte?raw'
import player from './Player.svelte?raw'

describe('player video surface toggle', () => {
  it('uses the compact video and its placeholder to expand and the wide video to collapse', () => {
    expect(nowPlaying).toContain('onvideotoggle={toggleVideoWide}')
    expect(nowPlaying).toContain('videoExpanded={videoWide}')
    expect(player).toMatch(/\{#if embedded\}[\s\S]*?\{#if onvideotoggle\}[\s\S]*?<button[\s\S]*?class="shield video-toggle"[\s\S]*?onclick=\{onvideotoggle\}/)
    expect(player).toContain("aria-label={videoExpanded ? 'Collapse video' : 'Expand video'}")
    expect(nowPlaying).toMatch(
      /\.mini-video-status \{[\s\S]*?pointer-events: none;/,
    )
  })

  it('uses a native button above the iframe for pointer and keyboard activation', () => {
    expect(player).toMatch(/<button[\s\S]*?type="button"[\s\S]*?class="shield video-toggle"/)
    expect(player).toContain('aria-expanded={videoExpanded}')
    expect(player).toMatch(/\.shield\.video-toggle:focus-visible \{[\s\S]*?outline:/)
  })
})

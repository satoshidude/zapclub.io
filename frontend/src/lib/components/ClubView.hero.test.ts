import { describe, expect, it } from 'vitest'
import clubView from './ClubView.svelte?raw'
import nowPlaying from './club/NowPlaying.svelte?raw'
import player from './club/Player.svelte?raw'

describe('club hero artwork', () => {
  it('keeps the individual generated fallback in the club hero', () => {
    expect(clubView).toContain("import { clubAvatar } from '../avatar'")
    expect(clubView).toContain('club?.picture || clubAvatar(owner || groupId)')
    expect(clubView).toContain('Image URL (leave empty for the generated one)')
  })

  it('uses the turntable artwork for the timed mini overlay and missing-video fallback', () => {
    expect(clubView).toContain("const PLAYER_MINI_ARTWORK = '/images/club-cover-turntable.webp'")
    expect(clubView).toContain('clubImage={PLAYER_MINI_ARTWORK}')
    expect(nowPlaying).toContain('const miniArtworkVisible = $derived(!videoWide && (miniVideoMaskVisible || audioOnly))')
    expect(nowPlaying).toContain('if (miniArtworkVisible) return clubImage')
    expect(nowPlaying).toContain('ledCover={miniArtworkVisible}')
    expect(player).toContain('class:led-cover={ledCover}')
    expect(player).toContain('.cover.led-cover::after')
  })
})

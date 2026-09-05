import { describe, expect, it } from 'vitest'
import { clubAvatar } from './avatar'
import source from './avatar.ts?raw'

describe('generated club avatars', () => {
  it('remain deterministic and distinct for different seeds', () => {
    expect(clubAvatar('club-a')).toBe(clubAvatar('club-a'))
    expect(clubAvatar('club-a')).not.toBe(clubAvatar('club-b'))
  })

  it('uses the turntable-derived violet/yellow palette plus Bitcoin-complementary accents', () => {
    expect(source).toContain("const BACKGROUND_COLORS = ['060405']")
    for (const color of [
      '752bf0',
      '973df9',
      'a454fe',
      'bd76fd',
      'f1d12d',
      'ffeb63',
      'f7931a',
      '1a8bf7',
      '4cff6a',
    ]) {
      expect(source).toContain(`'${color}'`)
    }
    expect(source).toContain('backgroundColor: BACKGROUND_COLORS')
    expect(source).toContain('ringColor: RING_COLORS')
  })
})

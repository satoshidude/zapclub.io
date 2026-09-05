import { describe, expect, it } from 'vitest'
import { clubAvatar } from './avatar'
import source from './avatar.ts?raw'

describe('generated club avatars', () => {
  it('remain deterministic and distinct for different seeds', () => {
    expect(clubAvatar('club-a')).toBe(clubAvatar('club-a'))
    expect(clubAvatar('club-a')).not.toBe(clubAvatar('club-b'))
  })

  it('uses an angular LED waveform and the turntable-derived violet/yellow palette', () => {
    for (const color of [
      '752bf0',
      '973df9',
      'a454fe',
      'bd76fd',
      'f4e04d',
      'ffeb63',
      'f7931a',
      '1a8bf7',
      '4cff6a',
    ]) {
      expect(source).toContain(`'${color}'`)
    }
    expect(source).toContain("const BACKGROUND_COLOR = '060405'")
    expect(source).toContain('function waveformSvg(seed: string)')
    expect(source).toContain('shape-rendering="crispEdges"')
    expect(source).toContain('preserveAspectRatio="xMidYMid slice"')
    expect(source).not.toContain('@dicebear')
  })
})

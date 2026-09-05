import { describe, expect, it } from 'vitest'
import leaderboard from './Leaderboard.svelte?raw'
import profileBadge from './ProfileBadge.svelte?raw'
import userProfile from './UserProfile.svelte?raw'

function styleBlock(source: string, selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))
  expect(match, `missing style block for ${selector}`).not.toBeNull()
  return match?.[1] ?? ''
}

function expectBorderless(source: string, selector: string): void {
  expect(styleBlock(source, selector)).toMatch(/\bborder:\s*0\s*;/)
}

describe('person avatars', () => {
  it('keeps leaderboard avatars free of decorative image borders', () => {
    expectBorderless(leaderboard, '.pod-av')
    expectBorderless(leaderboard, '.av')

    expect(leaderboard).not.toContain('.pod-first .pod-av { border-color:')
  })

  it('keeps profile and topbar avatars borderless without removing focus styling', () => {
    expectBorderless(userProfile, '.pavatar')
    expectBorderless(userProfile, '.senders .av')
    expectBorderless(userProfile, '.senders .anon-av')
    expectBorderless(profileBadge, '.avatar')

    expect(leaderboard).toContain('.pod-slot:focus-visible')
    expect(leaderboard).toContain('.row:focus-visible')
  })
})

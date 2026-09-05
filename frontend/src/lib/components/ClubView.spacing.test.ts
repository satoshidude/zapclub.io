import { describe, expect, it } from 'vitest'
import clubView from './ClubView.svelte?raw'

describe('club player spacing', () => {
  it('keeps a visible gap between the player and following cards', () => {
    expect(clubView).toMatch(/\.player-section \{[\s\S]*?margin-top: 0\.7rem;[\s\S]*?margin-bottom: 1rem;/)
    expect(clubView).toMatch(/@media \(max-width: 560px\)[\s\S]*?\.player-section \{[\s\S]*?margin-top: 0\.55rem;[\s\S]*?margin-bottom: 0\.75rem;/)
  })
})

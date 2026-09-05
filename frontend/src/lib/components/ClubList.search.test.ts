import { describe, expect, it } from 'vitest'
import source from './ClubList.svelte?raw'

describe('club search suggestions', () => {
  it('selects on pointerdown before Safari removes the blurred suggestion list', () => {
    expect(source).toMatch(
      /class="search-suggestion"[\s\S]*?onpointerdown=\{\(event\) => \{[\s\S]*?event\.preventDefault\(\)[\s\S]*?selectClubSuggestion\(club\.id\)/,
    )
    expect(source).toContain('onclick={() => selectClubSuggestion(club.id)}')
  })
})

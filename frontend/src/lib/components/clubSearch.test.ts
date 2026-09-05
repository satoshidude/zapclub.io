import { describe, expect, it } from 'vitest'
import { findClubSuggestions } from './clubSearch'
import type { Club } from '../nostr/types'

const club = (id: string, name: string): Club => ({
  id,
  name,
  open: true,
  isPublic: true,
})

describe('findClubSuggestions', () => {
  const clubs = [
    club('1', 'Chill house'),
    club('2', 'Def Dev'),
    club('3', 'House für alle'),
    club('4', 'Warehouse'),
  ]

  it('starts suggesting after the first non-space character', () => {
    expect(findClubSuggestions(clubs, '')).toEqual([])
    expect(findClubSuggestions(clubs, 'c').map((item) => item.id)).toEqual(['1'])
  })

  it('matches case and diacritics tolerantly', () => {
    expect(findClubSuggestions(clubs, 'FUR').map((item) => item.id)).toEqual(['3'])
  })

  it('ranks name starts before word starts and partial matches', () => {
    expect(findClubSuggestions(clubs, 'house').map((item) => item.id)).toEqual(['3', '1', '4'])
  })

  it('limits the result count', () => {
    expect(findClubSuggestions(clubs, 'e', 2)).toHaveLength(2)
  })
})

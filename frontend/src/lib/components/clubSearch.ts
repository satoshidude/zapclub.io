import type { Club } from '../nostr/types'

function searchable(value: string): string {
  return value
    .normalize('NFKD')
    .replace(/\p{Diacritic}/gu, '')
    .toLocaleLowerCase()
}

/** Ranked, case/diacritic-insensitive club-name suggestions. */
export function findClubSuggestions(clubs: Club[], query: string, limit = 6): Club[] {
  const needle = searchable(query.trim())
  if (!needle || limit <= 0) return []

  return clubs
    .map((club) => {
      const name = searchable(club.name)
      const index = name.indexOf(needle)
      const wordStart = index > 0 && /\s/.test(name[index - 1])
      const rank = index === 0 ? 0 : wordStart ? 1 : index >= 0 ? 2 : 3
      return { club, rank }
    })
    .filter(({ rank }) => rank < 3)
    .sort((a, b) => a.rank - b.rank || a.club.name.localeCompare(b.club.name))
    .slice(0, limit)
    .map(({ club }) => club)
}

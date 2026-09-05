import { describe, expect, it } from 'vitest'
import {
  COLLAPSED_MEMBER_ROWS,
  EXPANDED_MEMBER_ROWS,
  visibleMemberRows,
} from './memberRoster'

describe('chat member roster height', () => {
  it('shows at most six member rows before expansion', () => {
    expect(COLLAPSED_MEMBER_ROWS).toBe(6)
    expect(visibleMemberRows(9, false)).toBe(6)
  })

  it('grows with the chat up to ten member rows', () => {
    expect(EXPANDED_MEMBER_ROWS).toBe(10)
    expect(visibleMemberRows(9, true)).toBe(9)
    expect(visibleMemberRows(24, true)).toBe(10)
  })

  it('does not add empty rows for small clubs', () => {
    expect(visibleMemberRows(4, false)).toBe(4)
  })
})

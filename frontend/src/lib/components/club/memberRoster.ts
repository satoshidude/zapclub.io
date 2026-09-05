export const COLLAPSED_MEMBER_ROWS = 6
export const EXPANDED_MEMBER_ROWS = 10

export function visibleMemberRows(memberCount: number, expanded: boolean): number {
  const limit = expanded ? EXPANDED_MEMBER_ROWS : COLLAPSED_MEMBER_ROWS
  return Math.min(Math.max(0, memberCount), limit)
}

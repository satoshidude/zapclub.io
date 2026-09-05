import { describe, expect, it } from 'vitest'
import clubView from './ClubView.svelte?raw'
import groups from '../nostr/groups.ts?raw'

describe('club member count tag', () => {
  it('uses the public relay aggregate for guests without exposing the roster', () => {
    expect(groups).toContain('KIND_MEMBER_COUNT,')
    expect(groups).toContain('onMemberCount?: (ev: Event) => void')
    expect(groups).toContain('h.onMemberCount?.(ev)')
    expect(clubView).toContain('selectClubMemberCounts([ev], [id]).get(id)')
    expect(clubView).toContain('publicMemberCount ?? (isMember ? members.length : null)')
    expect(clubView).toContain('{#if memberTotal !== null}')
    expect(clubView).not.toMatch(/\{#if isMember\}\s*<span class="tag members-tag/)
  })
})

import { describe, expect, it } from 'vitest'
import source from './ComingNext.svelte?raw'
import dancefloor from './Dancefloor.svelte?raw'

describe('on-stage upcoming queue', () => {
  it('renders below the stage avatars and mirrors three conductor slots', () => {
    expect(dancefloor.indexOf('<ComingNext clubId={groupId} />')).toBeGreaterThan(
      dancefloor.indexOf('class="stagerow"'),
    )
    expect(source).toContain('upcomingTracks(clubId, 3)')
    expect(source).toContain('aria-label="Upcoming DJ queue"')
    expect(source).toContain("{displayName(item.dj, profile)}")
  })
})

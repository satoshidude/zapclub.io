import { describe, expect, it } from 'vitest'
import source from './ComingNext.svelte?raw'
import dancefloor from './Dancefloor.svelte?raw'
import clubView from '../ClubView.svelte?raw'

describe('on-stage upcoming queue', () => {
  it('renders below the stage avatars and mirrors three conductor slots', () => {
    expect(dancefloor.indexOf('<ComingNext clubId={groupId} />')).toBeGreaterThan(
      dancefloor.indexOf('class="stagerow"'),
    )
    expect(source).toContain('upcomingTracks(clubId, 3)')
    expect(source).toContain('aria-label="Upcoming DJ queue"')
    expect(source).toContain("{displayName(item.dj, profile)}")
  })

  it('keeps an armed Auto DJ visible as one occupied stage slot', () => {
    expect(dancefloor).toContain("import { autodj } from '../../nostr/autodj.svelte'")
    expect(dancefloor).toContain('const autoDJ = $derived(autodj.getConfig(groupId))')
    expect(dancefloor).toContain('const occupiedSlots = $derived(stageDjs.length + (autoDJ ? 1 : 0))')
    expect(dancefloor).toContain('const emptySlots = $derived(Math.max(0, MAX_DJS - occupiedSlots))')
    expect(dancefloor).toContain('occupiedSlots < MAX_DJS')
    expect(dancefloor).toContain('aria-label={`Auto DJ on stage — ${autoDJ.name}`}')
    expect(dancefloor).toContain('<span class="mq-inner">Auto DJ</span>')
    expect(clubView).toContain('autoPlaying={sync.live?.auto ?? false}')
  })
})

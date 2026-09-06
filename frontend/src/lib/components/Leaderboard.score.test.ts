import { describe, expect, it } from 'vitest'
import leaderboard from './Leaderboard.svelte?raw'
import profile from './UserProfile.svelte?raw'

describe('DJ leaderboard presentation', () => {
  it('uses the Top 10 product name consistently', () => {
    expect(leaderboard).toContain('TOP 10 LEADERBOARD')
    expect(leaderboard).not.toContain('TOP DJS Leaderboard')
  })

  it('shows votes and played tracks instead of a calculated DJ score', () => {
    expect(leaderboard).not.toContain('DJ SCORE')
    expect(leaderboard).toContain('e.bangers.toLocaleString()')
    expect(leaderboard).toContain('e.tracks.toLocaleString()')
    expect(leaderboard).toContain('entries = r.top.slice(0, 10)')
    expect(leaderboard).toContain('class="vote-count"')
  })

  it('shows relay-settled top tracks with their club, DJ and aggregate Bangers', () => {
    expect(leaderboard).toContain('TOP 10 TRACKS')
    expect(leaderboard).toContain('track.club')
    expect(leaderboard).toContain('track.dj')
    expect(leaderboard).toContain('track.bangers')
    expect(leaderboard).toContain('COMMUNITY SKIP')
    expect(leaderboard).toContain('goClub(track.club)')
    expect(leaderboard).toContain('goUser(npub)')
  })

  it('contains no payment-based ranking copy or fields', () => {
    expect(leaderboard).not.toContain('most-zapped')
    expect(leaderboard).not.toContain('sats received')
    expect(leaderboard).not.toContain('e.sats')
    expect(leaderboard).not.toContain('e.zappers')
  })

  it('uses the DJ rank on profiles while preserving private zap history', () => {
    expect(profile).toContain('fetchDJRank')
    expect(profile).toContain('type DJRank')
    expect(profile).toContain('djRank.bangers.toLocaleString()')
    expect(profile).not.toContain('DJ SCORE')
    expect(profile).not.toContain('fetchZapRank')
    expect(profile).not.toContain('type ZapRank')

    expect(profile).not.toContain('DJ Credibility')
    expect(profile).not.toContain('fetchCredibility')

    expect(profile).toContain('fetchReceivedZaps')
    expect(profile).toContain('Who zapped you')
  })
})

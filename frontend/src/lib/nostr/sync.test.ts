// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import type { Event } from 'nostr-tools/pure'
import { ingestNowPlaying, resetSync, shouldReportTrackError, upcomingTracks } from './sync.svelte'

function autoNowPlaying(next: Array<[string, string]>): Event {
  return {
    kind: 30100,
    created_at: 1,
    pubkey: 'relay',
    id: 'event',
    sig: 'sig',
    content: 'Current track',
    tags: [
      ['h', 'club'],
      ['d', 'club'],
      ['track', 'yt:CURRENT0001'],
      ['dj', 'owner'],
      ['pos', '7'],
      ['started_at', '1000'],
      ['sent_at', String(Date.now())],
      ['duration', '180'],
      ['status', 'playing'],
      ['auto', '1'],
      ...next.map(([videoId, title]) => ['next', `yt:${videoId}`, title]),
    ],
  } as Event
}

afterEach(() => resetSync())

describe('Auto DJ upcoming preview', () => {
  it('uses the relay-announced shuffle instead of the stored playlist order', () => {
    ingestNowPlaying(autoNowPlaying([
      ['SHUFFLED001', 'Shuffled first'],
      ['SHUFFLED002', 'Shuffled second'],
    ]), 'club')

    expect(upcomingTracks('club', 6)).toEqual([
      { dj: 'owner', videoId: 'SHUFFLED001', title: 'Shuffled first' },
      { dj: 'owner', videoId: 'SHUFFLED002', title: 'Shuffled second' },
    ])
  })

  it('ignores malformed relay preview entries', () => {
    ingestNowPlaying(autoNowPlaying([
      ['too-short', 'Invalid'],
      ['VALIDNEXT01', 'Valid'],
    ]), 'club')

    expect(upcomingTracks('club', 1)).toEqual([
      { dj: 'owner', videoId: 'VALIDNEXT01', title: 'Valid' },
    ])
  })
})

describe('broken-track reporting', () => {
  it('requires the current video, club membership and a signer', () => {
    expect(shouldReportTrackError('video', 'video', true, true)).toBe(true)
    expect(shouldReportTrackError('video', 'video', false, true)).toBe(false)
    expect(shouldReportTrackError('video', 'video', true, false)).toBe(false)
    expect(shouldReportTrackError('other', 'video', true, true)).toBe(false)
  })
})

// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import type { Event } from 'nostr-tools/pure'
import { ingestNowPlaying, resetSync, shouldReportTrackError, upcomingTracks } from './sync.svelte'
import { ingestAutoDJ, resetAutoDJ } from './autodj.svelte'
import { ingestQueue, resetQueues } from './queue.svelte'
import { ingestStage, resetStage } from './stage.svelte'

function event(kind: number, pubkey: string, tags: string[][], content = ''): Event {
  return {
    kind,
    created_at: Math.floor(Date.now() / 1000),
    pubkey,
    id: `${kind}:${pubkey}`,
    sig: 'sig',
    content,
    tags,
  } as Event
}

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

function armAutoDJ(): void {
  ingestAutoDJ(event(30105, 'owner', [
    ['h', 'club'],
    ['d', 'club'],
    ['status', 'armed'],
    ['track', 'yt:AUTONEXT001', 'Auto next', '180'],
  ], 'Auto DJ'))
}

function putOnStage(pubkey: string): void {
  const now = Math.floor(Date.now() / 1000)
  ingestStage(event(30102, pubkey, [['h', 'club'], ['since', String(now)]], 'on'))
}

function setQueue(pubkey: string): void {
  ingestQueue(event(30103, pubkey, [
    ['h', 'club'],
    ['d', 'club'],
    ['track', 'yt:REALCUR0001', 'Human current', '180'],
    ['track', 'yt:REALNEXT001', 'Human next one', '180'],
    ['track', 'yt:REALNEXT002', 'Human next two', '180'],
  ]))
}

function humanNowPlaying(): Event {
  return event(30100, 'relay', [
    ['h', 'club'],
    ['d', 'club'],
    ['track', 'yt:REALCUR0001'],
    ['dj', 'human'],
    ['pos', '8'],
    ['started_at', '1000'],
    ['sent_at', String(Date.now())],
    ['duration', '180'],
    ['status', 'playing'],
  ], 'Human current')
}

afterEach(() => {
  resetSync()
  resetAutoDJ()
  resetQueues()
  resetStage()
})

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

  it('keeps an armed Auto DJ out of the rotation while a real DJ is active', () => {
    armAutoDJ()
    putOnStage('human')
    setQueue('human')
    ingestNowPlaying(humanNowPlaying(), 'club')

    expect(upcomingTracks('club', 6)).toEqual([
      { dj: 'human', videoId: 'REALNEXT001', title: 'Human next one' },
      { dj: 'human', videoId: 'REALNEXT002', title: 'Human next two' },
    ])
  })

  it('previews the real-DJ handoff when a real DJ joins during an Auto-DJ track', () => {
    armAutoDJ()
    putOnStage('human')
    setQueue('human')
    ingestNowPlaying(autoNowPlaying([
      ['AUTONEXT001', 'Auto next'],
    ]), 'club')

    expect(upcomingTracks('club', 3)).toEqual([
      { dj: 'human', videoId: 'REALCUR0001', title: 'Human current' },
      { dj: 'human', videoId: 'REALNEXT001', title: 'Human next one' },
      { dj: 'human', videoId: 'REALNEXT002', title: 'Human next two' },
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

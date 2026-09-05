import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'svelte/server'
import type { Event } from 'nostr-tools/pure'
import VibeMeter from './VibeMeter.svelte'
import { setLoggedIn, setLoggedOut } from '../../nostr/auth.svelte'
import { ingestNowPlaying, resetSync } from '../../nostr/sync.svelte'
import { ingestStage, resetStage } from '../../nostr/stage.svelte'
import { resetMood } from '../../nostr/mood.svelte'
import vibeMeterSource from './VibeMeter.svelte?raw'

vi.hoisted(() => {
  Object.defineProperty(globalThis, 'location', {
    value: { pathname: '/' },
    configurable: true,
  })
})

const CLUB = 'club'
const ME = 'a'.repeat(64)
const OTHER = 'b'.repeat(64)
const VIDEO = 'dQw4w9WgXcQ'

function event(kind: number, pubkey: string, tags: string[][], content = ''): Event {
  return {
    kind,
    pubkey,
    created_at: Math.floor(Date.now() / 1000),
    tags,
    content,
    id: 'event',
    sig: 'signature',
  } as Event
}

function setTrack(dj: string, pos: number, auto = false): void {
  const now = Date.now()
  ingestNowPlaying(event(30100, 'relay', [
    ['h', CLUB],
    ['track', `yt:${VIDEO}`],
    ['dj', dj],
    ['pos', String(pos)],
    ['started_at', String(now)],
    ['sent_at', String(now)],
    ['duration', '300'],
    ['status', 'playing'],
    ...(auto ? [['auto', '1']] : []),
  ], 'Test track'), CLUB)
}

function putOnStage(pubkey: string): void {
  const now = Math.floor(Date.now() / 1000)
  ingestStage(event(30102, pubkey, [['h', CLUB], ['since', String(now)]], 'on'))
}

function renderMeter(dj: string, auto = false): string {
  setLoggedIn(ME, 'extension')
  if (!auto) putOnStage(dj)
  setTrack(dj, 1, auto)
  return render(VibeMeter, { props: { clubId: CLUB, isMember: true } }).body
}

function button(html: string, label: string): string {
  return [...html.matchAll(/<button\b[^>]*>/g)]
    .map(([tag]) => tag)
    .find((tag) => tag.includes(`aria-label="${label}"`)) ?? ''
}

afterEach(() => {
  resetMood()
  resetSync()
  resetStage()
  setLoggedOut()
})

describe('Vibemeter own-track voting', () => {
  for (const [label, auto] of [['real DJ', false], ['Auto DJ owner', true]] as const) {
    it(`disables both reactions for the signed-in ${label}`, () => {
      const html = renderMeter(ME, auto)
      const skip = button(html, 'Vote skip')
      const banger = button(html, 'Vote banger')

      expect(skip).toContain('disabled')
      expect(banger).toContain('disabled')
      expect(html).toContain('YOUR TRACK — NO VOTE')
      expect(skip).toContain(`aria-describedby="vibe-vote-state-${CLUB}"`)
      expect(html).toContain('aria-live="polite"')
    })
  }

  it('keeps reactions enabled for another DJ\'s track', () => {
    const html = renderMeter(OTHER)
    expect(button(html, 'Vote skip')).not.toContain('disabled')
    expect(button(html, 'Vote banger')).not.toContain('disabled')
    expect(html).toContain('RATE THE DJ')
  })

  it('updates eligibility when playback changes to the signed-in DJ', () => {
    renderMeter(OTHER)
    putOnStage(ME)
    setTrack(ME, 2)
    const html = render(VibeMeter, { props: { clubId: CLUB, isMember: true } }).body

    expect(button(html, 'Vote skip')).toContain('disabled')
    expect(button(html, 'Vote banger')).toContain('disabled')
    expect(html).toContain('YOUR TRACK — NO VOTE')
  })

  it('snapshots the target before awaiting the relay and animates only the same track', () => {
    expect(vibeMeterSource).toMatch(/const votePos = pos[\s\S]*?await sendMood\(voteClub, votePos, v\)[\s\S]*?optimisticVote\(voteClub, votePos, voter, v\)/)
    expect(vibeMeterSource).toContain("v === 'banger' && clubId === voteClub && sync.live?.pos === votePos")
  })
})

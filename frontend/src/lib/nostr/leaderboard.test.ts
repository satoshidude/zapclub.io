import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchDJRank, fetchLeaderboard, fetchReceivedZaps, recordZap } from './leaderboard'
import type { Event } from 'nostr-tools/pure'

vi.mock('./nip98', () => ({
  nip98Header: vi.fn(async (url: string, method: string) => `Nostr ${method} ${url}`),
}))

const alice = 'a'.repeat(64)
const bob = 'b'.repeat(64)
const carol = 'c'.repeat(64)

describe('DJ leaderboard API', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('accepts the DJ performance schema and drops legacy zap or malformed entries', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      total: 3,
      top: [
        { pubkey: alice, rank: 1, score: 333, tracks: 20, bangers: 40, skipped: 0, vibeScore: 40 },
        { pubkey: bob, rank: 2, sats: 12_000, zaps: 8, zappers: 5 },
        { pubkey: carol, rank: 3, score: 14.5, tracks: 1, bangers: 0, skipped: 0, vibeScore: 0 },
      ],
      topTracks: [
        { rank: 1, club: 'club-one', videoId: 'abcdefghijk', title: 'The track', dj: bob, bangers: 5, skipped: false, startedAt: 1_757_000_000_000 },
        { rank: 2, club: '', videoId: 'bad', title: 'Malformed', dj: carol, bangers: 8, skipped: false, startedAt: 1 },
      ],
    })))

    await expect(fetchLeaderboard()).resolves.toEqual({
      total: 3,
      top: [
        { pubkey: alice, rank: 1, score: 333, tracks: 20, bangers: 40, skipped: 0, vibeScore: 40 },
      ],
      topTracks: [
        { rank: 1, club: 'club-one', videoId: 'abcdefghijk', title: 'The track', dj: bob, bangers: 5, skipped: false, startedAt: 1_757_000_000_000 },
      ],
    })
  })

  it('keeps older relay responses compatible before track performances exist', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      total: 1,
      top: [{ pubkey: alice, rank: 1, score: 91, tracks: 1, bangers: 5, skipped: 0, vibeScore: 5 }],
    })))

    // Use a distinct module instance so the public one-minute cache from the
    // previous test cannot mask this rolling-deploy compatibility check.
    vi.resetModules()
    const { fetchLeaderboard: freshFetchLeaderboard } = await import('./leaderboard')
    await expect(freshFetchLeaderboard()).resolves.toEqual({
      total: 1,
      top: [{ pubkey: alice, rank: 1, score: 91, tracks: 1, bangers: 5, skipped: 0, vibeScore: 5 }],
      topTracks: [],
    })
  })

  it('loads a validated DJ rank without converting score tenths in the data layer', async () => {
    const payload = {
      ranked: true,
      total: 17,
      pubkey: alice,
      rank: 4,
      score: 257,
      tracks: 18,
      bangers: 31,
      skipped: 2,
      vibeScore: 29,
    }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify(payload)))

    await expect(fetchDJRank(alice)).resolves.toEqual({
      total: 17,
      pubkey: alice,
      rank: 4,
      score: 257,
      tracks: 18,
      bangers: 31,
      skipped: 2,
      vibeScore: 29,
    })
  })

  it('rejects an old zap-rank response instead of exposing undefined metrics', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      ranked: true,
      total: 17,
      pubkey: alice,
      rank: 4,
      sats: 12_000,
      zaps: 8,
      zappers: 5,
    })))

    await expect(fetchDJRank(alice)).resolves.toBeNull()
  })

  it('rejects a rank response for a different pubkey', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      ranked: true,
      total: 17,
      pubkey: bob,
      rank: 4,
      score: 257,
      tracks: 18,
      bangers: 31,
      skipped: 2,
      vibeScore: 29,
    })))

    await expect(fetchDJRank(alice)).resolves.toBeNull()
  })
})

describe('Zapclub zap API', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('records the explicit recipient instead of deriving it from a wallet receipt', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}'))
    const request = { kind: 9734, pubkey: 'sender', tags: [['p', 'recipient-pubkey']] } as Event
    await recordZap(request, 'lnbc_invoice')

    expect(fetchMock).toHaveBeenCalledWith(
      'https://relay.zapclub.io/zaps',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ request, invoice: 'lnbc_invoice' }),
      }),
    )
  })

  it('loads the signed-in user’s Zapclub-only sender history', async () => {
    const result = { total: 210, count: 1, bySender: [{ sender: 'sender', sats: 210, count: 1, exact: true }] }
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify(result)))

    await expect(fetchReceivedZaps()).resolves.toEqual(result)
    expect(fetchMock).toHaveBeenCalledWith(
      'https://relay.zapclub.io/zaps/received',
      expect.objectContaining({ headers: { Authorization: 'Nostr GET https://relay.zapclub.io/zaps/received' } }),
    )
  })
})

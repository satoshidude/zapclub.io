import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchReceivedZaps, recordZap } from './leaderboard'
import type { Event } from 'nostr-tools/pure'

vi.mock('./nip98', () => ({
  nip98Header: vi.fn(async (url: string, method: string) => `Nostr ${method} ${url}`),
}))

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

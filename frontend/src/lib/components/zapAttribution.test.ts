import { describe, expect, it } from 'vitest'
import clubView from './ClubView.svelte?raw'
import userProfile from './UserProfile.svelte?raw'
import payModal from '../nostr/payModal.svelte.ts?raw'
import zapsSource from '../nostr/zaps.svelte.ts?raw'

describe('Zapclub-only zap attribution', () => {
  it('does not seed a club score from global NIP-57 receipt subscriptions', () => {
    expect(clubView).not.toContain('subscribeZaps')
    expect(clubView).toContain('onZapBroadcast: ingestZapBroadcast')
  })

  it('records direct profile zaps for the displayed profile pubkey', () => {
    expect(userProfile).toContain('dj: pubkey, zapRequest: request')
    expect(payModal).toContain('recordZap(state.zapRequest, state.invoice)')
    expect(zapsSource).toContain("['client', 'zapclub.io']")
  })
})

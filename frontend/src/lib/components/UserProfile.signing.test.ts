import { describe, expect, it } from 'vitest'
import source from './UserProfile.svelte?raw'

describe('private profile signing', () => {
  it('loads NIP-98 zap history only after an explicit click', () => {
    expect(source).not.toMatch(/if \(isMe && auth\.canSign\).*fetchReceivedZaps/)
    expect(source).toContain('onclick={loadReceivedHistory}')
    expect(source).toContain('requires one signer confirmation')
  })
})

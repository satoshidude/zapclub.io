// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { lnurlPayEndpoint } from './zaps.svelte'

describe('lnurlPayEndpoint', () => {
  it('routes nsnip.io discovery through the CORS-enabled zapclub proxy', () => {
    expect(lnurlPayEndpoint('zapclub@nsnip.io')).toBe(
      'https://zapclub.io/.well-known/lnurlp/zapclub',
    )
  })

  it('keeps LNURL discovery on other Lightning domains', () => {
    expect(lnurlPayEndpoint('alice@example.com')).toBe(
      'https://example.com/.well-known/lnurlp/alice',
    )
  })

  it('rejects malformed Lightning addresses', () => {
    expect(() => lnurlPayEndpoint('not-an-address')).toThrow('Invalid lightning address')
    expect(() => lnurlPayEndpoint('alice@')).toThrow('Invalid lightning address')
  })
})

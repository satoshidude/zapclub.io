// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { verifyEvent } from 'nostr-tools/pure'

const mocked = vi.hoisted(() => ({
  auth: { pubkey: 'a'.repeat(64) as string | null },
}))

vi.mock('./auth.svelte', () => ({ auth: mocked.auth }))

describe('page-session signer', () => {
  beforeEach(async () => {
    mocked.auth.pubkey = 'a'.repeat(64)
    const { resetSessionSigner } = await import('./sessionSigner')
    resetSessionSigner()
  })

  it('signs a valid event while exposing only the authenticated principal in tags', async () => {
    const { SESSION_EVENT_MARKER, signSessionEvent } = await import('./sessionSigner')
    const signed = signSessionEvent({
      kind: 20100,
      created_at: 100,
      tags: [['h', 'club']],
      content: '',
    })

    expect(verifyEvent(signed)).toBe(true)
    expect(signed.pubkey).not.toBe(mocked.auth.pubkey)
    expect(signed.tags).toContainEqual(['p', mocked.auth.pubkey])
    expect(signed.tags).toContainEqual(['client', SESSION_EVENT_MARKER])
  })

  it('keeps timestamps monotonic for rapid stage transitions', async () => {
    const { signSessionEvent } = await import('./sessionSigner')
    const first = signSessionEvent({ kind: 30102, created_at: 100, tags: [['h', 'club']], content: 'on' })
    const second = signSessionEvent({ kind: 30102, created_at: 100, tags: [['h', 'club']], content: 'off' })
    expect(second.created_at).toBe(first.created_at + 1)
  })

  it('never ratchets rapid session events beyond the relay future window', async () => {
    const { SESSION_EVENT_MAX_FUTURE_SECONDS, signSessionEvent } = await import('./sessionSigner')
    const wallNow = Math.floor(Date.now() / 1000)
    for (let offset = 0; offset <= SESSION_EVENT_MAX_FUTURE_SECONDS; offset++) {
      const event = signSessionEvent({ kind: 20100, created_at: wallNow, tags: [['h', 'club']], content: '' })
      expect(event.created_at).toBe(wallNow + offset)
    }
    expect(() => signSessionEvent({ kind: 20100, created_at: wallNow, tags: [['h', 'club']], content: '' }))
      .toThrow('Too many session events')
  })

  it('maps only explicitly marked received events to their principal', async () => {
    const { SESSION_EVENT_MARKER, sessionEventPrincipal } = await import('./sessionSigner')
    const sessionPubkey = 'b'.repeat(64)
    expect(sessionEventPrincipal({
      kind: 20100,
      pubkey: sessionPubkey,
      tags: [['p', mocked.auth.pubkey!], ['client', SESSION_EVENT_MARKER]],
    })).toBe(mocked.auth.pubkey)
    expect(sessionEventPrincipal({ kind: 20100, pubkey: sessionPubkey, tags: [['p', mocked.auth.pubkey!]] })).toBe(sessionPubkey)
  })

  it('rejects ambiguous, malformed, and out-of-scope principal tags', async () => {
    const { SESSION_EVENT_MARKER, sessionEventPrincipal } = await import('./sessionSigner')
    const sessionPubkey = 'b'.repeat(64)
    const principal = mocked.auth.pubkey!
    const resolve = (kind: number, tags: string[][]) => sessionEventPrincipal({ kind, pubkey: sessionPubkey, tags })

    expect(resolve(20100, [['p', principal], ['p', principal], ['client', SESSION_EVENT_MARKER]])).toBe(sessionPubkey)
    expect(resolve(30102, [['p', principal], ['client', SESSION_EVENT_MARKER], ['client', SESSION_EVENT_MARKER]])).toBe(sessionPubkey)
    expect(resolve(20100, [['p', 'not-hex'], ['client', SESSION_EVENT_MARKER]])).toBe(sessionPubkey)
    expect(resolve(20100, [['p', principal, 'extra'], ['client', SESSION_EVENT_MARKER]])).toBe(sessionPubkey)
    expect(resolve(20100, [['p', principal], ['client', SESSION_EVENT_MARKER, 'extra']])).toBe(sessionPubkey)
    expect(resolve(9, [['p', principal], ['client', SESSION_EVENT_MARKER]])).toBe(sessionPubkey)
  })
})

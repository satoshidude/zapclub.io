import { describe, expect, it } from 'vitest'
import clubView from './ClubView.svelte?raw'

describe('club Nostr share link', () => {
  it('offers exactly one external sharing target', () => {
    expect(clubView).toContain('Share on Nostr')
    expect(clubView).toContain('href={nostrShareUrl}')
    expect(clubView).toContain('target="_blank"')
    expect(clubView).toContain('rel="noopener noreferrer"')
    expect(clubView).not.toContain('Share on Telegram')
    expect(clubView).not.toContain('Share on X')
    expect(clubView).not.toContain('Share on Facebook')
    expect(clubView).not.toContain('Copy link')
    expect(clubView).not.toContain('Share on WhatsApp')
    expect(clubView).not.toContain('navigator.share')
    expect(clubView).not.toContain('role="menu"')
  })

  it('prefills Nostter with the public club URL without requiring Zapclub login', () => {
    expect(clubView).toContain('https://zapclub.io/club/${groupId}')
    expect(clubView).toContain('https://nostter.app/post?content=${encodeURIComponent')
    expect(clubView).not.toContain('if (auth.canSign) askNostrShare()')
    expect(clubView).not.toContain('shareNote(')
  })
})

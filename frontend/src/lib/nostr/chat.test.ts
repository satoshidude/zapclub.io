// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import type { Event } from 'nostr-tools/pure'
import { CHAT_MAX_LENGTH, KIND_CHAT, mergeChatMessages, normalizeChatContent } from './chat'

function event(id: string, createdAt: number, kind = KIND_CHAT): Event {
  return {
    id,
    kind,
    created_at: createdAt,
    pubkey: id.padEnd(64, '0'),
    sig: '0'.repeat(128),
    tags: [['h', 'club']],
    content: id,
  } as Event
}

describe('chat messages', () => {
  it('trims content and applies the public message limit', () => {
    expect(normalizeChatContent('  hello  ')).toBe('hello')
    expect(normalizeChatContent('x'.repeat(CHAT_MAX_LENGTH + 20))).toHaveLength(CHAT_MAX_LENGTH)
  })

  it('deduplicates and sorts history chronologically', () => {
    const newest = event('b', 20)
    const oldest = event('a', 10)
    const merged = mergeChatMessages([newest], oldest)
    expect(merged.map((item) => item.id)).toEqual(['a', 'b'])
    expect(mergeChatMessages(merged, oldest)).toBe(merged)
  })

  it('ignores non-chat events and keeps the newest bounded history', () => {
    const current = [event('a', 1), event('b', 2)]
    expect(mergeChatMessages(current, event('x', 3, 1))).toBe(current)
    expect(mergeChatMessages(current, event('c', 3), 2).map((item) => item.id)).toEqual(['b', 'c'])
  })
})

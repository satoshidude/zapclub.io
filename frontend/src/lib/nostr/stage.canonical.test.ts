// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import type { Event } from 'nostr-tools/pure'
import { ingestStage, resetStage, stage } from './stage.svelte'

const DJ = 'a'.repeat(64)

function stageEvent(id: string, content: 'on' | 'off'): Event {
  const createdAt = Math.floor(Date.now() / 1000)
  return {
    kind: 30102,
    created_at: createdAt,
    pubkey: DJ,
    id,
    sig: 'sig',
    content,
    tags: [['h', 'club'], ['since', String(createdAt)]],
  } as Event
}

afterEach(() => resetStage())

describe('canonical stage replacement', () => {
  it('keeps the lower event id at equal created_at in either arrival order', () => {
    ingestStage(stageEvent('f'.repeat(64), 'on'))
    ingestStage(stageEvent('0'.repeat(64), 'off'))
    expect(stage.isOnStage(DJ)).toBe(false)

    resetStage()
    ingestStage(stageEvent('0'.repeat(64), 'off'))
    ingestStage(stageEvent('f'.repeat(64), 'on'))
    expect(stage.isOnStage(DJ)).toBe(false)
  })
})

import { describe, expect, it } from 'vitest'
import {
  AUTO_VIDEO_PREVIEW_END,
  AUTO_VIDEO_PREVIEW_START,
  nextAutoVideoPreviewPhase,
} from './autoVideoPreview'

describe('automatic video preview', () => {
  it('stays closed before 20 seconds', () => {
    expect(nextAutoVideoPreviewPhase('waiting', AUTO_VIDEO_PREVIEW_START - 0.01)).toBe('waiting')
  })

  it('opens at 20 seconds and remains open for eight seconds', () => {
    expect(nextAutoVideoPreviewPhase('waiting', AUTO_VIDEO_PREVIEW_START)).toBe('open')
    expect(nextAutoVideoPreviewPhase('open', AUTO_VIDEO_PREVIEW_END - 0.01)).toBe('open')
  })

  it('closes at 28 seconds and does not reopen during the track', () => {
    expect(nextAutoVideoPreviewPhase('open', AUTO_VIDEO_PREVIEW_END)).toBe('done')
    expect(nextAutoVideoPreviewPhase('done', AUTO_VIDEO_PREVIEW_START)).toBe('done')
  })

  it('does not open for a listener joining after the preview window', () => {
    expect(nextAutoVideoPreviewPhase('waiting', AUTO_VIDEO_PREVIEW_END + 30)).toBe('done')
  })
})

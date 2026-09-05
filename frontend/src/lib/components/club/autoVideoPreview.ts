export const AUTO_VIDEO_PREVIEW_START = 20
export const AUTO_VIDEO_PREVIEW_END = AUTO_VIDEO_PREVIEW_START + 8

export type AutoVideoPreviewPhase = 'waiting' | 'open' | 'done'

export function nextAutoVideoPreviewPhase(
  phase: AutoVideoPreviewPhase,
  position: number,
): AutoVideoPreviewPhase {
  if (phase === 'done' || position >= AUTO_VIDEO_PREVIEW_END) return 'done'
  if (phase === 'open' || position >= AUTO_VIDEO_PREVIEW_START) return 'open'
  return 'waiting'
}

/**
 * Svelte action: scrolls overflowing text on hover (desktop) or automatically (touch).
 * Usage: add use:marquee to the clipping container, wrap text in <span class="mq-inner">.
 * Sets --mq-shift and data-mq on the container so CSS handles the animation.
 */
export function marquee(node: HTMLElement) {
  function update() {
    const inner = node.querySelector<HTMLElement>('.mq-inner')
    if (!inner) return
    const overflow = inner.scrollWidth - node.clientWidth
    if (overflow > 2) {
      node.style.setProperty('--mq-shift', `-${overflow}px`)
      node.setAttribute('data-mq', 'true')
    } else {
      node.style.removeProperty('--mq-shift')
      node.removeAttribute('data-mq')
    }
  }

  const ro = new ResizeObserver(update)
  ro.observe(node)
  update()
  return { destroy() { ro.disconnect() } }
}

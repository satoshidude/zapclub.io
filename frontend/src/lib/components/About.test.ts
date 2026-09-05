import { describe, expect, it } from 'vitest'
import source from './About.svelte?raw'

describe('project about page', () => {
  it('explains the product, technology and vision', () => {
    expect(source).toContain('<h1 class="site-h1">One room. One clock. Everyone in sync.</h1>')
    expect(source).toContain('<h2>What it does</h2>')
    expect(source).toContain('<h2>How the signal moves</h2>')
    expect(source).toContain('vision://open-dancefloor')
    expect(source).not.toContain('<h2>Credits &amp; source</h2>')
    expect(source).not.toContain('<h2>Privacy</h2>')
  })
})

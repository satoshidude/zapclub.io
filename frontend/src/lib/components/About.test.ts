import { describe, expect, it } from 'vitest'
import source from './About.svelte?raw'

describe('project about page', () => {
  it('explains the product, technology, vision and credits', () => {
    expect(source).toContain('<h1>One room. One clock. Everyone in sync.</h1>')
    expect(source).toContain('<h2>What it does</h2>')
    expect(source).toContain('<h2>How the signal moves</h2>')
    expect(source).toContain('vision://open-dancefloor')
    expect(source).toContain('<h2>Credits &amp; source</h2>')
    expect(source).toContain('https://github.com/satoshidude/zapclub.io')
    expect(source).not.toContain('<h2>Privacy</h2>')
  })
})

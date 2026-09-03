import { describe, expect, it } from 'vitest'
import source from './About.svelte?raw'

describe('consolidated project information', () => {
  it('contains the former footer destinations in one page', () => {
    expect(source).toContain('<h1>About ZapClub</h1>')
    expect(source).toContain('<h2>Credits &amp; source</h2>')
    expect(source).toContain('<h2>Privacy</h2>')
    expect(source).toContain('<h2>Terms &amp; disclaimer</h2>')
    expect(source).toContain('https://github.com/satoshidude/zapclub.io')
  })
})

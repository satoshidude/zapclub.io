import { describe, expect, it } from 'vitest'
import source from './Disclaimer.svelte?raw'

describe('combined legal information', () => {
  it('contains privacy, terms and disclaimer in one page', () => {
    expect(source).toContain('<h1 class="site-h1">Privacy, terms &amp; disclaimer</h1>')
    expect(source).toContain('<h2>Privacy</h2>')
    expect(source).toContain('<h2>Terms of use</h2>')
    expect(source).toContain('<h2>Disclaimer</h2>')
    expect(source).toContain('Zapclub should never ask for your private key')
    expect(source).toContain('temporary, club-specific browser-tab key')
    expect(source).toContain('Lightning payments are real and normally irreversible')
  })
})

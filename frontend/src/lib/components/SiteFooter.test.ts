import { describe, expect, it } from 'vitest'
import source from './SiteFooter.svelte?raw'

describe('project footer contract', () => {
  it('implements neutral contract 1.2.0 without attribution', () => {
    expect(source).toContain('data-project-footer="1.2.0"')
    expect(source).toContain('data-project-version={build.version}')
    expect(source).toContain('data-build-commit={build.commit}')
    expect(source).not.toMatch(/sunnyhill\.io|site-footer__attribution|made by/i)
  })

  it('has one consolidated information link instead of a footer menu', () => {
    expect(source).not.toContain('<nav')
    expect(source.match(/<a\b/g)).toHaveLength(2)
    expect(source).toContain('>About &amp; legal</a>')
    expect(source).not.toMatch(/>Credits<|>Disclaimer<|>GitHub/)
  })

  it('gives the information link a minimum 44px touch target', () => {
    expect(source).toMatch(/small > a:first-child\s*\{[\s\S]*?min-height:\s*44px;[\s\S]*?display:\s*inline-flex;[\s\S]*?align-items:\s*center;/)
  })

  it('ends its metadata after version and revision', () => {
    expect(source).toMatch(/v\{build\.version\}[\s\S]*\{revision\}[\s\S]*<\/small>/)
    expect(source).not.toMatch(/attribution/i)
    expect(source).toMatch(/a:hover,[\s\S]*?font-weight:\s*800;/)
    expect(source).not.toMatch(/text-decoration-color/)
  })
})

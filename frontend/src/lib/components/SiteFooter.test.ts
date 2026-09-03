import { describe, expect, it } from 'vitest'
import source from './SiteFooter.svelte?raw'
import packageSource from '../../../package.json?raw'

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
    expect(source).toContain('class="info"')
    expect(source).toContain('>About &amp; legal</a>')
    expect(source).not.toMatch(/>Credits<|>Disclaimer<|>GitHub/)
  })

  it('gives the information link a minimum 44px touch target', () => {
    expect(source).toMatch(/\.info\s*\{[\s\S]*?min-height:\s*44px;[\s\S]*?display:\s*inline-flex;[\s\S]*?align-items:\s*center;/)
  })

  it('aligns information left and build metadata right like nsnip.io', () => {
    expect(source).toMatch(/display:\s*grid;/)
    expect(source).toMatch(/grid-template-columns:\s*minmax\(0, 1fr\) auto;/)
    expect(source).toMatch(/\.info\s*\{[\s\S]*?justify-self:\s*start;/)
    expect(source).toMatch(/small\s*\{[\s\S]*?justify-self:\s*end;/)
    expect(source).toMatch(/@media \(max-width: 560px\)[\s\S]*?grid-template-columns:\s*1fr;[\s\S]*?small\s*\{[\s\S]*?justify-self:\s*start;/)
  })

  it('shows release version 0.1 and ends its metadata after the revision', () => {
    expect(JSON.parse(packageSource).version).toBe('0.1.0')
    expect(source).toContain("const displayVersion = build.version.endsWith('.0') ? build.version.slice(0, -2) : build.version")
    expect(source).toMatch(/v\{displayVersion\}[\s\S]*\{revision\}[\s\S]*<\/small>/)
    expect(source).not.toMatch(/attribution/i)
    expect(source).toMatch(/a:hover,[\s\S]*?font-weight:\s*800;/)
    expect(source).not.toMatch(/text-decoration-color/)
  })
})

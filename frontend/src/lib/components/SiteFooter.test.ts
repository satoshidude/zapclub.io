import { describe, expect, it } from 'vitest'
import source from './SiteFooter.svelte?raw'

describe('project footer contract', () => {
  it('implements Sunnyhill contract 1.1.0 with unlinked attribution', () => {
    expect(source).toContain('data-sunnyhill-footer="1.1.0"')
    expect(source).toContain('data-project-version={build.version}')
    expect(source).toContain('data-build-commit={build.commit}')
    expect(source).toContain('site-footer__attribution">made by sunnyhill.io')
    expect(source).not.toMatch(/href=[^>]+sunnyhill\.io/i)
  })

  it('keeps the shared link order', () => {
    const labels = [...source.matchAll(/>(About|Credits|Disclaimer|GitHub ↗)<\/a>/g)].map(([, label]) => label)
    expect(labels).toEqual(['About', 'Credits', 'Disclaimer', 'GitHub ↗'])
  })

  it('gives every navigation link a minimum 44px touch target', () => {
    expect(source).toMatch(/nav a\s*\{[\s\S]*?min-width:\s*44px;[\s\S]*?min-height:\s*44px;[\s\S]*?display:\s*inline-flex;[\s\S]*?align-items:\s*center;/)
  })

  it('ends its metadata with version, revision and one-line attribution', () => {
    expect(source).toMatch(/v\{build\.version\}[\s\S]*\{revision\}[\s\S]*made by sunnyhill\.io[\s\S]*<\/small>/)
    expect(source).toMatch(/\.site-footer__attribution\s*\{[\s\S]*?white-space:\s*nowrap;/)
    expect(source).toMatch(/a:hover,[\s\S]*?font-weight:\s*800;/)
    expect(source).not.toMatch(/text-decoration-color/)
  })
})

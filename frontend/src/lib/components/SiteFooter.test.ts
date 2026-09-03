import { describe, expect, it } from 'vitest'
import source from './SiteFooter.svelte?raw'

describe('project footer contract', () => {
  it('implements neutral contract 1.2.0 without attribution', () => {
    expect(source).toContain('data-project-footer="1.2.0"')
    expect(source).toContain('data-project-version={build.version}')
    expect(source).toContain('data-build-commit={build.commit}')
    expect(source).not.toMatch(/sunnyhill\.io|site-footer__attribution|made by/i)
  })

  it('keeps the shared link order', () => {
    const labels = [...source.matchAll(/>(About|Credits|Disclaimer|GitHub ↗)<\/a>/g)].map(([, label]) => label)
    expect(labels).toEqual(['About', 'Credits', 'Disclaimer', 'GitHub ↗'])
  })

  it('ends its metadata after version and revision', () => {
    expect(source).toMatch(/v\{build\.version\}[\s\S]*\{revision\}[\s\S]*<\/small>/)
    expect(source).not.toMatch(/attribution/i)
  })
})

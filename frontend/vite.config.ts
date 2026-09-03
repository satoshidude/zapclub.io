import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import type { Plugin } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

const packageMetadata = JSON.parse(
  readFileSync(new URL('./package.json', import.meta.url), 'utf8'),
) as { version: string }
const commitPattern = /^[0-9a-f]{40}$/

function sourceCommit(command: string): string {
  const supplied = process.env.SOURCE_COMMIT
  if (supplied) {
    if (!commitPattern.test(supplied)) throw new Error('SOURCE_COMMIT must be a full lowercase Git commit.')
    return supplied
  }

  try {
    const commit = execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim()
    if (commitPattern.test(commit)) return commit
  } catch {
    // Development can run from an exported source tree without Git metadata.
  }

  if (command === 'build') throw new Error('A production build requires SOURCE_COMMIT or a Git checkout.')
  return 'development'
}

function releaseManifest(buildInfo: { commit: string; version: string }): Plugin {
  return {
    name: 'zapclub-release-manifest',
    generateBundle() {
      this.emitFile({
        type: 'asset',
        fileName: 'release.json',
        source: `${JSON.stringify({ project: 'zapclub.io', footerContract: '1.2.0', ...buildInfo }, null, 2)}\n`,
      })
    },
  }
}

export default defineConfig(({ command }) => {
  const buildInfo = { commit: sourceCommit(command), version: packageMetadata.version }
  return {
    define: { __ZAPCLUB_BUILD_INFO__: JSON.stringify(buildInfo) },
    plugins: [svelte(), releaseManifest(buildInfo)],
  }
})

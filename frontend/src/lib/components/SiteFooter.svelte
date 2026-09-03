<script lang="ts">
  import { navigate } from '../router.svelte'

  const repository = 'https://github.com/satoshidude/zapclub.io'
  const build = __ZAPCLUB_BUILD_INFO__
  const revision = build.commit === 'development' ? build.commit : build.commit.slice(0, 7)

  function follow(event: MouseEvent, path: string): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    event.preventDefault()
    navigate(path)
  }
</script>

<footer
  class="site-footer"
  data-sunnyhill-footer="1.1.0"
  data-project-version={build.version}
  data-build-commit={build.commit}
>
  <nav aria-label="Footer navigation">
    <a href="/about" onclick={(event) => follow(event, '/about')}>About</a>
    <a href="/credits" onclick={(event) => follow(event, '/credits')}>Credits</a>
    <a href="/disclaimer" onclick={(event) => follow(event, '/disclaimer')}>Disclaimer</a>
    <a href={repository} target="_blank" rel="noopener noreferrer">GitHub ↗</a>
  </nav>
  <small>
    v{build.version} <span aria-hidden="true">/</span>
    {#if build.commit === 'development'}
      {revision}
    {:else}
      <a class="revision" href={`${repository}/commit/${build.commit}`} target="_blank" rel="noopener noreferrer">{revision}</a>
    {/if}
    <span aria-hidden="true">/</span>
    <span class="site-footer__attribution">made by sunnyhill.io</span>
  </small>
</footer>

<style>
  .site-footer {
    min-height: 64px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1.5rem;
    padding: 1.1rem max(1rem, calc((100vw - 960px) / 2));
    border-top: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 0.72rem;
    font-weight: 600;
  }

  nav {
    display: flex;
    align-items: center;
    gap: clamp(0.85rem, 3vw, 1.5rem);
    min-width: 0;
  }

  nav a {
    min-width: 44px;
    min-height: 44px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }

  a {
    color: inherit;
    text-decoration: none;
    transition: color 0.15s ease;
  }

  a:hover,
  a:focus-visible {
    color: var(--accent);
    font-weight: 800;
  }

  a:focus-visible {
    border-radius: 3px;
    outline: 2px solid var(--accent-2);
    outline-offset: 4px;
  }

  small {
    flex: 0 0 auto;
    font: inherit;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  small span {
    margin-inline: 0.3rem;
    color: var(--border);
  }

  .revision {
    text-decoration: none;
  }

  .site-footer__attribution {
    margin-inline: 0;
    color: inherit;
    white-space: nowrap;
  }

  @media (max-width: 560px) {
    .site-footer {
      min-height: 0;
      align-items: flex-start;
      flex-direction: column;
      gap: 0.55rem;
      padding-block: 0.85rem calc(0.95rem + env(safe-area-inset-bottom));
    }

    nav {
      width: 100%;
      justify-content: space-between;
      gap: 0.65rem;
    }
  }
</style>

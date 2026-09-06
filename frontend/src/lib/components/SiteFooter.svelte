<script lang="ts">
  import { navigate } from '../router.svelte'

  const repository = 'https://github.com/satoshidude/zapclub.io'
  const build = __ZAPCLUB_BUILD_INFO__
  const displayVersion = build.version.endsWith('.0') ? build.version.slice(0, -2) : build.version
  const revision = build.commit === 'development' ? build.commit : build.commit.slice(0, 7)

  function follow(event: MouseEvent, path: string): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    event.preventDefault()
    navigate(path)
  }
</script>

<footer
  class="site-footer"
  data-project-footer="1.2.0"
  data-project-version={build.version}
  data-build-commit={build.commit}
>
  <nav class="footer-nav" aria-label="Project information">
    <a href="/leaderboard" onclick={(event) => follow(event, '/leaderboard')}>TOP 10</a>
    <a href="/about" onclick={(event) => follow(event, '/about')}>About</a>
    <a href="/disclaimer" onclick={(event) => follow(event, '/disclaimer')}>Disclaimer</a>
    <a href={repository} target="_blank" rel="noopener noreferrer">GitHub</a>
  </nav>
  <small>
    v.{displayVersion} <span aria-hidden="true">/</span>
    {#if build.commit === 'development'}
      {revision}
    {:else}
      <a class="revision" href={`${repository}/commit/${build.commit}`} target="_blank" rel="noopener noreferrer">{revision}</a>
    {/if}
  </small>
</footer>

<style>
  .site-footer {
    min-height: 64px;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.5rem 1.5rem;
    padding: 2.5rem max(1rem, calc((100vw - 960px) / 2));
    border-top: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 12px;
    font-weight: 400;
  }

  .footer-nav {
    min-height: 44px;
    display: flex;
    align-items: center;
    gap: 0.35rem 1.25rem;
    flex-wrap: wrap;
    justify-self: start;
  }

  .footer-nav a {
    min-height: 44px;
    display: inline-flex;
    align-items: center;
  }

  a {
    color: inherit;
    text-decoration: none;
    transition: color 0.15s ease;
  }

  a:hover,
  a:focus-visible {
    color: var(--accent);
    font-weight: 500;
  }

  a:focus-visible {
    border-radius: 3px;
    outline: 2px solid var(--accent-2);
    outline-offset: 4px;
  }

  small {
    justify-self: end;
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

  @media (max-width: 560px) {
    .site-footer {
      min-height: 0;
      grid-template-columns: 1fr;
      gap: 0;
      padding-block: 2rem calc(2rem + env(safe-area-inset-bottom));
    }

    .footer-nav {
      gap: 0 1rem;
    }

    small {
      justify-self: start;
    }
  }
</style>

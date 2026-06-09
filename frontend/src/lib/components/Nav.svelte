<script lang="ts">
  import { router, goHome, goAdmin, goHowto } from '../router.svelte'
  import { isSuperadmin } from '../nostr/admin'

  // Mounted twice: once in the header (desktop tabs) and once at top level
  // (mobile fixed bottom bar). `mobile` selects which one this instance is —
  // it must live outside the backdrop-filtered header, else position:fixed is
  // trapped by the header's containing block. Profile/sign-in live in the
  // top-right account badge (click → profile; logout is on the profile page).
  let { mobile = false }: { mobile?: boolean } = $props()

  const onHome = $derived(router.route.name === 'home')
  const onAdmin = $derived(router.route.name === 'admin')
  const onHowto = $derived(router.route.name === 'howto')
</script>

<nav class="nav" class:mobile aria-label="Primary">
  <button class="tab" class:active={onHome} onclick={goHome}>
    <span class="ico" aria-hidden="true">🎵</span>
    <span class="lbl">Clubs</span>
  </button>

  <button class="tab" class:active={onHowto} onclick={goHowto}>
    <span class="ico" aria-hidden="true">❔</span>
    <span class="lbl">How-to</span>
  </button>

  {#if isSuperadmin()}
    <button class="tab" class:active={onAdmin} onclick={goAdmin}>
      <span class="ico" aria-hidden="true">⚙️</span>
      <span class="lbl">Admin</span>
    </button>
  {/if}
</nav>

<style>
  /* ── Desktop instance (in the header): inline tab pills ───────────────── */
  .nav {
    display: flex;
    align-items: center;
    gap: 0.3rem;
  }
  .nav.mobile {
    display: none;
  }
  .tab {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    background: none;
    border: 1px solid transparent;
    border-radius: 999px;
    color: var(--text-dim);
    cursor: pointer;
    padding: 0.35rem 0.8rem;
    font-size: 0.88rem;
    font-weight: 600;
    transition: color 0.15s ease, background 0.15s ease, border-color 0.15s ease;
  }
  .tab:hover {
    color: var(--text);
    background: var(--bg-elev);
  }
  .tab.active {
    color: var(--accent);
    border-color: var(--border);
    background: var(--bg-elev);
  }
  .ico {
    font-size: 1rem;
    line-height: 1;
  }
  .lbl {
    line-height: 1;
  }

  /* ── Mobile: hide the desktop instance, show the fixed bottom bar ─────── */
  @media (max-width: 560px) {
    .nav:not(.mobile) {
      display: none;
    }
    .nav.mobile {
      display: flex;
      position: fixed;
      bottom: 0;
      left: 0;
      right: 0;
      z-index: 55;
      gap: 0;
      background: color-mix(in srgb, var(--bg-elev) 94%, transparent);
      backdrop-filter: blur(10px);
      border-top: 1px solid var(--border);
      padding-bottom: env(safe-area-inset-bottom);
    }
    .nav.mobile .tab {
      flex: 1;
      flex-direction: column;
      gap: 0.15rem;
      border-radius: 0;
      border: none;
      padding: 0.5rem 0.2rem 0.55rem;
      min-height: 3.2rem;
      font-size: 0.66rem;
    }
    .nav.mobile .tab:hover {
      background: none;
    }
    .nav.mobile .tab.active {
      background: none;
      border: none;
    }
    .nav.mobile .ico {
      font-size: 1.2rem;
    }
  }
</style>

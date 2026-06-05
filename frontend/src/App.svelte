<script lang="ts">
  import { router, goHome } from './lib/router.svelte'
  import { startConnectionWatch, connection } from './lib/nostr/connection.svelte'
  import LoginButton from './lib/components/LoginButton.svelte'
  import LoginDialog from './lib/components/LoginDialog.svelte'
  import ClubList from './lib/components/ClubList.svelte'
  import ClubView from './lib/components/ClubView.svelte'

  startConnectionWatch()
</script>

<header class="topbar">
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions -->
  <div class="brand" role="button" tabindex="0" onclick={goHome}>
    <svg class="logo" viewBox="0 0 36 36" width="32" height="32" role="img" aria-label="zapclub turntable">
      <!-- spinning vinyl record (Nostr purple) -->
      <g class="vinyl">
        <circle cx="16" cy="20" r="13" fill="#1b0b33" stroke="#8e30eb" stroke-width="1.6" />
        <circle cx="16" cy="20" r="9.5" fill="none" stroke="#a855f7" stroke-width="0.5" opacity="0.4" />
        <circle cx="16" cy="20" r="6.5" fill="none" stroke="#a855f7" stroke-width="0.5" opacity="0.3" />
        <circle cx="16" cy="20" r="3.6" fill="#8e30eb" />
        <!-- bright mark so the spin is visible -->
        <circle cx="16" cy="11.5" r="1.1" fill="#d8b4fe" />
        <circle cx="16" cy="20" r="1" fill="#1b0b33" />
      </g>
      <!-- static tonearm -->
      <line x1="29" y1="7" x2="20.5" y2="15.5" stroke="#c084fc" stroke-width="1.7" stroke-linecap="round" />
      <circle cx="29" cy="7" r="1.9" fill="#c084fc" />
    </svg>
    <span>zapclub<span class="dim">.io</span></span>
  </div>
  <LoginButton />
</header>

{#if connection.known && !connection.clubConnected}
  <div class="reconnect">Reconnecting to the club relay…</div>
{/if}

<main>
  {#if router.route.name === 'club'}
    {#key router.route.id}
      <ClubView groupId={router.route.id} />
    {/key}
  {:else if router.route.name === 'user'}
    <div class="stub">
      <p>Profiles are coming soon.</p>
      <button class="btn btn-ghost btn-sm" onclick={goHome}>← Back to clubs</button>
    </div>
  {:else}
    <ClubList />
  {/if}
</main>

<LoginDialog />

<style>
  .topbar {
    position: sticky;
    top: 0;
    z-index: 50;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.7rem 1rem;
    background: rgba(7, 7, 10, 0.8);
    backdrop-filter: blur(10px);
    border-bottom: 1px solid var(--border);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-weight: 800;
    font-size: 1.15rem;
    cursor: pointer;
    letter-spacing: -0.02em;
  }
  .logo {
    flex: 0 0 auto;
    filter: drop-shadow(0 0 6px rgba(142, 48, 235, 0.55));
  }
  /* Turntable: the vinyl record spins, the tonearm stays put (Nostr purple). */
  .logo .vinyl {
    transform-origin: 16px 20px;
    animation: vinyl-spin 2.4s linear infinite;
  }
  @keyframes vinyl-spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .logo .vinyl {
      animation: none;
    }
  }
  .brand .dim {
    color: var(--text-dim);
    font-weight: 700;
  }
  .reconnect {
    background: var(--bg-elev-2);
    border-bottom: 1px solid var(--border);
    color: var(--amber);
    text-align: center;
    font-size: 0.8rem;
    padding: 0.4rem;
  }
  .stub {
    max-width: 680px;
    margin: 0 auto;
    padding: 3rem 1rem;
    text-align: center;
    color: var(--text-dim);
    display: flex;
    flex-direction: column;
    gap: 1rem;
    align-items: center;
  }
</style>

<script lang="ts">
  import icon from './assets/zapclub-icon.png'
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
    <img class="logo" src={icon} alt="zapclub" width="26" height="34" />
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
    transform-origin: center;
    animation: logo-pulse 1.6s ease-in-out infinite;
  }
  /* Pulse: gentle scale + neon glow breathing (matches the bolt's purple/amber). */
  @keyframes logo-pulse {
    0%,
    100% {
      transform: scale(1);
      filter: drop-shadow(0 0 4px rgba(177, 77, 255, 0.5));
    }
    50% {
      transform: scale(1.12);
      filter: drop-shadow(0 0 13px rgba(177, 77, 255, 0.9));
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .logo {
      animation: none;
      filter: drop-shadow(0 0 6px rgba(177, 77, 255, 0.55));
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

<script lang="ts">
  import { onMount } from 'svelte'
  import { router, goHome, goAdmin } from './lib/router.svelte'
  import { isSuperadmin } from './lib/nostr/admin'
  import { startConnectionWatch, connection } from './lib/nostr/connection.svelte'
  import { accountWatch, startAccountWatch } from './lib/nostr/accountWatch.svelte'
  import { logout, launchLogin } from './lib/nostr/nostrLogin'
  import LoginButton from './lib/components/LoginButton.svelte'
  import LoginDialog from './lib/components/LoginDialog.svelte'
  import ClubList from './lib/components/ClubList.svelte'
  import ClubView from './lib/components/ClubView.svelte'
  import Turntable from './lib/components/Turntable.svelte'
  import UserProfile from './lib/components/UserProfile.svelte'
  import AdminDashboard from './lib/components/AdminDashboard.svelte'
  import HowTo from './lib/components/HowTo.svelte'
  import About from './lib/components/About.svelte'
  import Disclaimer from './lib/components/Disclaimer.svelte'
  import Leaderboard from './lib/components/Leaderboard.svelte'
  import PayModal from './lib/components/PayModal.svelte'
  import SiteFooter from './lib/components/SiteFooter.svelte'
  import LedThemeSwitcher from './lib/components/LedThemeSwitcher.svelte'

  startConnectionWatch()
  startAccountWatch()

  onMount(() => {
    document.body.classList.add('site-led-page')
    return () => document.body.classList.remove('site-led-page')
  })

  // Extension switched to a different account than we're logged in as → re-login as it.
  function reloginExtension() {
    logout()
    launchLogin()
  }

</script>

<div class="app-frame">
<header class="topbar">
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions -->
  <div class="brand" role="button" tabindex="0" onclick={goHome}>
    <Turntable size={32} />
    <span><span class="word">zapclub</span><span class="tld">.io</span></span>
  </div>
  <div class="top-actions">
    <LedThemeSwitcher />
    {#if isSuperadmin()}
      <button class="icon-btn" onclick={goAdmin} title="Admin" aria-label="Admin">⚙️</button>
    {/if}
    <LoginButton />
  </div>
</header>

{#if connection.known && !connection.clubConnected}
  <div class="reconnect">Reconnecting to the club relay…</div>
{/if}

{#if accountWatch.mismatch}
  <div class="reconnect mismatch">
    Your Nostr extension is on a different account — zapclub can't sign as the one you're logged in as.
    <button class="relogin" onclick={reloginExtension}>Re-login</button>
  </div>
{/if}

<main>
  {#if router.route.name === 'club'}
    {#key router.route.id}
      <ClubView groupId={router.route.id} />
    {/key}
  {:else if router.route.name === 'user'}
    {#key router.route.npub}
      <UserProfile npub={router.route.npub} />
    {/key}
  {:else if router.route.name === 'admin'}
    <AdminDashboard />
  {:else if router.route.name === 'howto'}
    <HowTo />
  {:else if router.route.name === 'about'}
    <About />
  {:else if router.route.name === 'credits'}
    <About />
  {:else if router.route.name === 'disclaimer'}
    <Disclaimer />
  {:else if router.route.name === 'leaderboard'}
    <Leaderboard />
  {:else if router.route.name === 'privacy'}
    <Disclaimer />
  {:else if router.route.name === 'terms'}
    <Disclaimer />
  {:else if router.route.name === 'legal'}
    <Disclaimer />
  {:else}
    <ClubList />
  {/if}
</main>

<SiteFooter />
</div>

<LoginDialog />
<PayModal />

<style>
  .topbar {
    position: fixed;
    top: 8px;
    left: 50%;
    transform: translateX(-50%);
    width: min(1120px, calc(100vw - 2rem));
    height: var(--topbar-h);
    z-index: 50;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 1rem;
    background: transparent;
    white-space: nowrap;
  }
  :global(body.site-led-page) .app-frame {
    width: 100%;
    min-height: 100vh;
    margin: 0;
    overflow: visible;
    border: 0;
    border-radius: 0;
    background: transparent;
    box-shadow: none;
  }
  :global(body.site-led-page) .app-frame .topbar {
    position: relative;
    top: auto;
    left: auto;
    transform: none;
    width: min(960px, 100%);
    margin-inline: auto;
    padding-inline: 1.35rem;
    border-bottom: 1px solid rgba(207, 233, 255, 0.22);
  }
  .top-actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border-radius: 999px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 1rem;
    font-weight: 700;
    line-height: 1;
    cursor: pointer;
  }
  .icon-btn:hover {
    border-color: var(--accent-2);
    color: var(--text);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-weight: 800;
    font-size: 1.25rem;
    cursor: pointer;
    letter-spacing: -0.02em;
  }
  .brand .word {
    color: #fff;
  }
  .brand .tld {
    /* Nostr purple */
    color: #8e30eb;
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
  .reconnect.mismatch {
    color: var(--danger);
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.6rem;
    flex-wrap: wrap;
  }
  .relogin {
    background: var(--bg-elev);
    border: 1px solid var(--danger);
    color: var(--danger);
    border-radius: 999px;
    padding: 0.15rem 0.6rem;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .relogin:hover {
    background: var(--danger);
    color: #fff;
  }
  /* Keep route content clear of mobile browser chrome. */
  @media (max-width: 560px) {
    :global(body.site-led-page) .app-frame {
      width: 100%;
      margin-top: 0;
      border-radius: 0;
    }
    :global(body.site-led-page) .app-frame .topbar {
      padding-inline: 0.9rem;
    }
    main {
      padding-bottom: 1rem;
    }
  }
</style>

<script lang="ts">
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
  import Leaderboard from './lib/components/Leaderboard.svelte'
  import PayModal from './lib/components/PayModal.svelte'
  import SiteFooter from './lib/components/SiteFooter.svelte'

  startConnectionWatch()
  startAccountWatch()

  // Extension switched to a different account than we're logged in as → re-login as it.
  function reloginExtension() {
    logout()
    launchLogin()
  }

</script>

<header class="topbar">
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions -->
  <div class="brand" role="button" tabindex="0" onclick={goHome}>
    <Turntable size={32} />
    <span><span class="word">zapclub</span><span class="tld">.io</span></span>
  </div>
  <div class="top-actions">
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
    <About />
  {:else if router.route.name === 'leaderboard'}
    <Leaderboard />
  {:else if router.route.name === 'privacy'}
    <About />
  {:else if router.route.name === 'terms'}
    <About />
  {:else if router.route.name === 'legal'}
    <About />
  {:else}
    <ClubList />
  {/if}
</main>

<SiteFooter />

<LoginDialog />
<PayModal />

<style>
  .topbar {
    position: fixed;
    top: 8px;
    left: 50%;
    transform: translateX(-50%);
    width: min(960px, calc(100vw - 2rem));
    height: var(--topbar-h);
    z-index: 50;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 1rem;
    background: transparent;
    white-space: nowrap;
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
    main {
      padding-bottom: 1rem;
    }
  }
</style>

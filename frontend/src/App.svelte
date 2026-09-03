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
  import Credits from './lib/components/Credits.svelte'
  import Disclaimer from './lib/components/Disclaimer.svelte'
  import Leaderboard from './lib/components/Leaderboard.svelte'
  import LegalNotice from './lib/components/LegalNotice.svelte'
  import Privacy from './lib/components/Privacy.svelte'
  import Terms from './lib/components/Terms.svelte'
  import PayModal from './lib/components/PayModal.svelte'
  import SiteFooter from './lib/components/SiteFooter.svelte'
  import { requestZapInvoice } from './lib/nostr/zaps.svelte'
  import { showPay } from './lib/nostr/payModal.svelte'

  startConnectionWatch()
  startAccountWatch()

  // Extension switched to a different account than we're logged in as → re-login as it.
  function reloginExtension() {
    logout()
    launchLogin()
  }

  // Footer donation — plain LNURL payment to the project's lightning address.
  const DONATE_LUD16 = 'zapclub@nsnip.io'
  let donating = $state(false)
  async function donate(sats: number) {
    if (donating) return
    donating = true
    try {
      const { invoice, verify } = await requestZapInvoice('', DONATE_LUD16, sats, 'zapclub donation')
      showPay(invoice, sats, 'Tip zapclub', { verify })
    } catch {
      /* ignore — user can retry */
    } finally {
      donating = false
    }
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
    <Credits />
  {:else if router.route.name === 'disclaimer'}
    <Disclaimer />
  {:else if router.route.name === 'leaderboard'}
    <Leaderboard />
  {:else if router.route.name === 'privacy'}
    <Privacy />
  {:else if router.route.name === 'terms'}
    <Terms />
  {:else if router.route.name === 'legal'}
    <LegalNotice />
  {:else}
    <ClubList />
  {/if}
</main>

<aside class="support-bar" aria-label="Support Zapclub">
  <span class="tip-label">⚡ Tip zapclub</span>
  {#each [100, 1000, 5000] as amt (amt)}
    <button class="tip" onclick={() => donate(amt)} disabled={donating}>{amt}</button>
  {/each}
  <span class="support-note">Powered by Nostr &amp; Lightning · released at <a class="block" href="https://mempool.space/block/940329" target="_blank" rel="noopener noreferrer">940329</a></span>
</aside>

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
  .support-bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    padding: 1.6rem 1rem 2rem;
    border-top: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 0.8rem;
  }
  .tip-label {
    font-weight: 700;
    color: var(--amber);
  }
  .tip {
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text);
    border-radius: 999px;
    padding: 0.25rem 0.7rem;
    font-size: 0.78rem;
    font-weight: 700;
    cursor: pointer;
  }
  .tip:hover:not(:disabled) {
    border-color: var(--amber);
    color: var(--amber);
  }
  .tip:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .support-note {
    flex-basis: 100%;
    text-align: center;
    margin-top: 0.4rem;
    font-size: 0.72rem;
  }
  .block {
    color: var(--text-dim);
    text-decoration: none;
    font-variant-numeric: tabular-nums;
  }
  .block:hover {
    color: var(--accent);
    text-decoration: underline;
  }
  /* Keep route content clear of mobile browser chrome. */
  @media (max-width: 560px) {
    main {
      padding-bottom: 1rem;
    }
  }
</style>

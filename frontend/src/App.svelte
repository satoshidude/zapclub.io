<script lang="ts">
  import { router, goHome, goAbout, goHowto, goAdmin, goLeaderboard } from './lib/router.svelte'
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
  import MiniPlayer from './lib/components/MiniPlayer.svelte'
  import PayModal from './lib/components/PayModal.svelte'
  import { requestZapInvoice } from './lib/nostr/zaps.svelte'
  import { showPay } from './lib/nostr/payModal.svelte'
  import { registerActiveClub, persistedActiveClub } from './lib/nostr/miniplay.svelte'
  import { fetchClub } from './lib/nostr/groups'
  import { watchOwnPremium } from './lib/nostr/premium.svelte'
  import { auth } from './lib/nostr/auth.svelte'

  startConnectionWatch()
  startAccountWatch()

  // Keep the logged-in user's premium status live. Start on login, stop on logout.
  $effect(() => {
    if (!auth.pubkey) return
    return watchOwnPremium()
  })

  // Extension switched to a different account than we're logged in as → re-login as it.
  function reloginExtension() {
    logout()
    launchLogin()
  }

  // Reload-resume: pick up the club whose audio was playing and keep it going in the
  // mini-player (unless we land straight back on that club's page, which has its own).
  {
    const resumeId = persistedActiveClub()
    if (resumeId && !(router.route.name === 'club' && router.route.id === resumeId)) {
      registerActiveClub(resumeId, '')
      void fetchClub(resumeId).then((c) => c?.name && registerActiveClub(resumeId, c.name))
    }
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
    <button class="icon-btn" onclick={goLeaderboard} title="Zap leaderboard" aria-label="Zap leaderboard">🏆</button>
    <button class="icon-btn" onclick={goHowto} title="How it works" aria-label="How it works">?</button>
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
  {:else if router.route.name === 'leaderboard'}
    <Leaderboard />
  {:else}
    <ClubList />
  {/if}
</main>

<footer class="footer">
  <span class="tip-label">⚡ Tip zapclub</span>
  {#each [100, 1000, 5000] as amt (amt)}
    <button class="tip" onclick={() => donate(amt)} disabled={donating}>{amt}</button>
  {/each}
  <span class="foot-note"><button class="foot-link" onclick={goLeaderboard}>Leaderboard</button> · <button class="foot-link" onclick={goAbout}>How it works</button> · <a class="foot-link" href="https://github.com/satoshidude/zapclub.io" target="_blank" rel="noopener noreferrer">GitHub</a> · Powered by Nostr &amp; Lightning · no tracking</span>
  <span class="foot-note">released at <a class="block" href="https://mempool.space/block/940329" target="_blank" rel="noopener noreferrer">940329</a> with love 4 music</span>
</footer>

<MiniPlayer />
<LoginDialog />
<PayModal />

<style>
  .topbar {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    height: var(--topbar-h);
    z-index: 50;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 1rem;
    background: rgba(7, 7, 10, 0.8);
    backdrop-filter: blur(10px);
    border-bottom: 1px solid var(--border);
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
  .footer {
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
  .foot-note {
    flex-basis: 100%;
    text-align: center;
    margin-top: 0.4rem;
    font-size: 0.72rem;
  }
  .foot-note + .foot-note {
    margin-top: 0.15rem;
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
  .foot-link {
    background: none;
    border: none;
    padding: 0;
    color: var(--accent);
    font: inherit;
    cursor: pointer;
  }
  .foot-link:hover {
    text-decoration: underline;
  }
  /* Mobile: leave room for the fixed bottom nav so content isn't hidden behind it. */
  @media (max-width: 560px) {
    main {
      padding-bottom: 1rem;
    }
    .footer {
      padding-bottom: calc(4.8rem + env(safe-area-inset-bottom));
    }
  }
</style>

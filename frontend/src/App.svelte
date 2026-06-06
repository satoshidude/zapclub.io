<script lang="ts">
  import { router, goHome } from './lib/router.svelte'
  import { startConnectionWatch, connection } from './lib/nostr/connection.svelte'
  import LoginButton from './lib/components/LoginButton.svelte'
  import LoginDialog from './lib/components/LoginDialog.svelte'
  import ClubList from './lib/components/ClubList.svelte'
  import ClubView from './lib/components/ClubView.svelte'
  import Nav from './lib/components/Nav.svelte'
  import Turntable from './lib/components/Turntable.svelte'
  import UserProfile from './lib/components/UserProfile.svelte'
  import AdminDashboard from './lib/components/AdminDashboard.svelte'
  import HowTo from './lib/components/HowTo.svelte'
  import MiniPlayer from './lib/components/MiniPlayer.svelte'
  import PayModal from './lib/components/PayModal.svelte'
  import { requestZapInvoice } from './lib/nostr/zaps.svelte'
  import { showPay } from './lib/nostr/payModal.svelte'
  import { registerActiveClub, persistedActiveClub } from './lib/nostr/miniplay.svelte'
  import { fetchClub } from './lib/nostr/groups'

  startConnectionWatch()

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
  <Nav />
  <div class="account"><LoginButton /></div>
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
    {#key router.route.npub}
      <UserProfile npub={router.route.npub} />
    {/key}
  {:else if router.route.name === 'admin'}
    <AdminDashboard />
  {:else if router.route.name === 'howto'}
    <HowTo />
  {:else}
    <ClubList />
  {/if}
</main>

<footer class="footer">
  <span class="tip-label">⚡ Tip zapclub</span>
  {#each [100, 1000, 5000] as amt (amt)}
    <button class="tip" onclick={() => donate(amt)} disabled={donating}>{amt}</button>
  {/each}
  <span class="foot-note">Powered by Nostr &amp; Lightning · no tracking</span>
</footer>

<Nav mobile />

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

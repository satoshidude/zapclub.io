<script lang="ts">
  import { auth } from '../nostr/auth.svelte'
  import { launchLogin, launchSignup, logout } from '../nostr/nostrLogin'
  import ProfileBadge from './ProfileBadge.svelte'

  let menuOpen = $state(false)
</script>

{#if auth.isLoggedIn}
  <div class="account">
    <button class="trigger" onclick={() => (menuOpen = !menuOpen)} aria-expanded={menuOpen}>
      <ProfileBadge pubkey={auth.pubkey!} npub={auth.npub!} profile={auth.profile} size={34} />
    </button>
    {#if menuOpen}
      <div class="menu" role="menu">
        <button
          class="menu-item"
          role="menuitem"
          onclick={() => {
            logout()
            menuOpen = false
          }}
        >
          Log out
        </button>
      </div>
    {/if}
  </div>
{:else}
  <div class="login-actions">
    <button class="btn btn-ghost btn-sm signup" onclick={launchSignup}>Create account</button>
    <button class="btn btn-primary btn-sm" onclick={launchLogin}>Sign in</button>
  </div>
{/if}

<style>
  .account {
    position: relative;
  }
  .trigger {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.25rem 0.6rem 0.25rem 0.25rem;
  }
  .trigger:hover {
    border-color: var(--accent-2);
  }
  .menu {
    position: absolute;
    right: 0;
    top: calc(100% + 0.4rem);
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.3rem;
    min-width: 160px;
    z-index: 20;
    box-shadow: 0 12px 30px rgba(0, 0, 0, 0.4);
  }
  .menu-item {
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    color: var(--text);
    padding: 0.5rem 0.6rem;
    border-radius: 7px;
    font-size: 0.9rem;
  }
  .menu-item:hover {
    background: var(--bg);
    color: var(--danger);
  }
  .login-actions {
    display: flex;
    gap: 0.5rem;
    align-items: center;
  }
  @media (max-width: 560px) {
    .login-actions .signup {
      display: none;
    }
  }
</style>

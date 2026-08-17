<script lang="ts">
  import { auth } from '../nostr/auth.svelte'
  import { launchLogin, launchSignup } from '../nostr/nostrLogin'
  import { goUser } from '../router.svelte'
  import ProfileBadge from './ProfileBadge.svelte'
</script>

{#if auth.isLoggedIn}
  <!-- Click the badge → your profile page (logout lives there). -->
  <button class="trigger" onclick={() => goUser(auth.npub!)} title="Your profile">
    <ProfileBadge pubkey={auth.pubkey!} npub={auth.npub!} profile={auth.profile} size={34} />
  </button>
{:else}
  <div class="login-actions">
    <button class="btn btn-ghost btn-sm signup" onclick={launchSignup}>Create account</button>
    <button class="btn btn-primary btn-sm" onclick={launchLogin}>Sign in</button>
  </div>
{/if}

<style>
  .trigger {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.25rem 0.6rem 0.25rem 0.25rem;
    cursor: pointer;
  }
  .trigger:hover {
    border-color: var(--accent-2);
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

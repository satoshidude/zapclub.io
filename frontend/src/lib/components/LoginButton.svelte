<script lang="ts">
  import { auth } from '../nostr/auth.svelte'
  import { launchLogin, launchSignup } from '../nostr/nostrLogin'
  import { goUser } from '../router.svelte'
  import ProfileBadge from './ProfileBadge.svelte'
</script>

{#if auth.isLoggedIn}
  <!-- The profile is navigation, so it is an open link rather than button chrome. -->
  <a
    class="profile-link"
    href={`/user/${auth.npub!}`}
    onclick={(event) => {
      event.preventDefault()
      goUser(auth.npub!)
    }}
    title="Your profile"
  >
    <ProfileBadge pubkey={auth.pubkey!} npub={auth.npub!} profile={auth.profile} size={34} />
  </a>
{:else}
  <div class="login-actions">
    <button class="btn btn-ghost btn-sm signup" onclick={launchSignup}>Create account</button>
    <button class="btn btn-primary btn-sm" onclick={launchLogin}>Sign in</button>
  </div>
{/if}

<style>
  .profile-link {
    display: inline-flex;
    align-items: center;
    padding: 0.25rem 0;
    color: inherit;
    text-decoration: none;
  }
  .profile-link:focus-visible {
    outline: 1px solid currentColor;
    outline-offset: 4px;
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

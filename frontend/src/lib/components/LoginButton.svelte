<script lang="ts">
  import { auth } from '../nostr/auth.svelte'
  import { launchLogin } from '../nostr/nostrLogin'
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
    <button class="btn btn-sm sign-in" onclick={launchLogin}>Sign in</button>
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
  .sign-in {
    min-height: 44px;
    padding: 0 0.35rem;
    border: 0;
    border-radius: 0;
    background: transparent;
    color: var(--accent);
    font-family: var(--font-display);
    font-size: 13px;
    font-weight: 400;
    letter-spacing: 0.08em;
    text-shadow: 0 0 3px rgba(74, 222, 94, 0.65), 0 0 9px rgba(74, 222, 94, 0.22);
  }
  .sign-in:hover {
    border-color: transparent;
    background: transparent;
    color: #8cf29a;
    filter: none;
    text-shadow: 0 0 4px rgba(74, 222, 94, 0.8), 0 0 11px rgba(74, 222, 94, 0.3);
  }
  .sign-in:focus-visible {
    outline: 1px solid currentColor;
    outline-offset: 3px;
  }
</style>

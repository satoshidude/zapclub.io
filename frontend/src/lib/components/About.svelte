<script lang="ts">
  import { goHome } from '../router.svelte'
</script>

<div class="wrap">
  <header class="hh">
    <h1>About ZapClub</h1>
    <p class="sub">Decentralized social music clubs — built on Nostr and Lightning.</p>
  </header>

  <section class="card">
    <p>
      ZapClub is a decentralized social music platform built on Nostr and the Lightning Network.
      Create music clubs, listen to synchronized music together, discover new artists, build
      communities and support DJs directly through Lightning payments.
    </p>
    <p>
      Unlike traditional streaming platforms, ZapClub focuses on shared experiences rather than
      algorithmic recommendations. No accounts, no central database, no tracking. Your Nostr key
      is your identity.
    </p>
    <ul class="feat-list">
      <li><strong>Synchronized playback</strong> — relay conductor drives the clock; everyone hears the same moment.</li>
      <li><strong>DJ stage &amp; round-robin</strong> — up to 5 DJs; queues interleaved track-by-track.</li>
      <li><strong>VibeMeter</strong> — zap the live DJ with a banger vote or nudge the room to skip; the meter resets per track.</li>
      <li><strong>Zaps (NIP-57)</strong> — pay the live DJ directly with Lightning; sats go straight to their wallet.</li>
      <li><strong>Avatars &amp; moderation</strong> — Nostr profiles with relay-enforced bans and roles.</li>
    </ul>
  </section>

  <section class="card">
    <h2>Vision</h2>
    <p>
      A global network of community-owned music clubs powered by Nostr and Lightning. No label
      platform, no tracking, no central identity. A club that genuinely belongs to the people in it.
    </p>
    <p>
      Built with Nostr (NIP-01/07/29/42/57), khatru + relay29, nostr-tools and Svelte.
      <a href="https://github.com/satoshidude/zapclub.io" target="_blank" rel="noopener noreferrer">Open source on GitHub.</a>
    </p>
    <button class="home" onclick={goHome}>← Back to the clubs</button>
  </section>
</div>

<style>
  .wrap {
    max-width: 640px;
    margin: 0 auto;
    padding: 1.4rem 1rem 5rem;
  }
  .hh {
    margin-bottom: 1.2rem;
  }
  h1 {
    margin: 0;
    font-size: 1.6rem;
  }
  .sub {
    color: var(--text-dim);
    margin: 0.3rem 0 0;
    line-height: 1.5;
  }
  .card {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.1rem 1.2rem;
    margin-bottom: 1rem;
  }
  h2 {
    margin: 0 0 0.6rem;
    font-size: 1.05rem;
  }
  p {
    margin: 0 0 0.6rem;
    font-size: 0.92rem;
    line-height: 1.55;
    color: var(--text);
  }
  p:last-child { margin-bottom: 0; }
  .feat-list {
    margin: 0.5rem 0 0;
    padding-left: 1.1rem;
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    font-size: 0.9rem;
    line-height: 1.5;
    color: var(--text);
  }
  a {
    color: var(--accent);
    font-weight: 600;
  }
  a:hover { text-decoration: underline; }
  .home {
    margin-top: 0.8rem;
    background: none;
    border: none;
    color: var(--accent);
    font-weight: 600;
    cursor: pointer;
    padding: 0;
    font-size: 0.92rem;
  }
  .home:hover { text-decoration: underline; }
</style>

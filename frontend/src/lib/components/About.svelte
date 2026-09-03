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
  </section>

  <section class="card">
    <h2>Credits &amp; source</h2>
    <p>
      Open protocols and open-source tools make ZapClub possible. It is built with Nostr,
      Bitcoin Lightning, Svelte, nostr-tools, khatru and relay29. Synchronized playback uses
      the YouTube IFrame API.
    </p>
    <p>
      The source code and complete dependency record are available in the
      <a href="https://github.com/satoshidude/zapclub.io" target="_blank" rel="noopener noreferrer">ZapClub repository</a>.
    </p>
  </section>

  <section class="card">
    <h2>Privacy</h2>
    <p>
      ZapClub is designed around decentralized identities and open protocols. Information may be
      processed through Nostr relays, Lightning infrastructure and other decentralized services.
      The project minimizes centralized data collection wherever technically feasible, but users
      remain responsible for understanding the privacy implications of the services they use.
    </p>
  </section>

  <section class="card">
    <h2>Terms &amp; disclaimer</h2>
    <p>
      ZapClub is an experimental music, technology, education and digital-art project. It is not
      a music streaming, banking or escrow service and is provided “as is” and “as available”,
      without guarantees of availability, correctness or fitness for a particular purpose.
    </p>
    <p>
      Users are responsible for the clubs they operate, the content they publish or reference,
      the payments they initiate and their interactions with other users. External media, Nostr
      relays and Lightning services are operated by third parties. ZapClub does not control their
      content, availability or privacy practices. Lightning payments are irreversible; use only
      small amounts.
    </p>
    <p>
      To the fullest extent permitted by applicable law, the operators are not liable for damages
      arising from use of the platform.
    </p>
  </section>

  <button class="home" onclick={goHome}>← Back to the clubs</button>
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

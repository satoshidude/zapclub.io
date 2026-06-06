<script lang="ts">
  import { goHome } from '../router.svelte'
</script>

<div class="wrap">
  <header class="hh">
    <h1>How-to & tips</h1>
    <p class="sub">Get the most out of zapclub — bring your playlists, set up zaps, and earn sats.</p>
  </header>

  <section class="card">
    <h2>🍏 On iOS / macOS Safari</h2>
    <p>
      Safari has no built-in Nostr login, so you sign in with a small signing extension.
      <strong>Nostash</strong> works best right now — it gives Safari the
      <code>window.nostr</code> login that zapclub uses. For paying zaps, <strong>Alby Go</strong>
      is the smoothest. zapclub supports both:
    </p>
    <ul class="links">
      <li>
        <a href="https://apps.apple.com/us/app/nostash/id6744309333" target="_blank" rel="noopener noreferrer">Nostash — App Store</a>
        (iPhone / iPad / Mac) — <strong>sign in</strong>. After installing, open the Nostash
        icon in Safari and <strong>allow it for zapclub</strong>, then tap
        “🧩 Browser extension (NIP-07)” on the sign-in screen.
      </li>
      <li>
        <a href="https://apps.apple.com/us/app/alby-go/id6471335774" target="_blank" rel="noopener noreferrer">Alby Go — App Store</a>
        (iPhone / iPad) — <strong>pay zaps</strong>. Tap “📲 Open in Alby Go” on the pay screen and confirm.
      </li>
    </ul>
    <p class="note">First time only: you must grant Nostash access to the site, otherwise Safari won’t expose the login. The extension button then appears automatically.</p>
  </section>

  <section class="card">
    <h2>🎚️ Bring your Spotify / Apple Music playlists</h2>
    <p>
      zapclub plays from YouTube. To move an existing Spotify or Apple Music playlist over,
      run it through a free converter, then paste the resulting <strong>YouTube playlist
      link</strong> into the search box in the DJ Station — “Add all” drops every track into
      your set.
    </p>
    <ul class="links">
      <li>
        <a href="https://www.tunemymusic.com/" target="_blank" rel="noopener noreferrer">TuneMyMusic</a>
        — Spotify / Apple Music → YouTube, free for a full playlist transfer.
      </li>
      <li>
        <a href="https://soundiiz.com/" target="_blank" rel="noopener noreferrer">Soundiiz</a>
        — another solid converter across many services.
      </li>
    </ul>
    <p class="note">Tip: pick “YouTube” as the destination, create the playlist, then copy its share link.</p>
  </section>

  <section class="card">
    <h2>⚡ Set up a lightning address (to receive zaps)</h2>
    <p>
      Zaps are real sats sent straight to the DJ that’s playing. To <strong>receive</strong>
      them you need a <strong>lightning address</strong> (looks like an email:
      <code>you@wallet.com</code>). Add it under <em>Profile → Edit</em>. No address yet?
      Get one in a minute:
    </p>
    <ul class="links">
      <li>
        <a href="https://rizful.com/" target="_blank" rel="noopener noreferrer">Rizful</a>
        — Nostr-native lightning, gives you an address tied to your identity.
      </li>
      <li>
        <a href="https://www.walletofsatoshi.com/" target="_blank" rel="noopener noreferrer">Wallet of Satoshi</a>
        — one-tap install, instant lightning address, zero setup.
      </li>
      <li>
        <a href="https://coinos.io/" target="_blank" rel="noopener noreferrer">Coinos</a>
        — sign up and you instantly get <code>name@coinos.io</code>.
      </li>
    </ul>
    <p class="note">Once you’ve got an address, paste it into your profile — that’s the only thing standing between you and earning sats on stage.</p>
  </section>

  <section class="card">
    <h2>🐝 Zap faster with Alby</h2>
    <p>
      Zapping is more fun when paying is one tap. <strong>Alby</strong> handles that on both
      desktop and mobile:
    </p>
    <ul class="links">
      <li>
        <a href="https://getalby.com/products/alby-go" target="_blank" rel="noopener noreferrer">Alby Go</a>
        (iOS / Android) — tap “Open in Alby Go” on the pay screen and confirm. Done.
      </li>
      <li>
        <a href="https://getalby.com/" target="_blank" rel="noopener noreferrer">Alby browser extension</a>
        (Chrome / Firefox) — enables the in-page “⚡ Pay now” button (WebLN), no QR needed.
      </li>
    </ul>
    <p class="note">Any lightning wallet works via QR or copy — Alby just makes it a single click.</p>
  </section>

  <button class="btn btn-ghost" onclick={goHome}>← Back to clubs</button>
</div>

<style>
  .wrap {
    max-width: 680px;
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
  }
  .card {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.1rem 1.2rem;
    margin-bottom: 1rem;
  }
  h2 {
    margin: 0 0 0.5rem;
    font-size: 1.1rem;
  }
  p {
    margin: 0 0 0.6rem;
    font-size: 0.92rem;
    line-height: 1.5;
    color: var(--text);
  }
  .links {
    margin: 0.4rem 0;
    padding-left: 1.1rem;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .links li {
    font-size: 0.9rem;
    line-height: 1.45;
  }
  a {
    color: var(--accent);
    font-weight: 600;
  }
  a:hover {
    text-decoration: underline;
  }
  code {
    background: var(--bg-elev-2);
    border-radius: 4px;
    padding: 0.05rem 0.3rem;
    font-size: 0.85em;
  }
  .note {
    margin: 0.6rem 0 0;
    font-size: 0.82rem;
    color: var(--text-dim);
  }
</style>

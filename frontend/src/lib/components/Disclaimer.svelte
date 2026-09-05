<script lang="ts">
  import { goHome } from '../router.svelte'
</script>

<div class="legal-page">
  <article class="legal-card led-zone">
    <header class="legal-hero">
      <span class="console-line">legal://read-before-use</span>
      <h1 class="site-h1">Privacy, terms &amp; disclaimer</h1>
      <p>The short version: this is experimental software. Keep your keys safe and your zaps small.</p>
    </header>

    <section class="legal-section">
      <h2>Privacy</h2>
      <p>
        Zapclub does not require an email account. Your Nostr public key is your identity, and your
        chosen signer handles private-key operations. Zapclub should never ask for your private key.
      </p>
      <p>
        Club activity, profiles, memberships, chat messages, playlists and reactions may be published
        as signed Nostr events. Nostr is a replicated public network: events can be copied, cached and
        retained by relays or clients outside Zapclub’s control. Do not publish anything you need to
        keep secret.
      </p>
      <p>
        The browser stores necessary local settings and session state. While an eligible club page is
        open, it sends a signed heartbeat under a temporary, club-specific browser-tab key that is not
        linked to your Nostr identity. The relay publishes only the aggregate live count and keeps a
        rolling 24-hour overview of anonymous listener sessions for the superadmin. Pseudonymized
        technical access logs may also be processed for security and operation. Zapclub does not use
        advertising trackers.
      </p>
      <p>
        YouTube, Nostr relays, Lightning wallets and LNURL services are independent third parties.
        Loading or using them can expose technical data such as your IP address under their own terms.
      </p>
    </section>

    <section class="legal-section">
      <h2>Terms of use</h2>
      <p>
        Use Zapclub lawfully and respectfully. You are responsible for the clubs you operate, the
        messages and playlists you publish, the media you reference and the rights required to use it.
        Do not use the service to distribute illegal content, abuse people or interfere with the system.
      </p>
      <p>
        Club hosts and moderators can manage access and remove participants. Access to a club or the
        service can be limited when needed for safety, abuse prevention, maintenance or legal reasons.
      </p>
      <p>
        Lightning payments are real and normally irreversible. Verify the recipient and amount before
        paying. Zapclub is not a bank, wallet, escrow service or financial adviser.
      </p>
    </section>

    <section class="legal-section">
      <h2>Disclaimer</h2>
      <p>
        Zapclub is an experimental music, technology, education and digital-art project. It coordinates
        social activity and third-party playback; it does not host the referenced media and is not a
        licensed music-streaming service.
      </p>
      <p>
        The software is provided “as is” and “as available”. Availability, synchronization, external
        content, relay delivery and payment services can fail. No guarantee is made that the service is
        uninterrupted, error-free or suitable for a particular purpose.
      </p>
      <p>
        To the fullest extent permitted by applicable law, the operators are not liable for losses or
        damages arising from use of the service, third-party systems, published content or irreversible
        payments. Mandatory statutory rights remain unaffected.
      </p>
    </section>

    <section class="legal-section contact">
      <h2>Questions or problems?</h2>
      <p>
        Open an issue in the
        <a href="https://github.com/satoshidude/zapclub.io" target="_blank" rel="noopener noreferrer">source repository</a>
        or contact the community through
        <a href="https://t.me/zapclub_io" target="_blank" rel="noopener noreferrer">Telegram</a>.
      </p>
    </section>

    <button class="back-link" onclick={goHome}>← Back to the clubs</button>
  </article>
</div>

<style>
  .legal-page {
    width: min(960px, 100%);
    margin: 0 auto;
    padding: 0.8rem 0.8rem 4rem;
    color: var(--lcd-text);
  }
  .legal-card {
    padding: clamp(1.15rem, 3vw, 2rem);
    border: 0;
    border-radius: 0;
  }
  .legal-hero {
    padding-bottom: 0.8rem;
  }
  .console-line {
    color: var(--accent);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 0.82rem;
    letter-spacing: 0.07em;
  }
  h2 {
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    text-shadow: var(--lcd-text-shadow);
  }
  h1 {
    margin: 0.35rem 0 0.7rem;
  }
  .legal-hero p {
    max-width: 66ch;
    font-size: 1.05rem;
  }
  .legal-section {
    margin-top: 1.6rem;
  }
  h2 {
    margin: 0 0 0.85rem;
    padding-bottom: 0.55rem;
    border-bottom: 1px solid rgba(241, 243, 244, 0.22);
    font-size: 1.25rem;
    letter-spacing: 0.04em;
  }
  p {
    max-width: 76ch;
    margin: 0 0 0.75rem;
    color: var(--lcd-text-soft);
    font-size: 0.94rem;
    line-height: 1.65;
  }
  p:last-child { margin-bottom: 0; }
  a,
  .back-link {
    color: var(--lcd-text-bright);
    font-family: 'DotGothic16', ui-monospace, monospace;
    text-decoration: none;
    text-shadow: var(--lcd-text-shadow);
  }
  a:hover,
  a:focus-visible,
  .back-link:hover,
  .back-link:focus-visible { color: var(--accent); }
  .back-link {
    min-height: 44px;
    margin-top: 1.1rem;
    padding: 1rem 0 0;
    border: 0;
    border-top: 1px solid rgba(241, 243, 244, 0.14);
    background: transparent;
  }
  @media (max-width: 560px) {
    .legal-page { padding: 0.45rem 0.45rem 3rem; }
    .legal-card { padding: 1rem; }
    .legal-section { margin-top: 1.35rem; }
  }
</style>

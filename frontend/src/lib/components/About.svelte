<script lang="ts">
  import { goHome } from '../router.svelte'
</script>

<!-- Product README: functions, signal path and vision. -->
<div class="info-page">
  <header class="page-hero led-zone">
    <span class="console-line">about://zapclub</span>
    <h1>One room. One clock. Everyone in sync.</h1>
    <p>
      Zapclub is a shared turntable for Nostr people. Join a club, listen to the same
      moment together, take a slot on stage and bring your own queue.
    </p>
  </header>

  <section class="info-module led-zone">
    <h2>What it does</h2>
    <div class="feature-grid">
      <article>
        <h3>Clubs, not feeds</h3>
        <p>Every club is a room with its own members, stage, chat, playlist flow and vibe.</p>
      </article>
      <article>
        <h3>A shared clock</h3>
        <p>The conductor keeps every listener near the same playback position.</p>
      </article>
      <article>
        <h3>Three DJ slots</h3>
        <p>DJs take turns track by track. No single queue owns the whole night.</p>
      </article>
      <article>
        <h3>Vibe Meter</h3>
        <p>The room can call a banger or skip. The meter turns reactions into live feedback.</p>
      </article>
      <article>
        <h3>Zaps, direct</h3>
        <p>NIP-57 sends sats to the DJ’s Lightning address instead of platform points.</p>
      </article>
      <article>
        <h3>Your key, your profile</h3>
        <p>Nostr provides identity, profiles and signed actions. There is no password account.</p>
      </article>
    </div>
  </section>

  <section class="info-module led-zone">
    <h2>How the signal moves</h2>
    <ol class="signal-path">
      <li><strong>Nostr key</strong><span>signs identity, membership, playlists and actions</span></li>
      <li><strong>Club relay</strong><span>stores events and enforces club roles</span></li>
      <li><strong>Conductor</strong><span>chooses the next track and publishes the shared clock</span></li>
      <li><strong>YouTube player</strong><span>plays the referenced media in each listener’s browser</span></li>
      <li><strong>Lightning</strong><span>routes real sats directly to the current DJ</span></li>
    </ol>
    <p class="tech-note">
      The interface is built with Svelte. The relay side uses Go, khatru and relay29.
      Nostr events are handled with nostr-tools; playback uses the YouTube IFrame API.
    </p>
  </section>

  <section class="vision led-zone">
    <span class="console-line">vision://open-dancefloor</span>
    <h2>Music rooms that belong to their communities.</h2>
    <p>
      The goal is a network of independent clubs that can move between relays, keep their
      social identity and pay artists or DJs without a platform wallet in the middle.
      Less recommendation machine, more people sharing a room and taking turns.
    </p>
  </section>

  <button class="back-link" onclick={goHome}>← Back to the clubs</button>
</div>

<style>
  .info-page {
    width: min(960px, 100%);
    margin: 0 auto;
    padding: 0.8rem 0.8rem 4rem;
    color: var(--lcd-text);
  }
  .page-hero,
  .info-module,
  .vision {
    margin-bottom: 0.7rem;
    padding: clamp(1.15rem, 3vw, 2rem);
    border: 0;
    border-radius: 0;
  }
  .page-hero {
    min-height: 240px;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
  }
  .console-line {
    color: var(--accent);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-size: 0.82rem;
    letter-spacing: 0.07em;
  }
  h1,
  h2,
  h3 {
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
    text-shadow: var(--lcd-text-shadow);
  }
  h1 {
    max-width: 720px;
    margin: 0.35rem 0 0.75rem;
    font-size: clamp(2.1rem, 6vw, var(--site-h1-max));
    line-height: 0.98;
    letter-spacing: -0.035em;
  }
  .page-hero > p {
    max-width: 64ch;
    margin: 0;
    color: var(--lcd-text-soft);
    font-size: clamp(1rem, 2vw, 1.15rem);
    line-height: 1.55;
  }
  h2 {
    margin: 0 0 1rem;
    padding-bottom: 0.55rem;
    border-bottom: 1px solid rgba(241, 243, 244, 0.22);
    font-size: 1.25rem;
    letter-spacing: 0.04em;
  }
  .feature-grid {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 1px;
    background: rgba(241, 243, 244, 0.14);
  }
  .feature-grid article {
    min-height: 142px;
    padding: 1rem;
    background: rgba(2, 5, 9, 0.76);
  }
  h3 {
    margin: 0 0 0.45rem;
    color: var(--lcd-text-bright);
    font-size: 1rem;
  }
  p {
    margin: 0;
    color: var(--lcd-text-soft);
    font-size: 0.94rem;
    line-height: 1.6;
  }
  .signal-path {
    margin: 0;
    padding: 0;
    list-style: none;
    counter-reset: signal;
  }
  .signal-path li {
    counter-increment: signal;
    display: grid;
    grid-template-columns: 2.3rem minmax(130px, 0.7fr) minmax(0, 1.3fr);
    gap: 0.8rem;
    align-items: baseline;
    padding: 0.68rem 0;
    border-bottom: 1px solid rgba(241, 243, 244, 0.13);
  }
  .signal-path li::before {
    content: counter(signal, decimal-leading-zero);
    color: var(--accent);
    font-family: 'DotGothic16', ui-monospace, monospace;
  }
  .signal-path strong {
    color: var(--lcd-text-bright);
    font-family: 'DotGothic16', ui-monospace, monospace;
    font-weight: 400;
  }
  .signal-path span {
    color: var(--lcd-text-soft);
    line-height: 1.45;
  }
  .tech-note {
    margin-top: 1rem;
    max-width: 74ch;
  }
  .vision {
    padding-block: clamp(1.5rem, 4vw, 2.8rem);
  }
  .vision h2 {
    max-width: 760px;
    margin: 0.4rem 0 0.8rem;
    padding: 0;
    border: 0;
    font-size: clamp(1.65rem, 4vw, 2.7rem);
    line-height: 1.08;
  }
  .vision p { max-width: 70ch; }
  .back-link {
    color: var(--lcd-text-bright);
    font-family: 'DotGothic16', ui-monospace, monospace;
    text-decoration: none;
    text-shadow: var(--lcd-text-shadow);
  }
  .back-link:hover,
  .back-link:focus-visible { color: var(--accent); }
  .back-link {
    min-height: 44px;
    padding: 0;
    border: 0;
    background: transparent;
  }
  @media (max-width: 720px) {
    .feature-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
  @media (max-width: 560px) {
    .info-page { padding: 0.45rem 0.45rem 3rem; }
    .page-hero,
    .info-module,
    .vision { margin-bottom: 0.55rem; padding: 1rem; }
    .page-hero { min-height: 210px; }
    .feature-grid { grid-template-columns: 1fr; }
    .feature-grid article { min-height: 0; }
    .signal-path li {
      grid-template-columns: 2rem minmax(0, 1fr);
      gap: 0.4rem;
    }
    .signal-path span { grid-column: 2; }
  }
</style>

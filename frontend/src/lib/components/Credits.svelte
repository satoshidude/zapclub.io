<script lang="ts">
  import { goHome } from '../router.svelte'
</script>

<div class="wrap">
  <header>
    <h1>Credits</h1>
    <p>Open protocols and open-source tools make Zapclub possible.</p>
  </header>

  <section>
    <h2>Protocols &amp; software</h2>
    <p>
      Zapclub is built with Nostr, Bitcoin Lightning, Svelte, nostr-tools,
      khatru and relay29. Synchronized playback uses the YouTube IFrame API.
    </p>
    <p>
      Source code and the complete dependency record are available in the
      <a href="https://github.com/satoshidude/zapclub.io" target="_blank" rel="noopener noreferrer">Zapclub repository</a>.
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
  header { margin-bottom: 1.2rem; }
  h1 { margin: 0; font-size: 1.6rem; }
  header p { margin: 0.3rem 0 0; color: var(--text-dim); line-height: 1.5; }
  section {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.1rem 1.2rem;
    margin-bottom: 1rem;
  }
  h2 { margin: 0 0 0.6rem; font-size: 1.05rem; }
  section p { margin: 0 0 0.6rem; font-size: 0.92rem; line-height: 1.55; }
  section p:last-child { margin-bottom: 0; }
  a { color: var(--accent); font-weight: 600; }
  .home {
    background: none;
    border: none;
    color: var(--accent);
    font-weight: 600;
    padding: 0;
    font-size: 0.92rem;
  }
  .home:hover { text-decoration: underline; }
</style>

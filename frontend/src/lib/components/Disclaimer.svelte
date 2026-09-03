<script lang="ts">
  import { goHome } from '../router.svelte'
</script>

<div class="wrap">
  <header>
    <h1>Disclaimer</h1>
    <p>Use Zapclub and its connected services at your own risk.</p>
  </header>

  <section>
    <h2>Experimental service</h2>
    <p>
      Zapclub is an experimental music, technology and digital-art project. It
      is provided without guarantees of availability, correctness or fitness
      for a particular purpose.
    </p>
  </section>

  <section>
    <h2>External media &amp; payments</h2>
    <p>
      External media, Nostr relays and Lightning services are operated by third
      parties. Zapclub does not control their content, availability or privacy
      practices. Lightning payments are irreversible; use only small amounts.
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
  p { margin: 0; font-size: 0.92rem; line-height: 1.55; }
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

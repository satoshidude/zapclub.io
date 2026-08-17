<script lang="ts">
  import { goHome } from '../router.svelte'
</script>

<div class="wrap">
  <header class="hh">
    <h1>Privacy Policy</h1>
  </header>

  <section class="card">
    <p>
      ZapClub is designed around decentralized identities and open protocols. Information may be
      processed through Nostr relays, Lightning infrastructure and other decentralized services.
    </p>
    <p>
      Users are responsible for understanding the privacy implications of the protocols they use.
      The project aims to minimize centralized data collection wherever technically feasible.
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
  .hh { margin-bottom: 1.2rem; }
  h1 { margin: 0; font-size: 1.6rem; }
  .card {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.1rem 1.2rem;
    margin-bottom: 1rem;
  }
  p {
    margin: 0 0 0.6rem;
    font-size: 0.92rem;
    line-height: 1.55;
    color: var(--text);
  }
  p:last-child { margin-bottom: 0; }
  .home {
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

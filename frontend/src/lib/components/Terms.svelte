<script lang="ts">
  import { goHome } from '../router.svelte'
</script>

<div class="wrap">
  <header class="hh">
    <h1>Terms of Service</h1>
  </header>

  <section class="card">
    <p>By using ZapClub, you agree to use the platform lawfully and responsibly.</p>
    <p>Users remain solely responsible for:</p>
    <ul class="list">
      <li>Content they publish</li>
      <li>Clubs they operate</li>
      <li>Payments they initiate</li>
      <li>Interactions with other users</li>
    </ul>
  </section>

  <section class="card">
    <p>Premium features may be modified or discontinued at any time.</p>
    <p>The service is provided without guarantees regarding availability or uptime.</p>
    <button class="home" onclick={goHome}>← Back to the clubs</button>
  </section>
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
    margin: 0 0 0.5rem;
    font-size: 0.92rem;
    line-height: 1.55;
    color: var(--text);
  }
  .list {
    margin: 0.2rem 0 0;
    padding-left: 1.2rem;
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }
  .list li {
    font-size: 0.92rem;
    color: var(--text);
  }
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

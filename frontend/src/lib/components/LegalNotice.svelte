<script lang="ts">
  import { goHome } from '../router.svelte'
</script>

<div class="wrap">
  <header class="hh">
    <h1>Legal Notice</h1>
  </header>

  <section class="card">
    <h2>Project Status</h2>
    <p>
      ZapClub is an experimental music, technology, education and digital art project.
      The platform serves as a research and development environment for testing decentralized
      communication systems, digital identity technologies, community-driven experiences and
      open payment networks.
    </p>
  </section>

  <section class="card">
    <h2>Nature of the Platform</h2>
    <p>ZapClub is not a music streaming service.</p>
    <p>
      The platform does not host, distribute or claim ownership of third-party music recordings,
      videos or other external media content.
    </p>
  </section>

  <section class="card">
    <h2>User Responsibility</h2>
    <p>Users are solely responsible for content they publish, share or reference through the platform.</p>
  </section>

  <section class="card">
    <h2>No Warranty</h2>
    <p>The platform is provided on an "AS IS" and "AS AVAILABLE" basis without warranties of any kind.</p>
  </section>

  <section class="card">
    <h2>Limitation of Liability</h2>
    <p>
      To the fullest extent permitted by applicable law, the operators shall not be liable for
      damages arising from the use of the platform.
    </p>
  </section>

  <section class="card">
    <h2>Lightning Payments</h2>
    <p>
      ZapClub may support peer-to-peer Lightning payments between users. The platform is not a
      bank, escrow service or financial institution.
    </p>
  </section>

  <section class="card">
    <h2>Contact</h2>
    <p>General inquiries may be submitted through the communication channels provided by the project.</p>
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
  h2 { margin: 0 0 0.5rem; font-size: 1.05rem; }
  p {
    margin: 0 0 0.5rem;
    font-size: 0.92rem;
    line-height: 1.55;
    color: var(--text);
  }
  p:last-child { margin-bottom: 0; }
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

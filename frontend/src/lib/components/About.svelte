<script lang="ts">
  import { goHome } from '../router.svelte'
</script>

<div class="wrap">
  <header class="hh">
    <h1>How zapclub works</h1>
    <p class="sub">
      A collaborative, decentralized music club — built entirely on Nostr and Lightning.
      No accounts, no central database, no tracking. Here's the technical picture.
    </p>
  </header>

  <section class="card">
    <h2>🪪 Your identity is a Nostr key</h2>
    <p>
      There is no sign-up and no password. You log in with a Nostr key — via a browser
      extension (<a href="https://nips.nostr.com/7" target="_blank" rel="noopener noreferrer">NIP-07</a>),
      a remote signer / bunker (<a href="https://nips.nostr.com/46" target="_blank" rel="noopener noreferrer">NIP-46</a>),
      a private key (<code>nsec</code>), or read-only. Your public key <em>is</em> your account.
    </p>
    <p>
      Your <strong>profile</strong> (name, picture, lightning address) is a kind <code>0</code>
      event that lives on the <strong>open Nostr network</strong>, not on zapclub. The same
      identity and profile work in every other Nostr app — zapclub keeps no user database.
    </p>
  </section>

  <section class="card">
    <h2>🏛️ Clubs are NIP-29 groups</h2>
    <p>
      A club is a <a href="https://nips.nostr.com/29" target="_blank" rel="noopener noreferrer">NIP-29</a>
      relay-based group on our own relay (<code>wss://relay.zapclub.io</code>, running
      khatru + relay29 in Go). The relay manages membership, roles and moderation:
    </p>
    <ul class="links">
      <li><code>9007</code> create a club · <code>9002</code> edit its metadata</li>
      <li><code>9021</code> join · <code>9022</code> leave · <code>9000</code>/<code>9001</code> add/remove members &amp; roles</li>
      <li><code>39000</code> metadata · <code>39001</code> admins · <code>39002</code> members — read directly from the relay</li>
    </ul>
    <p>
      The <strong>club directory is just a relay query</strong> (<code>{`{kinds:[39000]}`}</code>) — no
      separate index or database. Roles are owner, moderator, and DJ (taking a stage slot is a
      content event, not a relay role, so any member can step up).
    </p>
  </section>

  <section class="card">
    <h2>🎚️ The stage &amp; the round-robin</h2>
    <p>
      Up to five members can be DJs at once. Each DJ keeps a queue, and the tracks are
      interleaved round-robin so everyone gets a turn. The pieces are all Nostr events tagged
      to the club:
    </p>
    <ul class="links">
      <li><code>30102</code> stage — “I'm a DJ here” (with a join time, for ordering)</li>
      <li><code>30103</code> queue — a DJ's track list (one per DJ, replaceable)</li>
      <li><code>30100</code> now_playing — the single source of truth for the running track</li>
      <li><code>1313</code> play-log — shared rotation progress · <code>30107</code> skip request</li>
    </ul>
  </section>

  <section class="card">
    <h2>⏱️ The relay is the conductor</h2>
    <p>
      To keep everyone in sync, exactly <strong>one</strong> writer publishes <code>now_playing</code>
      per club — and that writer is the <strong>relay itself</strong>. A scheduler on the
      always-on relay reads the stage and queues, advances the round-robin by each track's
      duration, and signs the <code>now_playing</code> + play-log events with the relay's own key.
    </p>
    <p>
      This means a club keeps playing <strong>on its own</strong> — even if no one has the page
      open, the phone is locked, or a DJ's connection drops. Clients are pure listeners: they
      read <code>now_playing</code>, compute the playback position from the relay's clock
      (<code>started_at</code>) and continuously correct drift. Owners/moderators can skip via a
      role-validated request; the relay enacts it.
    </p>
  </section>

  <section class="card">
    <h2>🔊 Synchronized playback</h2>
    <p>
      Audio comes from the <strong>YouTube embed</strong> — nothing is hosted or re-streamed.
      Every client loads the same video and seeks to the same drift-corrected position, so the
      whole room hears the same thing at the same moment. When no DJ has a track queued, a lobby
      placeholder plays.
    </p>
  </section>

  <section class="card">
    <h2>⚡ Zaps — real sats, straight to the DJ</h2>
    <p>
      Instead of likes, listeners send <strong>Lightning zaps</strong>
      (<a href="https://nips.nostr.com/57" target="_blank" rel="noopener noreferrer">NIP-57</a>):
      a signed zap request (<code>9734</code>) produces a Lightning invoice; paying it yields a
      zap receipt (<code>9735</code>). The sats go <strong>directly to the DJ's wallet</strong> —
      zapclub takes no cut and never touches the money. A lightweight club broadcast
      (<code>20101</code>) animates the zap live for everyone in the room.
    </p>
  </section>

  <section class="card">
    <h2>👀 Presence &amp; chat</h2>
    <p>
      Live presence is an ephemeral heartbeat (<code>20100</code>) — it shows who's here right now
      but is never stored, so there's no listener tracking. Club chat is a simple group message
      (<code>9</code>) tagged to the club.
    </p>
  </section>

  <section class="card">
    <h2>🧭 Sovereignty by design</h2>
    <p>
      The only central component is the relay — and it's open source. There's no user database,
      no analytics, no platform lock-in. Your identity, profile and social graph are standard
      Nostr and work everywhere; the music stays on YouTube; the money flows peer-to-peer over
      Lightning. A club that genuinely belongs to the people in it.
    </p>
    <p class="note">
      Built with Nostr (NIP-01/07/29/42/57), khatru + relay29, nostr-tools and Svelte.
    </p>
    <button class="home" onclick={goHome}>← Back to the clubs</button>
  </section>
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
    margin: 0 0 0.5rem;
    font-size: 1.1rem;
  }
  p {
    margin: 0 0 0.6rem;
    font-size: 0.92rem;
    line-height: 1.55;
    color: var(--text);
  }
  .links {
    margin: 0.4rem 0;
    padding-left: 1.1rem;
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }
  .links li {
    font-size: 0.9rem;
    line-height: 1.45;
    color: var(--text-dim);
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
    color: var(--accent-2);
  }
  .note {
    margin: 0.6rem 0 0;
    font-size: 0.82rem;
    color: var(--text-dim);
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
  .home:hover {
    text-decoration: underline;
  }
</style>

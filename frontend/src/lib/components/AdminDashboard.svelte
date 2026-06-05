<script lang="ts">
  import { npubEncode } from 'nostr-tools/nip19'
  import { decode } from 'nostr-tools/nip19'
  import {
    isSuperadmin,
    loadAdminData,
    listBans,
    banPubkey,
    unbanPubkey,
    deleteClubAdmin,
    SUPERADMIN,
    type AdminClub,
  } from '../nostr/admin'
  import { goClub, goUser } from '../router.svelte'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'

  let clubs = $state<AdminClub[]>([])
  let bans = $state<Record<string, string>>({})
  let loading = $state(true)
  let error = $state('')
  let busy = $state('')

  // Manual ban field
  let banInput = $state('')
  let banReason = $state('')

  async function load() {
    loading = true
    error = ''
    try {
      const [c, b] = await Promise.all([loadAdminData(), listBans().catch(() => ({}))])
      clubs = c
      bans = b
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      loading = false
    }
  }

  function toHex(s: string): string {
    s = s.trim()
    if (/^[0-9a-f]{64}$/i.test(s)) return s.toLowerCase()
    try {
      const d = decode(s)
      if (d.type === 'npub') return d.data as string
    } catch {
      /* ignore */
    }
    return ''
  }

  async function doBan(pubkey: string, reason: string) {
    const hex = toHex(pubkey)
    if (!hex) {
      error = 'Invalid pubkey / npub'
      return
    }
    if (hex === SUPERADMIN) {
      error = 'Refusing to ban the superadmin.'
      return
    }
    if (!confirm(`Ban this user relay-wide and purge their events?\n\n${hex}`)) return
    busy = 'ban:' + hex
    error = ''
    try {
      const r = await banPubkey(hex, reason.trim())
      bans = { ...bans, [hex]: reason.trim() }
      banInput = ''
      banReason = ''
      alert(`Banned. Purged ${r.purged} event(s).`)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = ''
    }
  }

  async function doUnban(pubkey: string) {
    busy = 'unban:' + pubkey
    error = ''
    try {
      await unbanPubkey(pubkey)
      const next = { ...bans }
      delete next[pubkey]
      bans = next
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = ''
    }
  }

  async function doDeleteClub(c: AdminClub) {
    if (!confirm(`Delete club "${c.name}" and purge ALL its events?\n\nThis cannot be undone.`)) return
    busy = 'club:' + c.id
    error = ''
    try {
      const r = await deleteClubAdmin(c.id)
      clubs = clubs.filter((x) => x.id !== c.id)
      alert(`Club deleted. Purged ${r.purged} event(s).`)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = ''
    }
  }

  const bannedList = $derived(Object.entries(bans))

  $effect(() => {
    if (isSuperadmin()) void load()
    else loading = false
  })
</script>

<div class="wrap">
  {#if !isSuperadmin()}
    <div class="card denied">
      <h1>🔒 Admin</h1>
      <p>This area is restricted to the zapclub superadmin.</p>
    </div>
  {:else}
    <header class="ahead">
      <h1>⚙️ Superadmin</h1>
      <button class="btn btn-ghost btn-sm" onclick={load} disabled={loading}>↻ Refresh</button>
    </header>

    {#if error}<p class="err">⚠ {error}</p>{/if}

    <!-- Ban by npub/hex -->
    <section class="card ban-box">
      <h2>Ban a user</h2>
      <div class="ban-row">
        <input class="in" placeholder="npub1… or hex pubkey" bind:value={banInput} />
        <input class="in" placeholder="Reason (optional)" bind:value={banReason} />
        <button class="btn btn-danger btn-sm" onclick={() => doBan(banInput, banReason)} disabled={!banInput.trim()}>
          Ban
        </button>
      </div>
      <p class="hint">A banned pubkey can still read public clubs but can no longer write any event (join, DJ, chat). Their existing events are purged. Reversible below.</p>
    </section>

    <!-- Banned users -->
    <section class="card">
      <h2>Banned users <span class="count">{bannedList.length}</span></h2>
      {#if bannedList.length === 0}
        <p class="dim">No banned users.</p>
      {:else}
        <ul class="banlist">
          {#each bannedList as [pk, reason] (pk)}
            {@const p = useProfile(pk)}
            <li>
              <img class="av" src={avatarUrl(pk, p)} alt="" width="22" height="22" />
              <a class="who" href={`/user/${npubEncode(pk)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(pk)) }}>{displayName(pk, p)}</a>
              {#if reason}<span class="reason">“{reason}”</span>{/if}
              <button class="btn btn-ghost btn-sm" onclick={() => doUnban(pk)} disabled={busy === 'unban:' + pk}>Unban</button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <!-- All clubs -->
    <section>
      <h2 class="clubs-h">All clubs <span class="count">{clubs.length}</span></h2>
      {#if loading}
        <p class="dim">Loading…</p>
      {:else}
        {#each clubs as c (c.id)}
          <div class="card club">
            <div class="club-head">
              <button class="club-name" onclick={() => goClub(c.id)}>{c.name}</button>
              <code class="cid">{c.id}</code>
              <button class="btn btn-danger btn-sm del" onclick={() => doDeleteClub(c)} disabled={busy === 'club:' + c.id}>
                Delete club
              </button>
            </div>
            {#if c.about}<p class="about">{c.about}</p>{/if}
            <div class="facts">
              <span class="fact">{c.open ? 'open' : 'closed'}</span>
              <span class="fact">{c.isPublic ? 'public' : 'private'}</span>
              <span class="fact">👥 {c.memberCount} members</span>
              <span class="fact">🎧 {c.djs.length} on stage</span>
              {#if c.nowPlaying?.videoId}
                <span class="fact playing">▶ {c.nowPlaying.title}</span>
              {/if}
            </div>
            <div class="members">
              {#each c.members as m (m.pubkey)}
                {@const p = useProfile(m.pubkey)}
                {@const isOwner = c.owner === m.pubkey}
                <span class="member">
                  <img class="av sm" src={avatarUrl(m.pubkey, p)} alt="" width="18" height="18" />
                  <a class="who" href={`/user/${npubEncode(m.pubkey)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(m.pubkey)) }}>{displayName(m.pubkey, p)}</a>
                  {#if isOwner}<span class="role owner">owner</span>{/if}
                  {#each m.roles as r (r)}<span class="role">{r}</span>{/each}
                  {#if c.djs.includes(m.pubkey)}<span class="role dj">dj</span>{/if}
                  {#if bans[m.pubkey]}
                    <span class="role banned">banned</span>
                  {:else if m.pubkey !== SUPERADMIN}
                    <button class="ban-mini" title="Ban this user" onclick={() => doBan(m.pubkey, `club:${c.name}`)} disabled={busy === 'ban:' + m.pubkey}>ban</button>
                  {/if}
                </span>
              {/each}
            </div>
          </div>
        {/each}
      {/if}
    </section>
  {/if}
</div>

<style>
  .wrap {
    max-width: 820px;
    margin: 0 auto;
    padding: 1.4rem 1rem 5rem;
  }
  .ahead {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0;
  }
  h2 {
    font-size: 1.05rem;
    margin: 0 0 0.6rem;
  }
  .clubs-h {
    margin-top: 1.4rem;
  }
  .count {
    font-size: 0.8rem;
    color: var(--text-dim);
    font-weight: 600;
  }
  .card {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1rem;
    margin-bottom: 1rem;
  }
  .denied {
    text-align: center;
    padding: 2.5rem 1rem;
  }
  .dim {
    color: var(--text-dim);
  }
  .err {
    color: var(--danger);
    font-size: 0.86rem;
  }
  .hint {
    margin: 0.5rem 0 0;
    font-size: 0.74rem;
    color: var(--text-dim);
  }
  .in {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.7rem;
    color: var(--text);
    font-size: 0.85rem;
  }
  .ban-row {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .ban-row .in {
    flex: 1;
    min-width: 8rem;
  }
  .btn-danger {
    background: var(--danger);
    border: 1px solid var(--danger);
    color: #fff;
  }
  .btn-danger:hover:not(:disabled) {
    filter: brightness(1.1);
  }
  .banlist {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .banlist li {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .av {
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    flex: 0 0 auto;
  }
  .av.sm {
    width: 18px;
    height: 18px;
  }
  .who {
    color: var(--text);
    text-decoration: none;
    font-weight: 600;
    font-size: 0.85rem;
  }
  .who:hover {
    color: var(--accent-2);
  }
  .reason {
    color: var(--text-dim);
    font-size: 0.78rem;
    font-style: italic;
    flex: 1;
  }
  .club-head {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    flex-wrap: wrap;
  }
  .club-name {
    background: none;
    border: none;
    color: var(--text);
    font-weight: 700;
    font-size: 1.05rem;
    cursor: pointer;
    padding: 0;
  }
  .club-name:hover {
    color: var(--accent-2);
  }
  .cid {
    font-size: 0.68rem;
    color: var(--text-dim);
    font-family: ui-monospace, monospace;
  }
  .del {
    margin-left: auto;
  }
  .about {
    color: var(--text-dim);
    font-size: 0.84rem;
    margin: 0.4rem 0;
  }
  .facts {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem;
    margin: 0.5rem 0;
  }
  .fact {
    font-size: 0.72rem;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.1rem 0.5rem;
  }
  .fact.playing {
    color: var(--accent);
    border-color: var(--accent);
  }
  .members {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem 0.8rem;
    margin-top: 0.5rem;
    padding-top: 0.6rem;
    border-top: 1px solid var(--border);
  }
  .member {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
  }
  .role {
    font-size: 0.62rem;
    font-weight: 700;
    text-transform: uppercase;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 0 0.25rem;
  }
  .role.owner {
    color: var(--accent);
    border-color: var(--accent);
  }
  .role.dj {
    color: var(--amber);
    border-color: var(--amber);
  }
  .role.banned {
    color: var(--danger);
    border-color: var(--danger);
  }
  .ban-mini {
    font-size: 0.62rem;
    font-weight: 700;
    text-transform: uppercase;
    color: var(--danger);
    background: none;
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 0 0.3rem;
    cursor: pointer;
  }
  .ban-mini:hover:not(:disabled) {
    border-color: var(--danger);
  }
</style>

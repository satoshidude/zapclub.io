<script lang="ts">
  import {
    subscribeClub,
    joinClub,
    leaveClub,
    removeUser,
    addModerator,
    parseClubMetadata,
    parseMembers,
    parseAdmins,
  } from '../nostr/groups'
  import { goHome } from '../router.svelte'
  import { auth } from '../nostr/auth.svelte'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import type { Club, ClubMember } from '../nostr/types'

  let { groupId }: { groupId: string } = $props()

  let club = $state<Club | null>(null)
  let members = $state<ClubMember[]>([])
  let admins = $state<string[]>([])
  let busy = $state(false)
  let error = $state('')

  const owner = $derived(admins[0] ?? '')
  const isOwner = $derived(!!auth.pubkey && auth.pubkey === owner)
  const isMod = $derived(
    !!auth.pubkey && members.some((m) => m.pubkey === auth.pubkey && m.roles.includes('moderator')),
  )
  const isMember = $derived(!!auth.pubkey && members.some((m) => m.pubkey === auth.pubkey))
  const canModerate = $derived(isOwner || isMod)

  $effect(() => {
    // (re)subscribe whenever groupId changes
    const id = groupId
    club = null
    members = []
    admins = []
    const stop = subscribeClub(id, {
      onMeta: (ev) => (club = parseClubMetadata(ev)),
      onMembers: (ev) => (members = parseMembers(ev)),
      onAdmins: (ev) => (admins = parseAdmins(ev)),
    })
    return stop
  })

  async function doJoin() {
    busy = true
    error = ''
    try {
      await joinClub(groupId)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }

  async function doLeave() {
    busy = true
    error = ''
    try {
      await leaveClub(groupId)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }

  async function kick(pubkey: string) {
    error = ''
    try {
      await removeUser(groupId, pubkey)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    }
  }

  async function promote(pubkey: string) {
    error = ''
    try {
      await addModerator(groupId, pubkey)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    }
  }

  function roleLabel(m: ClubMember): string {
    if (m.pubkey === owner) return 'owner'
    if (m.roles.includes('moderator')) return 'mod'
    return ''
  }
</script>

<div class="wrap">
  <button class="back" onclick={goHome}>← All clubs</button>

  <header class="hero">
    <div class="pic" style:background-image={club?.picture ? `url(${club.picture})` : 'none'}>
      {#if !club?.picture}⚡{/if}
    </div>
    <div class="info">
      <h1>{club?.name ?? 'Loading…'}</h1>
      {#if club?.about}<p class="about">{club.about}</p>{/if}
      <div class="tags">
        {#if club?.open}<span class="tag">open</span>{/if}
        {#if club?.isPublic}<span class="tag">public</span>{/if}
        <span class="tag">{members.length} member{members.length === 1 ? '' : 's'}</span>
      </div>
    </div>
    <div class="actions">
      {#if auth.canSign}
        {#if isMember}
          <button class="btn btn-ghost btn-sm" onclick={doLeave} disabled={busy}>Leave</button>
        {:else}
          <button class="btn btn-primary btn-sm" onclick={doJoin} disabled={busy}>Join club</button>
        {/if}
      {/if}
    </div>
  </header>

  {#if error}<p class="err">⚠ {error}</p>{/if}

  <section class="members">
    <h2>Members</h2>
    {#if members.length === 0}
      <p class="dim">No members yet.</p>
    {:else}
      <ul>
        {#each members as m (m.pubkey)}
          {@const profile = useProfile(m.pubkey)}
          <li>
            <img class="avatar" src={avatarUrl(m.pubkey, profile)} alt="" width="30" height="30" />
            <span class="mname">{displayName(m.pubkey, profile)}</span>
            {#if roleLabel(m)}<span class="role">{roleLabel(m)}</span>{/if}
            {#if canModerate && m.pubkey !== owner && m.pubkey !== auth.pubkey}
              <span class="mod-actions">
                {#if isOwner && !m.roles.includes('moderator')}
                  <button class="mini" onclick={() => promote(m.pubkey)} title="Make moderator">+mod</button>
                {/if}
                <button class="mini danger" onclick={() => kick(m.pubkey)} title="Remove from club">kick</button>
              </span>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <p class="phase-note">
    🎧 The stage, synced playback and zaps land in the next phase.
  </p>
</div>

<style>
  .wrap {
    max-width: 680px;
    margin: 0 auto;
    padding: 1.2rem 1rem 4rem;
  }
  .back {
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    padding: 0;
    font-size: 0.85rem;
    margin-bottom: 1rem;
  }
  .hero {
    display: flex;
    gap: 1rem;
    align-items: flex-start;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.1rem;
  }
  .pic {
    width: 72px;
    height: 72px;
    flex: 0 0 72px;
    border-radius: 14px;
    background-color: var(--bg-elev-2);
    background-size: cover;
    background-position: center;
    display: grid;
    place-items: center;
    font-size: 2rem;
  }
  .info {
    flex: 1;
    min-width: 0;
  }
  h1 {
    margin: 0;
    font-size: 1.4rem;
  }
  .about {
    margin: 0.3rem 0 0;
    color: var(--text-dim);
    font-size: 0.9rem;
  }
  .tags {
    display: flex;
    gap: 0.4rem;
    margin-top: 0.6rem;
    flex-wrap: wrap;
  }
  .tag {
    font-size: 0.72rem;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.15rem 0.55rem;
  }
  .actions {
    flex: 0 0 auto;
  }
  .members {
    margin-top: 1.5rem;
  }
  .members h2 {
    font-size: 1.05rem;
  }
  .members ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .members li {
    display: flex;
    align-items: center;
    gap: 0.6rem;
  }
  .avatar {
    width: 30px;
    height: 30px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
  }
  .mname {
    font-size: 0.9rem;
    font-weight: 600;
  }
  .role {
    font-size: 0.68rem;
    color: var(--accent);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.1rem 0.45rem;
  }
  .mod-actions {
    margin-left: auto;
    display: flex;
    gap: 0.35rem;
  }
  .mini {
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: 7px;
    padding: 0.2rem 0.5rem;
    font-size: 0.72rem;
  }
  .mini:hover {
    border-color: var(--accent-2);
    color: var(--text);
  }
  .mini.danger:hover {
    border-color: var(--danger);
    color: var(--danger);
  }
  .err {
    color: var(--danger);
    font-size: 0.85rem;
  }
  .dim {
    color: var(--text-dim);
  }
  .phase-note {
    margin-top: 2rem;
    padding: 0.9rem;
    border: 1px dashed var(--border);
    border-radius: var(--radius);
    color: var(--text-dim);
    font-size: 0.85rem;
    text-align: center;
  }
</style>

<script lang="ts">
  import {
    subscribeClub,
    joinClub,
    leaveClub,
    removeUser,
    addModerator,
    deleteEvent,
    editClub,
    parseClubMetadata,
    parseMembers,
    parseAdmins,
  } from '../nostr/groups'
  import { goUser } from '../router.svelte'
  import { auth } from '../nostr/auth.svelte'
  import { launchLogin } from '../nostr/nostrLogin'
  import { npubEncode } from 'nostr-tools/nip19'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import {
    stage,
    ingestStage,
    ingestStageKick,
    setStageHost,
    clearStageView,
    joinStage,
    leaveStage,
    persistedStageGroup,
  } from '../nostr/stage.svelte'
  import { ingestQueue, queues, markPlayed, resetQueues } from '../nostr/queue.svelte'
  import { sync, ingestNowPlaying, conductorTick, onTrackEnded, onTrackError, resetSync, skipTrack, ingestSkipIntent, clearSkipIntent, canSkip } from '../nostr/sync.svelte'
  import { ingestChat, removeMessage, resetChat } from '../nostr/chat.svelte'
  import { subscribeZaps, resetZaps, zaps } from '../nostr/zaps.svelte'
  import { registerActiveClub } from '../nostr/miniplay.svelte'
  import Player from './club/Player.svelte'
  import Stage from './club/Stage.svelte'
  import Queue from './club/Queue.svelte'
  import NowPlaying from './club/NowPlaying.svelte'
  import ComingNext from './club/ComingNext.svelte'
  import ZapButton from './club/ZapButton.svelte'
  import Chat from './club/Chat.svelte'
  import { clubAvatar } from '../avatar'
  import type { Club, ClubMember } from '../nostr/types'

  let { groupId }: { groupId: string } = $props()

  let club = $state<Club | null>(null)
  let members = $state<ClubMember[]>([])
  let admins = $state<string[]>([])
  let busy = $state(false)
  let error = $state('')
  let stageResumed = false
  let tab = $state<'station' | 'chat'>('station')

  const owner = $derived(admins[0] ?? '')
  const isOwner = $derived(!!auth.pubkey && auth.pubkey === owner)
  const isMod = $derived(
    !!auth.pubkey && members.some((m) => m.pubkey === auth.pubkey && m.roles.includes('moderator')),
  )
  const isMember = $derived(!!auth.pubkey && members.some((m) => m.pubkey === auth.pubkey))
  const canModerate = $derived(isOwner || isMod)

  // Total sats zapped in this club so far — summed from the zaps received by the club's
  // stage DJs (receipts don't carry the club, so DJ totals are the available proxy).
  const clubZapTotal = $derived(stage.djs.reduce((sum, d) => sum + zaps.score(d.pubkey), 0))

  /** Is a pubkey an admin/moderator (allowed to kick from stage)? */
  function isModerator(pubkey: string): boolean {
    return (
      pubkey === owner ||
      members.some((m) => m.pubkey === pubkey && m.roles.includes('moderator'))
    )
  }

  $effect(() => {
    // (re)subscribe whenever groupId changes
    const id = groupId
    club = null
    members = []
    admins = []
    stageResumed = false
    const me = auth.pubkey
    const stop = subscribeClub(id, {
      onMeta: (ev) => (club = parseClubMetadata(ev)),
      onMembers: (ev) => (members = parseMembers(ev)),
      onAdmins: (ev) => (admins = parseAdmins(ev)),
      // Hijack protection: only accept now_playing from the current conductor (or until
      // a conductor is known) — a rogue client can't steer playback.
      onNowPlaying: (ev) => {
        if (!stage.conductor || ev.pubkey === stage.conductor) ingestNowPlaying(ev)
      },
      onStage: ingestStage,
      onStageKick: (ev) => {
        if (!isModerator(ev.pubkey)) return // only honor admin/mod kicks
        const kicked = ingestStageKick(ev)
        if (kicked && kicked === me && stage.isOnStage(me)) void leaveStage(id)
      },
      onQueue: ingestQueue,
      onSkip: ingestSkipIntent,
      onChat: ingestChat,
      onDeleteEvent: (ev) => {
        // Only honor deletions from an admin/moderator (or the author themselves).
        const target = ev.tags.find((t) => t[0] === 'e')?.[1]
        if (!target) return
        if (isModerator(ev.pubkey)) removeMessage(target)
      },
    })

    // Conductor tick: only the conductor acts inside conductorTick(). Touch queues so the
    // effect re-evaluates when queue lengths change (reactivity).
    const tick = setInterval(() => {
      void queues
      conductorTick(id)
    }, 8000)

    return () => {
      stop()
      clearInterval(tick)
      resetSync()
      // Only clear the stage DISPLAY — keep my own presence + heartbeat so navigating
      // doesn't drop me off the stage (WebKit). Full reset happens on logout.
      clearStageView()
      resetQueues()
      resetChat()
      resetZaps()
    }
  })

  // One zap-receipt (9735) subscription per club for everyone shown with a zap chip:
  // the stage DJs + the club owner. ZapButton instances only READ the score, they don't
  // open their own subscriptions (avoids N overlapping REQs per club).
  $effect(() => {
    const pks = [...new Set([...stage.djs.map((d) => d.pubkey), owner].filter(Boolean))]
    return subscribeZaps(pks)
  })

  // Owner (first admin) is the stage host = always conductor when on stage.
  $effect(() => {
    setStageHost(owner || null)
  })

  // This club is now the active audio source. The global mini-player keeps it playing
  // when the user navigates to other (non-club) pages — until they enter another club.
  $effect(() => {
    registerActiveClub(groupId, club?.name ?? '')
  })

  // Only the conductor writes now_playing, so it's the conductor that ENACTS a skip
  // requested by an owner/moderator (who may not be on stage). Validate the requester's
  // role here, match it to the running track's pos, then skip.
  $effect(() => {
    const intent = sync.skipIntent
    if (!intent) return
    if (auth.pubkey !== stage.conductor) return
    const authorized =
      intent.author === owner || isModerator(intent.author) || intent.author === stage.conductor
    const fresh = Date.now() - intent.at < 60_000
    if (authorized && fresh && sync.live && intent.pos === sync.live.pos) {
      clearSkipIntent()
      skipTrack(groupId)
    } else if (!sync.live || (sync.live && intent.pos !== sync.live.pos)) {
      clearSkipIntent() // stale request (track already moved on)
    }
  })

  // Reload-resume: if the user was on this club's stage before reload, rejoin.
  $effect(() => {
    if (stageResumed || !auth.canSign) return
    if (persistedStageGroup() !== groupId) return
    stageResumed = true
    void joinStage(groupId)
  })

  // When MY track is the live one, mark it as played (greyed out, out of rotation).
  $effect(() => {
    const np = sync.live
    if (np && np.dj === auth.pubkey && np.videoId) void markPlayed(groupId, np.videoId)
  })

  const onStageNow = $derived(stage.isOnStage(auth.pubkey))

  /** From the lobby "go on stage" link: hop on the stage and open the DJ Station tab. */
  function goOnStage() {
    if (!onStageNow) void joinStage(groupId)
    tab = 'station'
  }

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

  // Owner: edit the club (name / about / picture).
  let editing = $state(false)
  let eName = $state('')
  let eAbout = $state('')
  let ePic = $state('')
  function openEdit() {
    eName = club?.name ?? ''
    eAbout = club?.about ?? ''
    ePic = club?.picture ?? ''
    editing = true
  }
  async function saveEdit() {
    if (!eName.trim()) return
    error = ''
    try {
      await editClub(groupId, {
        name: eName.trim(),
        about: eAbout.trim() || undefined,
        picture: ePic.trim() || undefined,
      })
      editing = false
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    }
  }
</script>

<div class="wrap">
  <header class="hero">
    <div class="hero-top">
      <div class="pic">
        <img class="pic-img" src={club?.picture || clubAvatar(owner || groupId)} alt="" />
      </div>
      <div class="info">
        <h1>{club?.name ?? 'Loading…'}</h1>
        <div class="tags">
          <span class="tag">{members.length} member{members.length === 1 ? '' : 's'}</span>
          {#if owner}
            {@const op = useProfile(owner)}
            <a class="tag host" href={`/user/${npubEncode(owner)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(owner)) }}>
              <img class="host-av" src={avatarUrl(owner, op)} alt="" width="14" height="14" />
              {displayName(owner, op)}
            </a>
            <ZapButton pubkey={owner} />
          {/if}
          {#if clubZapTotal > 0}<span class="tag zaps">⚡ {clubZapTotal.toLocaleString()} sats</span>{/if}
        </div>
      </div>
      <div class="actions">
        {#if isOwner}
          <button class="btn btn-ghost btn-sm" onclick={openEdit} title="Edit club">✏️</button>
        {/if}
        {#if auth.canSign}
          {#if isMember}
            <button class="btn btn-ghost btn-sm" onclick={doLeave} disabled={busy}>Leave</button>
          {:else}
            <button class="btn btn-primary btn-sm" onclick={doJoin} disabled={busy}>Join club</button>
          {/if}
        {/if}
      </div>
    </div>

    {#if editing}
      <div class="edit-form">
        <div class="field">
          <label for="e-name">Club name</label>
          <input id="e-name" bind:value={eName} maxlength="60" />
        </div>
        <div class="field">
          <label for="e-about">About</label>
          <textarea id="e-about" bind:value={eAbout} rows="2" maxlength="280"></textarea>
        </div>
        <div class="field">
          <label for="e-pic">Image URL (leave empty for the generated one)</label>
          <input id="e-pic" bind:value={ePic} placeholder="https://…" />
        </div>
        <div class="edit-actions">
          <button class="btn btn-primary btn-sm" onclick={saveEdit} disabled={!eName.trim()}>Save</button>
          <button class="btn btn-ghost btn-sm" onclick={() => (editing = false)}>Cancel</button>
        </div>
      </div>
    {:else if club?.about}
      <p class="desc">{club.about}</p>
    {/if}

    <div class="hero-now">
      <NowPlaying
        onGoStage={goOnStage}
        stageLabel={isMember && auth.canSign ? (onStageNow ? 'Add a track →' : 'Go on stage →') : ''}
        clubId={groupId}
        clubName={club?.name ?? ''}
        canSkip={canSkip(canModerate)}
      />
    </div>
    <Player
      canHear={isMember}
      ctaText={isMember ? '' : auth.isLoggedIn ? 'Join to listen' : 'Sign in to listen'}
      onCta={() => {
        if (auth.isLoggedIn) void doJoin()
        else launchLogin()
      }}
      onended={() => onTrackEnded(groupId)}
      onerror={(vid) => onTrackError(groupId, vid)}
    />
    <ComingNext />
  </header>

  {#if error}<p class="err">⚠ {error}</p>{/if}

  <!-- Stage: always visible under the hero. -->
  <section class="stream">
    <Stage {groupId} {canModerate} {isMember} />
  </section>

  <div class="club-tabs" role="tablist">
    <button class="ctab" class:active={tab === 'station'} role="tab" aria-selected={tab === 'station'} onclick={() => (tab = 'station')}>
      🎛️ DJ Station
    </button>
    <button class="ctab" class:active={tab === 'chat'} role="tab" aria-selected={tab === 'chat'} onclick={() => (tab = 'chat')}>
      💬 Chat
    </button>
  </div>

  {#if tab === 'chat'}
    <div class="panel">
      <Chat
        {groupId}
        canChat={isMember}
        {canModerate}
        onauthor={(pubkey) => goUser(npubEncode(pubkey))}
        ondelete={(id) => void deleteEvent(groupId, id)}
      />

      <section class="members card">
        <details class="members-acc" open>
          <summary>
            <span class="sum-label">Members</span>
            <span class="mcount">{members.length}</span>
            <span class="chevron" aria-hidden="true">▾</span>
          </summary>
          {#if members.length === 0}
            <p class="dim">No members yet.</p>
          {:else}
            <ul class="member-list">
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
        </details>
      </section>
    </div>
  {:else}
    <div class="panel">
      {#if isMember}
        <Queue {groupId} />
      {:else}
        <section class="join-hint">Join the club to step on stage and queue tracks.</section>
      {/if}
    </div>
  {/if}

</div>

<style>
  .wrap {
    max-width: 680px;
    margin: 0 auto;
    padding: 1.2rem 1rem 4rem;
  }
  .hero {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.1rem;
  }
  .hero-top {
    display: flex;
    gap: 1rem;
    align-items: flex-start;
  }
  .hero-now {
    margin-top: 0.9rem;
  }
  .edit-form {
    margin-top: 0.9rem;
    padding-top: 0.9rem;
    border-top: 1px solid var(--border);
  }
  .edit-actions {
    display: flex;
    gap: 0.5rem;
  }
  .pic {
    width: 72px;
    height: 72px;
    flex: 0 0 72px;
    border-radius: 14px;
    overflow: hidden;
    background: var(--bg-elev-2);
  }
  .pic-img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .info {
    flex: 1;
    min-width: 0;
  }
  h1 {
    margin: 0;
    font-size: 1.4rem;
  }
  .tags {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    margin-top: 0.6rem;
    flex-wrap: nowrap;
    overflow-x: auto;
  }
  .tags > * {
    flex: 0 0 auto;
  }
  .tag {
    font-size: 0.72rem;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.15rem 0.55rem;
  }
  .tag.zaps {
    color: var(--amber);
    border-color: var(--amber);
    font-weight: 700;
  }
  .tag.host {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    text-decoration: none;
    cursor: pointer;
  }
  .tag.host:hover {
    border-color: var(--accent-2);
    color: var(--text);
  }
  .host-av {
    width: 14px;
    height: 14px;
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
  }
  .actions {
    flex: 0 0 auto;
  }
  .desc {
    margin: 0.9rem 0 0;
    font-size: 0.9rem;
    color: var(--text-dim);
    line-height: 1.6;
  }
  .members.card {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1rem;
  }
  .members-acc {
    margin: 0;
  }
  .members-acc summary {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    cursor: pointer;
    list-style: none;
    font-size: 0.9rem;
    font-weight: 600;
    user-select: none;
  }
  .members-acc summary::-webkit-details-marker {
    display: none;
  }
  .mcount {
    font-size: 0.72rem;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.05rem 0.5rem;
  }
  .chevron {
    margin-left: auto;
    color: var(--text-dim);
    transition: transform 0.18s ease;
  }
  .members-acc[open] .chevron {
    transform: rotate(180deg);
  }
  .member-list {
    list-style: none;
    margin: 0.8rem 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .member-list li {
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
  /* Player + now-playing + coming-up, always under the hero. */
  .stream {
    margin-top: 1.1rem;
    display: flex;
    flex-direction: column;
    gap: 0.9rem;
  }
  /* In-club tabs — underline style (no pills). */
  .club-tabs {
    display: flex;
    gap: 0.2rem;
    margin: 1.2rem 0 1rem;
    border-bottom: 1px solid var(--border);
  }
  .ctab {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    color: var(--text-dim);
    cursor: pointer;
    padding: 0.6rem 0.9rem;
    font-size: 0.92rem;
    font-weight: 600;
    transition: color 0.15s ease, border-color 0.15s ease;
  }
  .ctab:hover {
    color: var(--text);
  }
  .ctab.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }
  .panel {
    display: flex;
    flex-direction: column;
    gap: 0.9rem;
  }
  .join-hint {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.3rem;
    color: var(--text-dim);
    text-align: center;
    font-size: 0.9rem;
  }
</style>

<script lang="ts">
  import {
    subscribeClub,
    joinClub,
    leaveClub,
    removeUser,
    addModerator,
    addMember,
    fetchJoinRequests,
    deleteEvent,
    editClub,
    setClubConfig,
    parseClubConfig,
    parseClubMetadata,
    parseMembers,
    parseAdmins,
    parseOwner,
    shareNote,
  } from '../nostr/groups'
  import { untrack } from 'svelte'
  import { goUser } from '../router.svelte'
  import { auth } from '../nostr/auth.svelte'
  import { launchLogin } from '../nostr/nostrLogin'
  import { npubEncode, decode } from 'nostr-tools/nip19'
  import { ownPremium } from '../nostr/premium.svelte'
  import PremiumModal from './PremiumModal.svelte'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import {
    stage,
    ingestStage,
    ingestStageKick,
    clearStageView,
    seedStageFromCache,
    joinStage,
    leaveStage,
    persistedStageGroup,
  } from '../nostr/stage.svelte'
  import { ingestQueue, queues, resetQueues, startQueueSync, stopQueueSync, refreshQueues, reactivateMyQueue } from '../nostr/queue.svelte'
  import { ingestPlay, startPlayLogSync, stopPlayLogSync, refreshPlayLog, resetPlayLog } from '../nostr/playlog.svelte'
  import { sync, ingestNowPlaying, onTrackEnded, onTrackError, resetSync } from '../nostr/sync.svelte'
  import { ingestChat, removeMessage, resetChat } from '../nostr/chat.svelte'
  import { ingestEmote, resetEmotes } from '../nostr/emotes.svelte'
  import { ingestAutoDJ, ingestAutoCtrl, resetAutoDJ } from '../nostr/autodj.svelte'
  import { presence, ingestPresence, startPresence, stopPresence, resetPresence } from '../nostr/presence.svelte'
  import { subscribeZaps, resetZaps, ingestZapBroadcast, requestEntryInvoice, captureEntryReceipt } from '../nostr/zaps.svelte'
  import { showPay, markPaid } from '../nostr/payModal.svelte'
  import { registerActiveClub } from '../nostr/miniplay.svelte'
  import { CLUB_RELAY_PUBKEY } from '../nostr/pool'
  import type { Event } from 'nostr-tools/pure'
  import Queue from './club/Queue.svelte'
  import NowPlaying from './club/NowPlaying.svelte'
  import Dancefloor from './club/Dancefloor.svelte'
  import { clubAvatar } from '../avatar'
  import type { Club, ClubMember } from '../nostr/types'

  let { groupId }: { groupId: string } = $props()

  let club = $state<Club | null>(null)
  let members = $state<ClubMember[]>([])
  let admins = $state<string[]>([])
  let ownerPk = $state('')
  let busy = $state(false)
  let error = $state('')
  let stageResumed = false

  // Private-club state
  let requested = $state(false)        // user sent a join-request to a closed club
  let pendingRequests = $state<{ pubkey: string; createdAt: number }[]>([])
  let inviteNpub = $state('')
  let inviteError = $state('')
  let showPremModal = $state(false)

  // Owner = the 'owner'-role admin, NOT admins[0] (tag order isn't owner-first).
  const owner = $derived(ownerPk)
  const isOwner = $derived(!!auth.pubkey && auth.pubkey === owner)
  const isMod = $derived(
    !!auth.pubkey && members.some((m) => m.pubkey === auth.pubkey && m.roles.includes('moderator')),
  )
  const isMember = $derived(!!auth.pubkey && members.some((m) => m.pubkey === auth.pubkey))
  const canModerate = $derived(isOwner || isMod)

  // Access config (kind 30101) — keep the newest per author; only the OWNER's counts.
  let configEvs = $state<Record<string, Event>>({})
  const clubConfig = $derived.by(() => {
    const ev = owner ? configEvs[owner] : null
    return ev ? parseClubConfig(ev) : { access: 'open' as const, price: 0, lud16: '', zapper: '' }
  })
  const isPaid = $derived(clubConfig.access === 'paid')
  // Open clubs: everyone hears (guests included). Paid clubs: only members (who paid) hear.
  const canHear = $derived(!isPaid || isMember)


  /** Is a pubkey an admin/moderator (allowed to kick from stage)? */
  function isModerator(pubkey: string): boolean {
    return (
      pubkey === owner ||
      members.some((m) => m.pubkey === pubkey && m.roles.includes('moderator'))
    )
  }

  $effect(() => {
    const id = groupId
    console.log(`[zc:club] subscribe: ${id.slice(0, 8)} auth=${auth.pubkey?.slice(0, 8) ?? 'none'}`)
    club = null
    members = []
    admins = []
    configEvs = {}
    stageResumed = false
    const me = auth.pubkey
    untrack(() => seedStageFromCache(id))
    const stop = subscribeClub(id, {
      onMeta: (ev) => (club = parseClubMetadata(ev)),
      onMembers: (ev) => (members = parseMembers(ev)),
      onAdmins: (ev) => {
        admins = parseAdmins(ev)
        ownerPk = parseOwner(ev)
      },
      // The RELAY is the conductor: accept now_playing authored by the relay key. (Still accept
      // an on-stage DJ's now_playing as a transition fallback for any not-yet-migrated event;
      // a rogue non-DJ member can't steer playback.)
      onNowPlaying: (ev) => {
        const ok = ev.pubkey === CLUB_RELAY_PUBKEY || stage.djs.length === 0 || stage.isOnStage(ev.pubkey)
        if (!ok) { console.log(`[zc:club] onNowPlaying: drop non-conductor ${ev.pubkey.slice(0, 8)}`); return }
        ingestNowPlaying(ev)
      },
      onStage: ingestStage,
      onStageKick: (ev) => {
        if (!isModerator(ev.pubkey)) return // only honor admin/mod kicks
        const kicked = ingestStageKick(ev)
        if (kicked && kicked === me && stage.isOnStage(me)) void leaveStage(id)
      },
      onQueue: ingestQueue,
      onConfig: (ev) => {
        const prev = configEvs[ev.pubkey]
        if (!prev || ev.created_at >= prev.created_at) configEvs = { ...configEvs, [ev.pubkey]: ev }
      },
      onPresence: ingestPresence,
      onZapBroadcast: ingestZapBroadcast,
      onPlay: ingestPlay,
      onChat: ingestChat,
      onEmote: ingestEmote,
      onAutoDJ: ingestAutoDJ,
      onAutoDJCtrl: ingestAutoCtrl,
      onDeleteEvent: (ev) => {
        // Only honor deletions from an admin/moderator (or the author themselves).
        const target = ev.tags.find((t) => t[0] === 'e')?.[1]
        if (!target) return
        if (isModerator(ev.pubkey)) removeMessage(target)
      },
    })

    // Reliable round-robin preview: besides the live push subscription above, periodically
    // re-query all DJ queues so "Up next" stays correct even if a 30103 push was missed
    // (reconnect, relay restart). Idempotent ingest, so it never fights live updates / edits.
    startQueueSync(id)
    // Shared round-robin progress: keep the play-log (kind 1313) live so the "Up next" preview
    // mirrors what the relay (the conductor) will actually play.
    startPlayLogSync(id)

    return () => {
      console.log(`[zc:club] cleanup: ${id.slice(0, 8)}`)
      stop()
      stopQueueSync()
      stopPlayLogSync()
      resetSync()
      // Only clear the stage DISPLAY — keep my own presence + heartbeat so navigating
      // doesn't drop me off the stage (WebKit). Full reset happens on logout.
      // Pass the group we're leaving so my own entry is preserved on re-entry.
      clearStageView(id)
      resetQueues()
      resetPlayLog()
      resetChat()
      resetEmotes()
      resetZaps()
      resetPresence()
      resetAutoDJ()
    }
  })

  // Live presence: beat a heartbeat while I'm a member of this club so others see me online
  // (the relay rejects presence from non-members). Stops when I leave / am no longer a member.
  $effect(() => {
    if (isMember) startPresence(groupId)
    else stopPresence()
    return () => stopPresence()
  })

  // One zap-receipt (9735) subscription per club for everyone shown with a zap chip:
  // the stage DJs + the club owner. ZapButton instances only READ the score, they don't
  // open their own subscriptions (avoids N overlapping REQs per club).
  $effect(() => {
    const pks = [...new Set([...stage.djs.map((d) => d.pubkey), owner].filter(Boolean))]
    return subscribeZaps(pks)
  })

  // When the DJ roster changes (someone steps on/off stage), re-sync the queues right away
  // so a new DJ's playlist enters the round-robin immediately instead of waiting for the
  // next poll tick. Keyed on the DJ set so it only fires on actual roster changes.
  $effect(() => {
    const roster = stage.djs.map((d) => d.pubkey).join(',')
    void roster
    void refreshQueues(groupId)
    void refreshPlayLog(groupId) // so a takeover gets the shared played-set immediately
  })

  // This club is now the active audio source. The global mini-player keeps it playing
  // when the user navigates to other (non-club) pages — until they enter another club.
  $effect(() => {
    registerActiveClub(groupId, club?.name ?? '')
  })

  // Restore join-request state from localStorage on load (so "Request sent" persists across
  // page refreshes until the owner approves and the user appears in 39002).
  $effect(() => {
    const pk = auth.pubkey
    if (!pk) return
    const key = `zapclub:jreq:${groupId}:${pk}`
    if (isMember) {
      // Approved — clear the persisted request.
      try { localStorage.removeItem(key) } catch { /* ignore */ }
      requested = false
    } else if (club?.closed) {
      try { requested = !!localStorage.getItem(key) } catch { /* ignore */ }
    }
  })

  // Owner: load pending join-requests for invite-only clubs (refresh when member list changes).
  $effect(() => {
    if (!canModerate || !club?.closed) { pendingRequests = []; return }
    const memberPks = members.map((m) => m.pubkey)
    void fetchJoinRequests(groupId, memberPks).then((r) => (pendingRequests = r))
  })

  // Only the conductor writes now_playing, so it's the conductor that ENACTS a skip
  // requested by an owner/moderator (who may not be on stage). Validate the requester's
  // role here, match it to the running track's pos, then skip.
  // Reload-resume: if the user was on this club's stage before reload, rejoin.
  $effect(() => {
    if (stageResumed || !auth.canSign) return
    if (persistedStageGroup() !== groupId) return
    console.log(`[zc:club] reload-resume: joining ${groupId.slice(0, 8)}`)
    stageResumed = true
    void joinStage(groupId)
  })


  const onStageNow = $derived(stage.isOnStage(auth.pubkey))

  /** From the lobby "go on stage" link: hop on the stage; the round-robin interleaves my set. */
  function goOnStage() {
    if (!onStageNow) {
      void joinStage(groupId)
      void reactivateMyQueue(groupId) // bring my full set into the round-robin
    }
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

  /** Sends a join-request to an invite-only club (no auto-add; owner must approve). */
  async function doRequestJoin() {
    busy = true
    error = ''
    try {
      await joinClub(groupId)
      requested = true
      try {
        if (auth.pubkey) localStorage.setItem(`zapclub:jreq:${groupId}:${auth.pubkey}`, '1')
      } catch { /* ignore */ }
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }

  /** Owner approves a pending join-request (publishes NIP-29 kind 9000 put-user). */
  async function doApprove(pubkey: string) {
    error = ''
    try {
      await addMember(groupId, pubkey)
      pendingRequests = pendingRequests.filter((r) => r.pubkey !== pubkey)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    }
  }

  /** Owner invites a user by npub (decodes → hex → kind 9000 put-user). */
  async function doInvite() {
    inviteError = ''
    let hex = ''
    try {
      const decoded = decode(inviteNpub.trim())
      if (decoded.type !== 'npub') throw new Error('expected an npub')
      hex = decoded.data as string
    } catch {
      inviteError = 'Invalid npub — paste an npub1… key'
      return
    }
    try {
      await addMember(groupId, hex)
      inviteNpub = ''
    } catch (e) {
      inviteError = String((e as Error)?.message ?? e)
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

  // Share this club: copy the link, share via the OS sheet, post to a social network, or — with
  // an explicit extra confirm, since it publishes a public note — post it to Nostr.
  let shareOpen = $state(false)
  let shareMsg = $state('')
  let sharing = $state(false)
  let sharedAt = $state(0)
  let nostrConfirm = $state(false) // second-step confirm before the public Nostr post
  const SHARE_COOLDOWN_MS = 3_600_000 // don't re-post the same club to Nostr within an hour
  const shareKey = $derived(`zapclub:shared:${groupId}`)
  // Load the last-shared timestamp for this club (so the button reflects the cooldown).
  $effect(() => {
    try {
      sharedAt = Number(localStorage.getItem(shareKey) || 0)
    } catch {
      sharedAt = 0
    }
  })
  const sharedRecently = $derived(sharedAt > 0 && Date.now() - sharedAt < SHARE_COOLDOWN_MS)
  const clubUrl = $derived(`${location.origin}/club/${groupId}`)
  const shareText = $derived(`🎧 ${club?.name ?? 'A club'} on zapclub! Join the club and become a DJ - Just music, sats, and a crew with mighty playlists`)
  const canNativeShare = typeof navigator !== 'undefined' && typeof navigator.share === 'function'
  // Social share-intent links (open the network's own composer in a new tab — the user posts
  // there, we never post on their behalf). Not Nostr-only.
  const socials = $derived([
    { id: 'x', label: '𝕏 Share on X', url: `https://x.com/intent/post?text=${encodeURIComponent(shareText)}&url=${encodeURIComponent(clubUrl)}` },
    { id: 'tg', label: '✈️ Share on Telegram', url: `https://t.me/share/url?url=${encodeURIComponent(clubUrl)}&text=${encodeURIComponent(shareText)}` },
    { id: 'wa', label: '💬 Share on WhatsApp', url: `https://wa.me/?text=${encodeURIComponent(`${shareText} ${clubUrl}`)}` },
  ])
  function closeShare() {
    shareOpen = false
    nostrConfirm = false
    shareMsg = ''
  }
  function shareSocial(url: string) {
    window.open(url, '_blank', 'noopener,noreferrer')
    closeShare()
  }
  async function copyLink() {
    try {
      await navigator.clipboard.writeText(clubUrl)
      shareMsg = 'Link copied ✓'
      setTimeout(() => (shareMsg = ''), 1500)
    } catch {
      shareMsg = 'Copy failed'
    }
  }
  async function shareNative() {
    try {
      await navigator.share({ title: club?.name ?? 'zapclub', url: clubUrl })
    } catch {
      /* user cancelled */
    }
    closeShare()
  }
  // Step 1: a tap on "Share on Nostr" doesn't post — it asks for confirmation first (a public note).
  function askNostrShare() {
    let last = 0
    try { last = Number(localStorage.getItem(shareKey) || 0) } catch { /* ignore */ }
    if (Date.now() - last < SHARE_COOLDOWN_MS) {
      shareMsg = 'Already shared this club in the last hour'
      return
    }
    shareMsg = ''
    nostrConfirm = true
  }
  // Step 2: confirmed → publish the public Nostr note.
  async function confirmNostrShare() {
    if (sharing) return // guard a rapid double-click
    sharing = true
    try {
      await shareNote(`${shareText}\n${clubUrl}`, clubUrl)
      const now = Date.now()
      try { localStorage.setItem(shareKey, String(now)) } catch { /* ignore */ }
      sharedAt = now
      nostrConfirm = false
      shareMsg = 'Posted to Nostr ✓'
      setTimeout(() => { closeShare() }, 1400)
    } catch (e) {
      shareMsg = String((e as Error)?.message ?? 'Post failed')
    } finally {
      sharing = false
    }
  }

  // Owner: edit the club (name / about / picture / access / privacy).
  let editing = $state(false)
  let eName = $state('')
  let eAbout = $state('')
  let ePic = $state('')
  let eAccess = $state<'open' | 'paid'>('open')
  let ePrice = $state(21)
  let eLud16 = $state('')
  let ePrivate = $state(false)
  function openEdit() {
    eName = club?.name ?? ''
    eAbout = club?.about ?? ''
    ePic = club?.picture ?? ''
    eAccess = clubConfig.access
    ePrice = clubConfig.price || 21
    // Entry address defaults to the club config, else the owner's profile lightning address,
    // else the zapclub fallback.
    const ownerLud = (useProfile(owner)?.lud16 as string) || ''
    eLud16 = clubConfig.lud16 || ownerLud || 'zapclub@nsnip.io'
    ePrivate = !!(club?.closed)
    editing = true
  }
  async function saveEdit() {
    if (!eName.trim()) return
    error = ''
    try {
      await editClub(
        groupId,
        { name: eName.trim(), about: eAbout.trim() || undefined, picture: ePic.trim() || undefined },
        { isPrivate: ePrivate },
      )
      await setClubConfig(groupId, {
        access: eAccess,
        price: eAccess === 'paid' ? Math.max(1, Math.floor(ePrice)) : 0,
        lud16: eAccess === 'paid' ? eLud16.trim() : '',
      })
      editing = false
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    }
  }

  // Paid club: pay the entry fee, then join WITH the 9735 receipt as proof — the relay's
  // entry gate verifies it before admitting (so the gate can't be bypassed).
  async function doPaidJoin() {
    if (!auth.isLoggedIn) {
      launchLogin()
      return
    }
    if (!clubConfig.zapper) {
      error = "This club's entry address doesn't support Nostr zaps — ask the owner to use a zap-enabled one."
      return
    }
    busy = true
    error = ''
    try {
      const { invoice, verify } = await requestEntryInvoice(
        groupId,
        clubConfig.zapper,
        clubConfig.lud16,
        clubConfig.price,
      )
      showPay(invoice, clubConfig.price, `Enter ${club?.name ?? 'club'}`, { verify })
      busy = false
      // Wait for the receipt the LNURL server publishes, then join with it as proof.
      const receipt = await captureEntryReceipt(invoice, clubConfig.zapper)
      if (receipt) {
        await joinClub(groupId, receipt)
        markPaid()
      } else {
        error = 'Payment not detected — if you paid, tap Join again.'
      }
    } catch (e) {
      error = String((e as Error)?.message ?? e)
      busy = false
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
          {#if isPaid}<span class="tag paid">🔒 {clubConfig.price} sats entry</span>{/if}
          {#if club?.isPrivate}<span class="tag private">🔒 Private</span>{/if}
          {#if owner}
            {@const op = useProfile(owner)}
            <a class="tag host" href={`/user/${npubEncode(owner)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(owner)) }}>
              <img class="host-av" src={avatarUrl(owner, op)} alt="" width="14" height="14" />
              {displayName(owner, op)}
            </a>
          {/if}
          {#if presence.count > 0}
            <span class="tag live-count" title="People listening to the stream right now">🎧 {presence.count} listening</span>
          {/if}
        </div>
      </div>
      <div class="actions">
        <div class="action-btns">
          <div class="share-wrap">
            <button class="btn btn-ghost btn-sm" onclick={() => { if (shareOpen) closeShare(); else { shareOpen = true; shareMsg = '' } }} title="Share this club" aria-label="Share this club">↗</button>
            {#if shareOpen}
              <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
              <div class="share-backdrop" role="presentation" onclick={closeShare}></div>
              <div class="share-menu" role="menu">
                {#if nostrConfirm}
                  <div class="share-confirm">
                    <div class="share-confirm-q">Post this club publicly to Nostr?</div>
                    <div class="share-confirm-btns">
                      <button class="share-item confirm-yes" role="menuitem" onclick={confirmNostrShare} disabled={sharing}>
                        {sharing ? '⚡ Posting…' : '⚡ Post'}
                      </button>
                      <button class="share-item" role="menuitem" onclick={() => (nostrConfirm = false)} disabled={sharing}>Cancel</button>
                    </div>
                  </div>
                {:else}
                  <button class="share-item" role="menuitem" onclick={copyLink}>🔗 Copy link</button>
                  {#if canNativeShare}
                    <button class="share-item" role="menuitem" onclick={shareNative}>📤 Share…</button>
                  {/if}
                  {#each socials as s (s.id)}
                    <button class="share-item" role="menuitem" onclick={() => shareSocial(s.url)}>{s.label}</button>
                  {/each}
                  {#if auth.canSign}
                    <div class="share-sep"></div>
                    <button class="share-item" role="menuitem" onclick={askNostrShare} disabled={sharedRecently}>
                      {sharedRecently ? '✓ Shared on Nostr (within 1h)' : '⚡ Share on Nostr'}
                    </button>
                  {/if}
                {/if}
                {#if shareMsg}<div class="share-msg">{shareMsg}</div>{/if}
              </div>
            {/if}
          </div>
          {#if isOwner}
            <button class="btn btn-ghost btn-sm" onclick={openEdit} title="Edit club">✏️</button>
          {/if}
          {#if auth.canSign}
            {#if isMember}
              <button class="btn btn-ghost btn-sm" onclick={doLeave} disabled={busy}>Leave</button>
            {:else if club?.closed}
              {#if requested}
                <span class="badge-sent">Request sent</span>
              {:else}
                <button class="btn btn-primary btn-sm" onclick={doRequestJoin} disabled={busy}>Request to join</button>
              {/if}
            {:else if isPaid}
              <button class="btn btn-primary btn-sm" onclick={doPaidJoin} disabled={busy}>⚡ Join · {clubConfig.price} sats</button>
            {:else}
              <button class="btn btn-primary btn-sm" onclick={doJoin} disabled={busy}>Join club</button>
            {/if}
          {/if}
        </div>
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
        <div class="field">
          <label for="e-access">Access</label>
          <select id="e-access" bind:value={eAccess}>
            <option value="open">Open — anyone listens free; join to DJ/chat</option>
            <option value="paid">Paid — pay sats to enter</option>
          </select>
        </div>
        {#if eAccess === 'paid'}
          <div class="field">
            <label for="e-price">Entry price (sats)</label>
            <input id="e-price" type="number" min="1" inputmode="numeric" bind:value={ePrice} />
          </div>
          <div class="field">
            <label for="e-lud16">Entry lightning address (receives the entry fees)</label>
            <input id="e-lud16" bind:value={eLud16} placeholder="you@wallet.com" autocomplete="off" />
          </div>
          <p class="paid-note">Guests hear nothing until they pay. Defaults to your profile address.</p>
        {/if}
        <div class="field-row">
          {#if ownPremium.active}
            <label class="toggle-label">
              <input type="checkbox" bind:checked={ePrivate} />
              🔒 Private (invite-only, hidden from non-members)
            </label>
          {:else}
            <button class="toggle-upsell" onclick={() => (showPremModal = true)} title="Requires zapclub Premium">
              🔒 Private (invite-only) <span class="prem-tag">⚡ Premium</span>
            </button>
          {/if}
        </div>
        <div class="edit-actions">
          <button class="btn btn-primary btn-sm" onclick={saveEdit} disabled={!eName.trim()}>Save</button>
          <button class="btn btn-ghost btn-sm" onclick={() => (editing = false)}>Cancel</button>
        </div>
      </div>
    {:else if club?.about}
      <p class="desc">{club.about}</p>
    {/if}

    <!-- Owner panel for invite-only clubs: pending requests + invite by npub. -->
    {#if canModerate && club?.closed}
      <div class="invite-panel card">
        <h4>🔒 Private club management</h4>
        {#if pendingRequests.length > 0}
          <p class="panel-label">Pending requests ({pendingRequests.length})</p>
          <ul class="req-list">
            {#each pendingRequests as req (req.pubkey)}
              {@const rp = useProfile(req.pubkey)}
              <li class="req-row">
                <img class="req-av" src={avatarUrl(req.pubkey, rp)} alt="" width="24" height="24" />
                <span class="req-name">{displayName(req.pubkey, rp)}</span>
                <button class="btn btn-primary btn-sm" onclick={() => doApprove(req.pubkey)}>Approve</button>
                <button class="btn btn-ghost btn-sm" onclick={() => { pendingRequests = pendingRequests.filter((r) => r.pubkey !== req.pubkey) }}>Ignore</button>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="panel-empty">No pending requests.</p>
        {/if}
        <p class="panel-label">Invite by npub</p>
        <div class="invite-row">
          <input class="invite-input" bind:value={inviteNpub} placeholder="npub1…" autocomplete="off" spellcheck="false" />
          <button class="btn btn-primary btn-sm" onclick={doInvite} disabled={!inviteNpub.trim()}>Invite</button>
        </div>
        {#if inviteError}<p class="invite-err">{inviteError}</p>{/if}
      </div>
    {/if}
    <!-- Player lives inside the hero: no separate card, just a section divider. -->
    <div class="player-section">
      <NowPlaying
        onGoStage={goOnStage}
        stageLabel={isMember && auth.canSign ? (onStageNow ? 'Add a track →' : 'Enter stage →') : ''}
        clubId={groupId}
        clubName={club?.name ?? ''}
        canHear={canHear}
        ctaText={canHear ? '' : `⚡ Pay ${clubConfig.price} sats to enter`}
        onCta={doPaidJoin}
        onended={() => onTrackEnded(groupId)}
        onerror={(vid) => onTrackError(groupId, vid)}
      />
    </div>
  </header>

  {#if showPremModal}
    <PremiumModal onClose={() => (showPremModal = false)} />
  {/if}

  {#if error}<p class="err">⚠ {error}</p>{/if}

  <!-- The floor: DJs up front, crowd behind, chat. -->
  <Dancefloor
    {groupId}
    {members}
    canChat={isMember}
    {canModerate}
    {isOwner}
    {isMember}
    {owner}
    currentDj={sync.live?.dj ?? ''}
    onkick={kick}
    onpromote={promote}
    ondelete={(id) => void deleteEvent(groupId, id)}
  />

  <!-- The user's own live playlist for this club — feeds the round-robin. -->
  {#if isMember}
    <Queue {groupId} {canModerate} />
  {:else}
    <section class="join-hint">Join the club to step on stage and queue tracks.</section>
  {/if}

</div>

<style>
  .wrap {
    max-width: 680px;
    margin: 0 auto;
    padding: 1.2rem 1rem 4rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
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
  /* Installed PWA (standalone): the club hero is the first thing on screen with no browser
     chrome, so trim its height — smaller club image, name and padding — to give the player
     and stage more room. */
  @media (display-mode: standalone) {
    .hero {
      padding: 0.7rem 0.8rem;
    }
    .hero-top {
      gap: 0.7rem;
    }
    .pic {
      width: 44px;
      height: 44px;
      flex-basis: 44px;
      border-radius: 10px;
    }
    h1 {
      font-size: 1.05rem;
    }
    .tags {
      margin-top: 0.4rem;
    }
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
  .tag.paid {
    color: var(--amber);
    border-color: var(--amber);
    font-weight: 700;
  }
  .tag.private {
    color: var(--accent-2);
    border-color: var(--accent-2);
    font-weight: 700;
  }
  .badge-sent {
    font-size: 0.8rem;
    color: var(--text-dim);
    background: var(--bg-elev-2);
    border-radius: var(--radius-sm);
    padding: 0.3rem 0.6rem;
  }
  /* Private toggle in edit form */
  .field-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .toggle-label {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    font-size: 0.88rem;
    color: var(--text-dim);
    cursor: pointer;
    user-select: none;
  }
  .toggle-label input[type="checkbox"] {
    accent-color: var(--accent-2);
    cursor: pointer;
  }
  .toggle-upsell {
    background: none;
    border: none;
    font-size: 0.88rem;
    color: var(--text-dim);
    cursor: pointer;
    padding: 0;
    display: flex;
    align-items: center;
    gap: 0.4rem;
    opacity: 0.6;
  }
  .toggle-upsell:hover { opacity: 1; }
  .prem-tag {
    font-size: 0.75rem;
    color: var(--amber);
    background: color-mix(in srgb, var(--amber) 12%, transparent);
    border-radius: 4px;
    padding: 0.1rem 0.4rem;
  }
  /* Owner invite panel */
  .invite-panel {
    margin-top: 1rem;
  }
  .invite-panel h4 {
    margin: 0 0 0.8rem;
    font-size: 0.9rem;
  }
  .panel-label {
    font-size: 0.78rem;
    color: var(--text-dim);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    margin: 0 0 0.4rem;
  }
  .panel-empty {
    font-size: 0.83rem;
    color: var(--text-dim);
    margin: 0 0 0.8rem;
  }
  .req-list {
    list-style: none;
    padding: 0;
    margin: 0 0 0.9rem;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .req-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.85rem;
  }
  .req-av {
    border-radius: 50%;
    object-fit: cover;
    flex-shrink: 0;
  }
  .req-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .invite-row {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.4rem;
  }
  .invite-input {
    flex: 1;
    min-width: 0;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    padding: 0.4rem 0.6rem;
    font-size: 0.82rem;
    font-family: monospace;
  }
  .invite-input:focus { outline: none; border-color: var(--accent-2); }
  .invite-err {
    font-size: 0.8rem;
    color: var(--danger);
    margin: 0;
  }
  .tag.live-count {
    border: none;
    color: var(--text-dim);
    padding-left: 0.2rem;
  }
  .edit-form .field select {
    width: 100%;
    background: var(--bg);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.6rem;
    font-size: 0.9rem;
  }
  .paid-note {
    margin: 0.2rem 0 0;
    font-size: 0.78rem;
    color: var(--text-dim);
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
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 0.45rem;
  }
  .action-btns {
    display: flex;
    gap: 0.4rem;
  }
  .share-wrap {
    position: relative;
  }
  .share-backdrop {
    position: fixed;
    inset: 0;
    z-index: 30;
  }
  .share-menu {
    position: absolute;
    top: calc(100% + 0.4rem);
    right: 0;
    z-index: 31;
    min-width: 168px;
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    padding: 0.35rem;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    box-shadow: 0 12px 30px rgba(0, 0, 0, 0.5);
  }
  .share-item {
    text-align: left;
    background: none;
    border: none;
    color: var(--text);
    font-size: 0.85rem;
    padding: 0.5rem 0.6rem;
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .share-item:hover:not(:disabled) {
    background: var(--bg);
    color: var(--accent);
  }
  .share-item:disabled {
    opacity: 0.55;
    cursor: default;
    color: var(--text-dim);
  }
  .share-msg {
    font-size: 0.72rem;
    color: var(--accent);
    padding: 0.3rem 0.6rem;
  }
  .share-sep {
    height: 1px;
    margin: 0.2rem 0.3rem;
    background: var(--border);
  }
  .share-confirm {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
    padding: 0.2rem;
  }
  .share-confirm-q {
    font-size: 0.8rem;
    color: var(--text);
    padding: 0.3rem 0.4rem 0.1rem;
    line-height: 1.4;
  }
  .share-confirm-btns {
    display: flex;
    gap: 0.3rem;
  }
  .share-confirm-btns .share-item {
    flex: 1;
    text-align: center;
    border: 1px solid var(--border);
  }
  .share-item.confirm-yes {
    color: var(--amber);
    border-color: var(--amber);
  }
  .share-item.confirm-yes:hover:not(:disabled) {
    background: var(--amber);
    color: #07070a;
  }
  .desc {
    margin: 0.9rem 0 0;
    font-size: 0.9rem;
    color: var(--text-dim);
    line-height: 1.6;
  }
  .err {
    color: var(--danger);
    font-size: 0.85rem;
  }
  /* Player inside the hero — divider separates it from club info. */
  .player-section {
    margin-top: 0.9rem;
    padding-top: 0.9rem;
    border-top: 1px solid var(--border);
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

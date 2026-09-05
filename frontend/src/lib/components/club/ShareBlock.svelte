<script lang="ts">
  import { auth } from '../../nostr/auth.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { signEvent } from '../../nostr/nostrLogin'
  import { pool, PROFILE_RELAYS } from '../../nostr/pool'

  const now = () => Math.floor(Date.now() / 1000)

  let { clubId, clubName, embedded = false }: { clubId: string; clubName: string; embedded?: boolean } = $props()

  type Target = 'nostr' | 'x'
  let target = $state<Target>('nostr')
  let sharing = $state(false)
  let shared = $state(false)
  let confirmNostr = $state(false)

  const shareText = $derived(`🎧 Listening in "${clubName}" on zapclub.io`)
  const shareUrl = $derived(`https://zapclub.io/club/${clubId}`)
  const fullText = $derived(`${shareText}\n\n${shareUrl}`)

  // Current user profile for preview
  const myProfile = $derived(auth.pubkey ? useProfile(auth.pubkey) : null)
  const myName = $derived(auth.pubkey ? displayName(auth.pubkey, myProfile) : 'You')
  const myAvatar = $derived(auth.pubkey ? avatarUrl(auth.pubkey, myProfile) : '')
  const myHandle = $derived(
    auth.pubkey ? '@' + (myProfile?.name || auth.pubkey.slice(0, 8) + '…') : '@you',
  )

  async function shareNostr() {
    if (!auth.pubkey || sharing) return
    sharing = true
    try {
      const signed = await signEvent({ kind: 1, content: fullText, tags: [], created_at: now() })
      await Promise.allSettled(pool.publish(PROFILE_RELAYS, signed))
      shared = true
      setTimeout(() => (shared = false), 3000)
    } catch (e) {
      console.error('[share] nostr:', e)
    } finally {
      sharing = false
    }
  }

  function shareX() {
    const url = `https://twitter.com/intent/tweet?text=${encodeURIComponent(fullText)}`
    window.open(url, '_blank', 'noopener')
  }

  function doShare() {
    if (target === 'nostr') confirmNostr = true
    else shareX()
  }

  async function confirmAndShare() {
    confirmNostr = false
    await shareNostr()
  }
</script>

<div class="share-block" class:embedded>
  <div class="share-head">
    <span class="share-label">Share</span>
    <div class="toggle">
      <button class:active={target === 'nostr'} onclick={() => (target = 'nostr')} title="Nostr">
        <img src="/nostrich.png" alt="Nostr" class="nostrich-icon" width="32" height="32" />
      </button>
      <button class:active={target === 'x'} onclick={() => (target = 'x')} title="X">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor">
          <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-4.714-6.231-5.402 6.23H2.744l7.73-8.835L1.254 2.25H8.08l4.253 5.622zm-1.161 17.52h1.833L7.084 4.126H5.117z"/>
        </svg>
      </button>
    </div>
  </div>

  <!-- Preview card -->
  <div class="preview" class:preview-x={target === 'x'} class:preview-nostr={target === 'nostr'}>
    <div class="post-head">
      <img class="av" src={myAvatar} alt="" width="32" height="32" />
      <div class="post-meta">
        <span class="post-name">{myName}</span>
        {#if target === 'x'}
          <span class="post-handle">{myHandle}</span>
        {:else}
          <span class="post-handle">{auth.pubkey ? auth.pubkey.slice(0, 16) + '…' : ''}</span>
        {/if}
      </div>
      {#if target === 'x'}
        <svg class="platform-icon" width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
          <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-4.714-6.231-5.402 6.23H2.744l7.73-8.835L1.254 2.25H8.08l4.253 5.622zm-1.161 17.52h1.833L7.084 4.126H5.117z"/>
        </svg>
      {:else}
        <img src="/nostrich.png" alt="Nostr" class="platform-icon nostrich-preview" width="32" height="32" />
      {/if}
    </div>
    <p class="post-text">{shareText}</p>
    <div class="og-card">
      <img src="/og.png" alt="zapclub.io" class="og-thumb" />
      <div class="og-meta">
        <span class="og-domain">zapclub.io</span>
        <span class="og-title">{clubName}</span>
      </div>
    </div>
  </div>

  {#if confirmNostr}
    <div class="confirm-box">
      <p class="confirm-q">Post this publicly to Nostr?</p>
      <div class="confirm-btns">
        <button class="share-btn nostr" onclick={confirmAndShare} disabled={sharing}>
          {sharing ? 'Posting…' : 'Yes, post'}
        </button>
        <button class="share-btn cancel" onclick={() => (confirmNostr = false)}>Cancel</button>
      </div>
    </div>
  {:else}
    <button
      class="share-btn"
      class:nostr={target === 'nostr'}
      class:x={target === 'x'}
      onclick={doShare}
      disabled={sharing || (!auth.pubkey && target === 'nostr')}
    >
      {#if shared}
        ✓ Shared
      {:else if sharing}
        Sharing…
      {:else if target === 'nostr'}
        Post to Nostr
      {:else}
        Post to X
      {/if}
    </button>
  {/if}

</div>

<style>
  .share-block {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.65rem;
  }
  .share-block.embedded {
    background: transparent;
    border: 0;
    border-radius: 0;
    padding: 0;
  }

  .share-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .share-label {
    font-size: 0.72rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
  }

  .toggle {
    display: flex;
    gap: 0.25rem;
    background: var(--bg);
    border-radius: 6px;
    padding: 2px;
  }
  .toggle button {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0.25rem 0.4rem;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.15s;
  }
  .toggle button.active {
    background: var(--bg-elev);
    color: var(--text);
  }
  .nostrich-icon {
    width: 32px;
    height: 32px;
    object-fit: contain;
    opacity: 0.45;
    transition: opacity 0.15s;
  }
  .toggle button.active .nostrich-icon {
    opacity: 1;
  }

  /* Preview card */
  .preview {
    border-radius: 8px;
    padding: 0.65rem 0.7rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    font-size: 0.78rem;
    border: 1px solid transparent;
  }
  .preview-x {
    background: #0f1923;
    border-color: #2f3640;
  }
  .preview-nostr {
    background: #160d2a;
    border-color: #3b206a;
  }

  .post-head {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .av {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    object-fit: cover;
    flex-shrink: 0;
    background: var(--bg);
  }
  .post-meta {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }
  .post-name {
    font-weight: 600;
    font-size: 0.78rem;
    color: #f0f0f0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .post-handle {
    font-size: 0.68rem;
    color: #666;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .platform-icon {
    flex-shrink: 0;
    color: #aaa;
  }
  .nostrich-preview {
    width: 32px;
    height: 32px;
    object-fit: contain;
  }

  .post-text {
    margin: 0;
    color: #d4d4d4;
    line-height: 1.45;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .og-card {
    border-radius: 6px;
    overflow: hidden;
    border: 1px solid #2a2a3a;
    display: flex;
    flex-direction: column;
  }
  .og-thumb {
    width: 100%;
    height: auto;
    max-height: 120px;
    object-fit: contain;
    display: block;
    background: #111;
  }
  .og-meta {
    padding: 0.35rem 0.5rem;
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    background: #111;
  }
  .og-domain {
    font-size: 0.65rem;
    color: #666;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .og-title {
    font-size: 0.72rem;
    color: #ccc;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* Share button */
  .share-btn {
    width: 100%;
    padding: 0.4rem;
    border: none;
    border-radius: 6px;
    font-size: 0.78rem;
    font-weight: 600;
    cursor: pointer;
    transition: opacity 0.15s;
  }
  .share-btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .share-btn.nostr {
    background: #6d28d9;
    color: #fff;
  }
  .share-btn.nostr:hover:not(:disabled) {
    background: #7c3aed;
  }
  .share-btn.x {
    background: #1a1a1a;
    color: #fff;
    border: 1px solid #333;
  }
  .share-btn.x:hover:not(:disabled) {
    background: #222;
  }
  .confirm-box {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .confirm-q {
    margin: 0;
    font-size: 0.78rem;
    color: var(--text-dim);
    text-align: center;
  }
  .confirm-btns {
    display: flex;
    gap: 0.4rem;
  }
  .confirm-btns .share-btn {
    flex: 1;
  }
  .share-btn.cancel {
    background: var(--bg-elev-2, #2a2a2a);
    color: var(--text-dim);
    border: 1px solid var(--border);
  }
  .share-btn.cancel:hover {
    opacity: 0.85;
  }
</style>

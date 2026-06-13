<script lang="ts">
  import { auth } from '../../nostr/auth.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { sync } from '../../nostr/sync.svelte'
  import { signEvent } from '../../nostr/nostrLogin'
  import { pool, PROFILE_RELAYS } from '../../nostr/pool'

  const now = () => Math.floor(Date.now() / 1000)

  let { clubId, clubName }: { clubId: string; clubName: string } = $props()

  type Target = 'nostr' | 'x'
  let target = $state<Target>('nostr')
  let sharing = $state(false)
  let shared = $state(false)

  const np = $derived(sync.nowPlaying)
  const thumbUrl = $derived(
    np?.videoId ? `https://img.youtube.com/vi/${np.videoId}/mqdefault.jpg` : null,
  )
  const shareText = $derived(
    np
      ? `🎵 ${np.title}\n\nListening in "${clubName}" on zapclub.io`
      : `🎧 Listening in "${clubName}" on zapclub.io`,
  )
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
    if (target === 'nostr') shareNostr()
    else shareX()
  }
</script>

<div class="share-block">
  <div class="share-head">
    <span class="share-label">Share</span>
    <div class="toggle">
      <button class:active={target === 'nostr'} onclick={() => (target = 'nostr')}>
        <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor">
          <path d="M20.5 3.5a4.5 4.5 0 0 0-6.37 0L12 5.63 9.87 3.5A4.5 4.5 0 0 0 3.5 9.87L12 18.37l8.5-8.5a4.5 4.5 0 0 0 0-6.37z"/>
        </svg>
        Nostr
      </button>
      <button class:active={target === 'x'} onclick={() => (target = 'x')}>
        <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor">
          <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-4.714-6.231-5.402 6.23H2.744l7.73-8.835L1.254 2.25H8.08l4.253 5.622zm-1.161 17.52h1.833L7.084 4.126H5.117z"/>
        </svg>
        X
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
        <svg class="platform-icon nostr-icon" width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
          <path d="M20.5 3.5a4.5 4.5 0 0 0-6.37 0L12 5.63 9.87 3.5A4.5 4.5 0 0 0 3.5 9.87L12 18.37l8.5-8.5a4.5 4.5 0 0 0 0-6.37z"/>
        </svg>
      {/if}
    </div>
    <p class="post-text">{shareText}</p>
    {#if thumbUrl}
      <div class="og-card">
        <img src={thumbUrl} alt="track thumbnail" class="og-thumb" />
        <div class="og-meta">
          <span class="og-domain">zapclub.io</span>
          <span class="og-title">{np?.title ?? clubName}</span>
        </div>
      </div>
    {:else}
      <div class="og-card og-text-only">
        <span class="og-domain">zapclub.io</span>
        <span class="og-title">{clubName}</span>
      </div>
    {/if}
  </div>

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
    gap: 0.3rem;
    padding: 0.2rem 0.5rem;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--text-muted);
    font-size: 0.72rem;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.15s;
  }
  .toggle button.active {
    background: var(--bg-elev);
    color: var(--text);
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
  .nostr-icon {
    color: #9333ea;
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
    height: 80px;
    object-fit: cover;
    display: block;
  }
  .og-meta {
    padding: 0.35rem 0.5rem;
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    background: #111;
  }
  .og-text-only {
    padding: 0.4rem 0.6rem;
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
</style>

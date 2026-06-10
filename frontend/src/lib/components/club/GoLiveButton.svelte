<script lang="ts">
  import { auth } from '../../nostr/auth.svelte'
  import { stage } from '../../nostr/stage.svelte'
  import { liveSession, goLive, endLive, type LiveMode, type LiveMedia } from '../../nostr/livesession.svelte'
  import { connectLivekit, type LivekitClient } from '../../player/livekit'

  interface Props {
    groupId: string
    isOwnerOrMod?: boolean
  }
  let { groupId, isOwnerOrMod = false }: Props = $props()

  const onStage = $derived(stage.isOnStage(auth.pubkey))
  const canGoLive = $derived(onStage || isOwnerOrMod)
  const session = $derived(liveSession.current)
  const iAmLive = $derived(session?.dj === auth.pubkey)

  type Step = 'idle' | 'picking' | 'connecting' | 'live' | 'error'
  let step = $state<Step>('idle')
  let mode = $state<LiveMode>('takeover')
  let media = $state<LiveMedia>('av')
  let err = $state('')
  let client = $state<LivekitClient | null>(null)

  // If someone else ended the live session while we were "live", reset.
  $effect(() => {
    if (step === 'live' && !iAmLive) {
      step = 'idle'
      if (client) { void client.disconnect(); client = null }
    }
  })

  async function startLive() {
    step = 'connecting'
    err = ''
    try {
      await goLive(groupId, mode, media)
      const c = await connectLivekit(groupId)
      await c.publishLocal({ video: media === 'av' })
      client = c
      step = 'live'
    } catch (e) {
      err = String((e as Error)?.message ?? e)
      step = 'error'
      if (client) { void client.disconnect(); client = null }
    }
  }

  async function stopLive() {
    await endLive(groupId)
    if (client) { void client.disconnect(); client = null }
    step = 'idle'
  }

  function cancel() {
    step = 'idle'
    err = ''
  }
</script>

{#if canGoLive}
  {#if step === 'idle'}
    <button class="btn btn-live" onclick={() => (step = 'picking')} disabled={!!session && !iAmLive}>
      ● Go Live
    </button>
  {:else if step === 'picking'}
    <div class="picker">
      <div class="picker-row">
        <label>
          <input type="radio" bind:group={mode} value="takeover" />
          <span>Takeover <span class="hint">YT pauses</span></span>
        </label>
        <label>
          <input type="radio" bind:group={mode} value="talkover" />
          <span>Talk over <span class="hint">YT ducks</span></span>
        </label>
      </div>
      <div class="picker-row">
        <label>
          <input type="radio" bind:group={media} value="audio" />
          <span>Audio only</span>
        </label>
        <label>
          <input type="radio" bind:group={media} value="av" />
          <span>Audio + Video</span>
        </label>
      </div>
      <div class="picker-actions">
        <button class="btn btn-live" onclick={startLive}>Go Live</button>
        <button class="btn btn-ghost btn-sm" onclick={cancel}>Cancel</button>
      </div>
    </div>
  {:else if step === 'connecting'}
    <button class="btn btn-live" disabled>Connecting…</button>
  {:else if step === 'live'}
    <button class="btn btn-live btn-live-on" onclick={stopLive}>
      ● Live — Go offline
    </button>
  {:else if step === 'error'}
    <div class="live-err">{err}</div>
    <button class="btn btn-ghost btn-sm" onclick={cancel}>Dismiss</button>
  {/if}
{/if}

<style>
  .btn-live {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.35rem 0.8rem;
    background: rgba(220, 38, 38, 0.15);
    border: 1px solid rgba(220, 38, 38, 0.5);
    border-radius: 999px;
    color: #ef4444;
    font-size: 0.8rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }
  .btn-live:hover:not(:disabled) {
    background: rgba(220, 38, 38, 0.28);
    border-color: #ef4444;
  }
  .btn-live:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .btn-live-on {
    background: rgba(220, 38, 38, 0.35);
    border-color: #ef4444;
    animation: live-pulse 2s ease-in-out infinite;
  }
  @keyframes live-pulse {
    0%, 100% { box-shadow: 0 0 0 0 rgba(239, 68, 68, 0.4); }
    50% { box-shadow: 0 0 0 6px rgba(239, 68, 68, 0); }
  }
  @media (prefers-reduced-motion: reduce) {
    .btn-live-on { animation: none; }
  }
  .picker {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm, 6px);
    padding: 0.7rem 0.9rem;
    font-size: 0.82rem;
  }
  .picker-row {
    display: flex;
    gap: 1rem;
    align-items: center;
  }
  label {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    cursor: pointer;
  }
  .hint {
    color: var(--text-dim);
    font-size: 0.75rem;
  }
  .picker-actions {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    margin-top: 0.2rem;
  }
  .live-err {
    color: var(--danger, #ef4444);
    font-size: 0.8rem;
  }
  .btn-ghost {
    background: none;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm, 6px);
    color: var(--text-dim);
    padding: 0.25rem 0.6rem;
    font-size: 0.8rem;
    cursor: pointer;
  }
  .btn-ghost:hover { border-color: var(--text-dim); }
  .btn-sm { font-size: 0.75rem; padding: 0.2rem 0.5rem; }
</style>

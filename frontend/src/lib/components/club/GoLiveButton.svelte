<script lang="ts">
  import { auth } from '../../nostr/auth.svelte'
  import { liveSession, goLive, endLive } from '../../nostr/livesession.svelte'
  import { connectLivekit, type LivekitClient } from '../../player/livekit'

  let { groupId }: { groupId: string } = $props()

  const session = $derived(liveSession.current)
  const iAmLive = $derived(session?.dj === auth.pubkey)

  type Step = 'idle' | 'connecting' | 'live' | 'error'
  let step = $state<Step>('idle')
  let err = $state('')
  let client = $state<LivekitClient | null>(null)

  // If the relay ended our session (e.g. another client took over), reset.
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
      await goLive(groupId, 'takeover', 'av')
      const c = await connectLivekit(groupId)
      await c.publishLocal({ video: true })
      client = c
      step = 'live'
    } catch (e) {
      err = String((e as Error)?.message ?? e)
      step = 'error'
      if (client) { void client.disconnect(); client = null }
      await endLive(groupId).catch(() => {})
    }
  }

  async function stopLive() {
    await endLive(groupId)
    if (client) { void client.disconnect(); client = null }
    step = 'idle'
  }

  function dismiss() {
    step = 'idle'
    err = ''
  }
</script>

{#if step === 'idle'}
  <button class="btn-live" onclick={startLive}>
    ● Go Live
  </button>
{:else if step === 'connecting'}
  <button class="btn-live" disabled>Connecting…</button>
{:else if step === 'live'}
  <button class="btn-live btn-live-on" onclick={stopLive}>
    ● Live — Go offline
  </button>
{:else if step === 'error'}
  <div class="live-wrap">
    <span class="live-err">{err}</span>
    <button class="btn-dismiss" onclick={dismiss}>✕</button>
  </div>
{/if}

<style>
  .btn-live {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.3rem 0.75rem;
    background: rgba(220, 38, 38, 0.15);
    border: 1px solid rgba(220, 38, 38, 0.5);
    border-radius: 999px;
    color: #ef4444;
    font-size: 0.78rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
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
    50% { box-shadow: 0 0 0 5px rgba(239, 68, 68, 0); }
  }
  @media (prefers-reduced-motion: reduce) {
    .btn-live-on { animation: none; }
  }
  .live-wrap {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }
  .live-err {
    font-size: 0.75rem;
    color: var(--danger, #ef4444);
    max-width: 220px;
  }
  .btn-dismiss {
    background: none;
    border: none;
    color: var(--text-dim);
    font-size: 0.75rem;
    cursor: pointer;
    padding: 0.1rem 0.2rem;
  }
</style>

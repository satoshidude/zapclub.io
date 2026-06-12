<script lang="ts">
  import { startRtmpStream, loadRtmpConfig, saveRtmpConfig, type RtmpSession } from '../../nostr/rtmpstream'

  let { groupId, clubName = '' }: { groupId: string; clubName?: string } = $props()

  const TWITCH_SERVER = 'rtmp://live.twitch.tv/app'

  type Step = 'idle' | 'open' | 'capturing' | 'streaming' | 'error'
  let step = $state<Step>('idle')
  let err = $state('')
  let session = $state<RtmpSession | null>(null)

  const saved = loadRtmpConfig(groupId)
  let key = $state(saved.key)
  let channel = $state(saved.watchURL ? saved.watchURL.replace('https://www.twitch.tv/', '') : '')

  const watchURL = $derived(channel.trim() ? `https://www.twitch.tv/${channel.trim()}` : '')

  async function start() {
    if (!key.trim()) return
    saveRtmpConfig(groupId, TWITCH_SERVER, key.trim(), watchURL)
    step = 'capturing'
    err = ''
    try {
      session = await startRtmpStream(groupId, clubName, TWITCH_SERVER, key.trim(), watchURL)
      step = 'streaming'
    } catch (e) {
      err = String((e as Error)?.message ?? e)
      step = 'error'
      session = null
    }
  }

  async function stop() {
    try { await session?.stop() } catch { /* best-effort */ }
    session = null
    step = 'idle'
  }

  function toggle() {
    if (step === 'idle') step = 'open'
    else if (step === 'open') step = 'idle'
  }
</script>

<div class="wrap">
  {#if step === 'idle' || step === 'open'}
    <button class="btn-stream" onclick={toggle} title="Stream club audio to Twitch">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <path d="M2.149 0L.537 4.119v16.836h5.731V24l4.121-3.045h3.284l5.434-5.434V0H2.149zm18.692 14.103l-2.688 2.688H14.65l-2.358 2.358v-2.358H8.285V1.791h12.556v12.312zm-2.356-8.057h-1.79v5.072h1.79V6.046zm-4.642 0H11.94v5.072h1.903V6.046z"/>
      </svg>
      {step === 'open' ? '▲ Twitch' : 'Stream to Twitch'}
    </button>

    {#if step === 'open'}
      <div class="panel">
        <label class="lbl">
          Stream Key
          <input
            class="inp"
            type="password"
            bind:value={key}
            placeholder="live_123456789_…"
            autocomplete="off"
            autofocus
          />
          <span class="hint-sm">Twitch Dashboard → Settings → Stream → Primary Stream Key</span>
        </label>
        <label class="lbl">
          Your Twitch channel <span class="opt">(optional — shows watch link to members)</span>
          <div class="channel-row">
            <span class="prefix">twitch.tv/</span>
            <input
              class="inp inp-channel"
              type="text"
              bind:value={channel}
              placeholder="yourchannel"
              autocomplete="off"
              spellcheck="false"
            />
          </div>
        </label>
        <p class="hint-capture">Your browser will ask you to share this tab's audio.</p>
        <button class="btn-start" onclick={start} disabled={!key.trim()}>
          Go Live
        </button>
      </div>
    {/if}

  {:else if step === 'capturing'}
    <span class="status">Waiting for audio capture…</span>

  {:else if step === 'streaming'}
    <button class="btn-stream btn-on" onclick={stop} title="Stop Twitch stream">
      ● Live on Twitch — Stop
    </button>

  {:else if step === 'error'}
    <div class="err-row">
      <span class="err-msg">{err}</span>
      <button class="mini" onclick={() => { step = 'open'; err = '' }}>↩</button>
    </div>
  {/if}
</div>

<style>
  .wrap {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .btn-stream {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.25rem 0.65rem;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--text-dim);
    font-size: 0.75rem;
    font-weight: 500;
    cursor: pointer;
    white-space: nowrap;
    transition: border-color 0.15s, color 0.15s;
  }
  .btn-stream:hover { border-color: #9146ff; color: #bf94ff; }
  .btn-on { border-color: #9146ff; color: #bf94ff; }

  .panel {
    display: flex;
    flex-direction: column;
    gap: 0.55rem;
    padding: 0.75rem;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius, 8px);
    max-width: 320px;
  }

  .lbl {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    font-size: 0.72rem;
    color: var(--text-dim);
    font-weight: 500;
  }
  .opt { font-weight: 400; opacity: 0.65; }
  .hint-sm { font-size: 0.68rem; opacity: 0.6; font-weight: 400; }
  .hint-capture {
    font-size: 0.68rem;
    color: var(--text-dim);
    opacity: 0.7;
    margin: 0;
    font-style: italic;
  }

  .inp {
    padding: 0.3rem 0.5rem;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text);
    font-size: 0.78rem;
    font-family: var(--font-mono, monospace);
    outline: none;
    width: 100%;
    box-sizing: border-box;
  }
  .inp:focus { border-color: #9146ff; }

  .channel-row {
    display: flex;
    align-items: center;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    overflow: hidden;
  }
  .channel-row:focus-within { border-color: #9146ff; }
  .prefix {
    padding: 0.3rem 0.4rem 0.3rem 0.5rem;
    font-size: 0.72rem;
    color: var(--text-dim);
    white-space: nowrap;
    border-right: 1px solid var(--border);
    background: var(--bg-elev);
    font-family: var(--font-mono, monospace);
  }
  .inp-channel { border: none; border-radius: 0; flex: 1; }
  .inp-channel:focus { border-color: transparent; }

  .btn-start {
    align-self: flex-start;
    padding: 0.3rem 1rem;
    background: #9146ff;
    border: none;
    border-radius: 4px;
    color: #fff;
    font-size: 0.78rem;
    font-weight: 700;
    cursor: pointer;
    transition: opacity 0.15s;
  }
  .btn-start:disabled { opacity: 0.4; cursor: default; }
  .btn-start:not(:disabled):hover { opacity: 0.85; }

  .status { font-size: 0.75rem; color: var(--text-dim); }

  .err-row { display: flex; align-items: center; gap: 0.4rem; }
  .err-msg {
    font-size: 0.72rem;
    color: var(--danger, #ef4444);
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mini { background: none; border: none; color: var(--text-dim); font-size: 0.75rem; cursor: pointer; padding: 0.1rem 0.2rem; }
</style>

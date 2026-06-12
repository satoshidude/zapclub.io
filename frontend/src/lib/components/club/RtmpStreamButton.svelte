<script lang="ts">
  import { auth } from '../../nostr/auth.svelte'
  import { startRtmpStream, stopRtmpStream, loadRtmpConfig, saveRtmpConfig } from '../../nostr/rtmpstream'

  let { groupId, clubName = '' }: { groupId: string; clubName?: string } = $props()

  type Step = 'idle' | 'open' | 'connecting' | 'streaming' | 'error'
  let step = $state<Step>('idle')
  let err = $state('')

  const saved = loadRtmpConfig(groupId)
  let server = $state(saved.server)
  let key = $state(saved.key)
  let watchURL = $state(saved.watchURL)

  async function start() {
    if (!server.trim() || !key.trim()) return
    saveRtmpConfig(groupId, server.trim(), key.trim(), watchURL.trim())
    step = 'connecting'
    err = ''
    try {
      await startRtmpStream(groupId, clubName, server.trim(), key.trim(), watchURL.trim())
      step = 'streaming'
    } catch (e) {
      err = String((e as Error)?.message ?? e)
      step = 'error'
    }
  }

  async function stop() {
    try {
      await stopRtmpStream(groupId)
    } catch { /* best-effort */ }
    step = 'idle'
  }

  function toggle() {
    if (step === 'idle') step = 'open'
    else if (step === 'open') step = 'idle'
  }
</script>

{#if auth.canSign}
  <div class="rtmp-wrap">
    {#if step === 'idle' || step === 'open'}
      <button class="btn-stream" onclick={toggle} title="Stream club audio to RTMP">
        {step === 'open' ? '▲' : '⬆'} Stream to RTMP
      </button>

      {#if step === 'open'}
        <div class="panel">
          <label class="lbl">
            RTMP Server
            <input
              class="inp"
              type="url"
              bind:value={server}
              placeholder="rtmp://in.core.zap.stream:1935/good"
              autocomplete="off"
              spellcheck="false"
            />
          </label>
          <label class="lbl">
            Stream Key
            <input
              class="inp"
              type="password"
              bind:value={key}
              placeholder="your-stream-key"
              autocomplete="off"
            />
          </label>
          <label class="lbl">
            Watch URL <span class="opt">(optional — shown to club members)</span>
            <input
              class="inp"
              type="url"
              bind:value={watchURL}
              placeholder="https://www.twitch.tv/yourchannel"
              autocomplete="off"
              spellcheck="false"
            />
          </label>
          <button
            class="btn-start"
            onclick={start}
            disabled={!server.trim() || !key.trim()}
          >
            Start Stream
          </button>
        </div>
      {/if}

    {:else if step === 'connecting'}
      <span class="status dim">Connecting…</span>

    {:else if step === 'streaming'}
      <button class="btn-stream btn-on" onclick={stop} title="Stop RTMP stream">
        ● Streaming — Stop
      </button>

    {:else if step === 'error'}
      <div class="err-row">
        <span class="err-msg">{err}</span>
        <button class="mini" onclick={() => { step = 'open'; err = '' }}>↩</button>
      </div>
    {/if}
  </div>
{/if}

<style>
  .rtmp-wrap {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .btn-stream {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
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
  .btn-stream:hover {
    border-color: var(--accent, #7c3aed);
    color: var(--text);
  }
  .btn-on {
    border-color: #7c3aed;
    color: #a78bfa;
  }

  .panel {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.7rem;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius, 8px);
  }

  .lbl {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    font-size: 0.72rem;
    color: var(--text-dim);
  }
  .opt {
    font-weight: 400;
    color: var(--text-muted, var(--text-dim));
    opacity: 0.7;
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
  }
  .inp:focus {
    border-color: var(--accent, #7c3aed);
  }

  .btn-start {
    align-self: flex-start;
    padding: 0.3rem 0.8rem;
    background: var(--accent, #7c3aed);
    border: none;
    border-radius: 4px;
    color: #fff;
    font-size: 0.78rem;
    font-weight: 600;
    cursor: pointer;
    transition: opacity 0.15s;
  }
  .btn-start:disabled { opacity: 0.4; cursor: default; }
  .btn-start:not(:disabled):hover { opacity: 0.85; }

  .status {
    font-size: 0.75rem;
    color: var(--text-dim);
  }

  .err-row {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }
  .err-msg {
    font-size: 0.72rem;
    color: var(--danger, #ef4444);
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mini {
    background: none;
    border: none;
    color: var(--text-dim);
    font-size: 0.75rem;
    cursor: pointer;
    padding: 0.1rem 0.2rem;
  }
</style>

<script lang="ts">
  import { loadNwcConnection, saveNwcConnection, clearNwcConnection } from '../../nostr/premium.svelte'
  import { auth } from '../../nostr/auth.svelte'

  let nwcStored = $state(!!loadNwcConnection())
  let nwcInput  = $state('')
  let nwcOpen   = $state(false)
  let nwcError  = $state('')

  // Balance
  let balance    = $state<number | null>(null)
  let balLoading = $state(false)

  // Receive invoice
  let rxInvoice = $state('')
  let rxLoading = $state(false)
  let rxAmount  = $state(1000)
  let rxCopied  = $state(false)

  async function fetchBalance() {
    const connStr = loadNwcConnection()
    if (!connStr) return
    balLoading = true
    try {
      const { NWCClient } = await import('@getalby/sdk/nwc')
      const client = new NWCClient({ nostrWalletConnectUrl: connStr })
      const res = await client.getBalance()
      balance = Math.floor(res.balance / 1000)
      client.close()
    } catch { balance = null } finally { balLoading = false }
  }

  async function makeReceiveInvoice() {
    const connStr = loadNwcConnection()
    if (!connStr) return
    rxLoading = true; rxInvoice = ''
    try {
      const { NWCClient } = await import('@getalby/sdk/nwc')
      const client = new NWCClient({ nostrWalletConnectUrl: connStr })
      const res = await client.makeInvoice({ amount: rxAmount * 1000, description: 'Top up — zapclub.io' })
      rxInvoice = res.invoice
      client.close()
    } catch (e) { console.warn('[nwc] makeInvoice failed', e) } finally { rxLoading = false }
  }

  async function copyRx() {
    await navigator.clipboard.writeText(rxInvoice)
    rxCopied = true; setTimeout(() => (rxCopied = false), 2000)
  }

  $effect(() => {
    if (nwcOpen && nwcStored && balance === null) void fetchBalance()
  })

  function saveNwc() {
    const s = nwcInput.trim()
    if (!s.startsWith('nostr+walletconnect://')) { nwcError = 'Must start with nostr+walletconnect://'; return }
    saveNwcConnection(s); nwcStored = true; nwcOpen = false; nwcInput = ''; nwcError = ''; balance = null
  }

  function removeNwc() {
    clearNwcConnection(); nwcStored = false; balance = null; rxInvoice = ''
  }
</script>

{#if auth.isLoggedIn}
<div class="nwc-block">
  <button
    class="nwc-head"
    class:connected={nwcStored}
    onclick={() => (nwcOpen = !nwcOpen)}
    title={nwcStored ? 'Wallet — click to manage' : 'Connect NWC wallet for 1-tap voting'}
  >
    <span class="dot" class:green={nwcStored}></span>
    <span class="nwc-label">{nwcStored ? '⚡ Wallet' : '⚡ Connect wallet'}</span>
    {#if nwcStored}
      {#if balance !== null}
        <span class="nwc-bal">{balance.toLocaleString()} sats</span>
      {:else}
        <span class="nwc-sub">1-tap votes ▪ NWC</span>
      {/if}
    {:else}
      <span class="nwc-sub">NWC · 1 sat/vote</span>
    {/if}
    <span class="nwc-chevron">{nwcOpen ? '▲' : '▼'}</span>
  </button>

  {#if nwcOpen}
    <div class="nwc-panel">
      {#if nwcStored}

        <!-- Balance -->
        <div class="bal-row">
          <span class="bal-label">Balance</span>
          {#if balLoading}
            <span class="bal-val dim">…</span>
          {:else if balance !== null}
            <span class="bal-val">{balance.toLocaleString()} <span class="bal-unit">sats</span></span>
          {:else}
            <span class="bal-val dim">—</span>
          {/if}
          <button class="icon-btn" onclick={() => { balance = null; void fetchBalance() }} disabled={balLoading} title="Refresh">↻</button>
        </div>

        <!-- Receive / top up -->
        {#if rxInvoice}
          <div class="rx-result">
            <p class="rx-hint">Share or open this invoice to receive sats</p>
            <div class="rx-actions">
              <a class="btn-alby" href="alby:{rxInvoice}">Open in Alby Go</a>
              <a class="btn-lightning" href="lightning:{rxInvoice}">Open in wallet</a>
              <button class="btn-copy" onclick={copyRx}>{rxCopied ? '✓ Copied' : 'Copy'}</button>
            </div>
            <button class="link-btn" onclick={() => (rxInvoice = '')}>← back</button>
          </div>
        {:else}
          <div class="rx-row">
            <span class="rx-label">Receive</span>
            <input class="rx-input" type="number" min="1" max="999999" bind:value={rxAmount} />
            <span class="rx-unit">sats</span>
            <button class="btn-receive" onclick={makeReceiveInvoice} disabled={rxLoading || rxAmount < 1}>
              {rxLoading ? '…' : '↙ Invoice'}
            </button>
          </div>
        {/if}

        <button class="nwc-remove" onclick={removeNwc}>Disconnect wallet</button>

      {:else}
        <p class="nwc-hint">Paste your NWC connection string to vote with 1 sat automatically</p>
        <input
          class="nwc-input"
          type="password"
          placeholder="nostr+walletconnect://..."
          bind:value={nwcInput}
          onkeydown={(e) => e.key === 'Enter' && saveNwc()}
        />
        {#if nwcError}<p class="nwc-err">{nwcError}</p>{/if}
        <button class="nwc-save" onclick={saveNwc} disabled={!nwcInput.trim()}>Connect</button>
      {/if}
    </div>
  {/if}
</div>
{/if}

<style>
  .nwc-block {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .nwc-head {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.45rem;
    padding: 0.6rem 0.8rem;
    background: none;
    border: none;
    cursor: pointer;
    text-align: left;
  }
  .nwc-head:hover { background: var(--bg-elev-2, #111118); }

  .dot {
    width: 7px; height: 7px;
    border-radius: 50%;
    background: #3a3a5a;
    flex-shrink: 0;
  }
  .dot.green { background: #4ec94e; box-shadow: 0 0 5px #4ec94e88; }

  .nwc-label {
    font-size: 0.78rem;
    font-weight: 600;
    color: var(--text);
    flex: 1;
  }
  .nwc-bal {
    font-size: 0.72rem;
    font-weight: 700;
    color: var(--amber, #f59e0b);
    letter-spacing: 0.01em;
  }
  .nwc-sub {
    font-size: 0.63rem;
    color: var(--text-dim);
  }
  .nwc-chevron {
    font-size: 0.5rem;
    color: var(--text-dim);
    margin-left: 0.15rem;
  }

  /* Panel */
  .nwc-panel {
    padding: 0.6rem 0.8rem 0.7rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    border-top: 1px solid var(--border);
  }

  /* Balance row */
  .bal-row {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    min-height: 1.6rem;
  }
  .bal-label {
    font-size: 0.68rem;
    color: var(--text-dim);
    flex-shrink: 0;
  }
  .bal-val {
    font-size: 0.82rem;
    font-weight: 700;
    color: var(--amber, #f59e0b);
    flex: 1;
  }
  .bal-val.dim { color: var(--text-dim); font-weight: 400; }
  .bal-unit { font-size: 0.65rem; font-weight: 500; }
  .icon-btn {
    background: none;
    border: none;
    color: var(--text-dim);
    font-size: 0.85rem;
    cursor: pointer;
    padding: 0.1rem 0.25rem;
    border-radius: 3px;
    line-height: 1;
  }
  .icon-btn:hover { color: var(--text); background: var(--bg-elev-2, #111118); }
  .icon-btn:disabled { opacity: 0.4; cursor: default; }

  /* Receive row */
  .rx-row {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    flex-wrap: wrap;
  }
  .rx-label {
    font-size: 0.68rem;
    color: var(--text-dim);
    flex-shrink: 0;
  }
  .rx-input {
    width: 70px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text);
    font-size: 0.72rem;
    padding: 0.2rem 0.35rem;
    text-align: right;
  }
  .rx-input:focus { outline: none; border-color: var(--accent); }
  .rx-unit {
    font-size: 0.65rem;
    color: var(--text-dim);
    flex-shrink: 0;
  }
  .btn-receive {
    background: var(--bg-elev-2, #111118);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text);
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.22rem 0.5rem;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn-receive:hover { border-color: var(--accent); color: var(--accent); }
  .btn-receive:disabled { opacity: 0.4; cursor: default; }

  /* Receive result */
  .rx-result {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .rx-hint {
    font-size: 0.65rem;
    color: var(--text-dim);
    margin: 0;
  }
  .rx-actions {
    display: flex;
    gap: 0.3rem;
    flex-wrap: wrap;
  }
  .btn-alby {
    display: inline-flex;
    align-items: center;
    gap: 0.2rem;
    background: #29a0d4;
    color: #fff;
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.25rem 0.55rem;
    border-radius: 4px;
    text-decoration: none;
    white-space: nowrap;
  }
  .btn-alby:hover { background: #1e8ab8; }
  .btn-lightning {
    display: inline-flex;
    align-items: center;
    background: #2a2a40;
    color: var(--text);
    font-size: 0.7rem;
    padding: 0.25rem 0.55rem;
    border-radius: 4px;
    text-decoration: none;
    white-space: nowrap;
  }
  .btn-lightning:hover { background: #36364e; }
  .btn-copy {
    background: none;
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text-dim);
    font-size: 0.7rem;
    padding: 0.22rem 0.45rem;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn-copy:hover { border-color: var(--text-dim); color: var(--text); }
  .link-btn {
    background: none; border: none; color: var(--text-dim);
    font-size: 0.65rem; cursor: pointer; padding: 0; text-align: left;
  }
  .link-btn:hover { color: var(--text); }

  /* Disconnect / connect */
  .nwc-remove {
    background: none;
    border: none;
    color: var(--text-dim);
    font-size: 0.65rem;
    padding: 0;
    cursor: pointer;
    text-align: left;
    align-self: flex-start;
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .nwc-remove:hover { color: var(--danger); }

  .nwc-hint {
    font-size: 0.7rem;
    color: var(--text-dim);
    margin: 0;
    line-height: 1.4;
  }
  .nwc-input {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text);
    font-size: 0.72rem;
    padding: 0.3rem 0.5rem;
    width: 100%;
    box-sizing: border-box;
  }
  .nwc-input:focus { outline: none; border-color: var(--accent); }
  .nwc-err { font-size: 0.65rem; color: var(--danger); margin: 0; }
  .nwc-save {
    background: var(--accent);
    border: none;
    border-radius: 4px;
    color: #fff;
    font-size: 0.72rem;
    font-weight: 600;
    padding: 0.3rem 0.6rem;
    cursor: pointer;
    align-self: flex-start;
  }
  .nwc-save:disabled { opacity: 0.4; cursor: default; }
</style>

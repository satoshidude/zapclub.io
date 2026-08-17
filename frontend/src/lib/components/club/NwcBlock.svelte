<script lang="ts">
  import { loadNwcConnection, saveNwcConnection, clearNwcConnection, syncNwcFromNostr } from '../../nostr/premium.svelte'
  import { zaps } from '../../nostr/zaps.svelte'
  import { useProfile, displayName } from '../../nostr/profiles.svelte'
  import { auth } from '../../nostr/auth.svelte'

  let nwcStored = $state(false)
  let nwcInput  = $state('')
  let nwcError  = $state('')
  let nwcSyncing = $state(false)

  $effect(() => {
    void auth.pubkey
    const local = !!loadNwcConnection()
    nwcStored = local
    nwcInput = ''
    nwcError = ''
    if (!local && auth.pubkey) {
      nwcSyncing = true
      syncNwcFromNostr().then((found) => {
        if (found) { nwcStored = true; void fetchBalance() }
      }).catch(() => {}).finally(() => { nwcSyncing = false })
    }
    if (local) void fetchBalance()
  })

  let balance    = $state<number | null>(null)
  let balLoading = $state(false)

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

  function relTime(ts: number): string {
    const s = Math.floor(Date.now() / 1000) - Math.floor(ts / 1000)
    if (s < 60) return `${s}s ago`
    if (s < 3600) return `${Math.floor(s / 60)}m ago`
    if (s < 86400) return `${Math.floor(s / 3600)}h ago`
    return `${Math.floor(s / 86400)}d ago`
  }

  async function saveNwc() {
    const s = nwcInput.trim()
    if (!s.startsWith('nostr+walletconnect://')) { nwcError = 'Must start with nostr+walletconnect://'; return }
    await saveNwcConnection(s)
    nwcStored = true
    nwcInput = ''
    nwcError = ''
    balance = null
    void fetchBalance()
  }

  async function removeNwc() {
    await clearNwcConnection()
    nwcStored = false
    balance = null
  }
</script>

{#if auth.isLoggedIn}
<div class="nwc-block">
  <div class="nwc-head">
    <span class="dot" class:green={nwcStored}></span>
    <span class="nwc-label">{nwcStored ? '⚡ Wallet' : nwcSyncing ? '⚡ Syncing…' : '⚡ Connect wallet'}</span>
    {#if nwcStored && balance !== null}
      <span class="nwc-bal">{balance.toLocaleString()} sats</span>
    {:else}
      <span class="nwc-sub">NWC · 1-tap zaps</span>
    {/if}
    {#if nwcStored}
      <button class="icon-btn" onclick={() => { balance = null; void fetchBalance() }} disabled={balLoading} title="Refresh">↻</button>
    {/if}
  </div>

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
      </div>

      <!-- Last 5 zaps sent this session -->
      {#if zaps.myRecent.length > 0}
        <ul class="tx-list">
          {#each zaps.myRecent as z (z.at)}
            {@const profile = useProfile(z.dj)}
            <li class="tx-row">
              <span class="tx-bolt">⚡</span>
              <span class="tx-desc">{displayName(z.dj, profile)}</span>
              <span class="tx-amt">−{z.sats.toLocaleString()}</span>
              <span class="tx-time">{relTime(z.at)}</span>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="tx-empty">No zaps yet this session</p>
      {/if}

      <button class="nwc-remove" onclick={removeNwc}>Disconnect wallet</button>

    {:else}
      <p class="nwc-hint">Paste your NWC connection string to zap with one tap</p>
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
    display: flex;
    align-items: center;
    gap: 0.45rem;
    padding: 0.6rem 0.8rem;
  }

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

  .nwc-panel {
    padding: 0 0.8rem 0.7rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    border-top: 1px solid var(--border);
  }

  .bal-row {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    min-height: 1.6rem;
    padding-top: 0.5rem;
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

  .tx-empty {
    font-size: 0.65rem;
    color: var(--text-dim);
    margin: 0;
  }
  .tx-list {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 0.22rem;
  }
  .tx-row {
    display: grid;
    grid-template-columns: 14px 1fr auto auto;
    align-items: center;
    gap: 0.3rem;
    font-size: 0.68rem;
    min-height: 1.3rem;
  }
  .tx-bolt {
    font-size: 0.65rem;
    text-align: center;
    color: var(--amber, #f59e0b);
  }
  .tx-desc {
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tx-amt {
    font-weight: 600;
    white-space: nowrap;
    font-size: 0.67rem;
    color: var(--amber, #f59e0b);
  }
  .tx-time {
    color: var(--text-dim);
    font-size: 0.6rem;
    white-space: nowrap;
  }

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
    padding-top: 0.5rem;
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

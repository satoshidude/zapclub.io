<script lang="ts">
  import qrcode from 'qrcode-generator'
  import { fetchPremiumInvoice, pollPremiumPaid, saveNwcConnection, loadNwcConnection, clearNwcConnection } from '../nostr/premium.svelte'
  import { auth } from '../nostr/auth.svelte'

  let { onClose }: { onClose: () => void } = $props()

  interface WebLN {
    enable(): Promise<void>
    sendPayment(invoice: string): Promise<{ preimage: string }>
  }
  const webln = $derived(
    typeof window !== 'undefined' ? ((window as unknown as { webln?: WebLN }).webln ?? null) : null,
  )
  const preferAlby = (() => {
    if (typeof navigator === 'undefined') return false
    const ua = navigator.userAgent
    const iOS = /iPad|iPhone|iPod/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)
    const macSafari = /Macintosh/.test(ua) && /Safari/.test(ua) && !/Chrome|Chromium|Edg|OPR/.test(ua)
    return iOS || macSafari
  })()

  let step = $state<'info' | 'pay' | 'success' | 'nwc'>('info')
  let bolt11 = $state('')
  let hash = $state('')
  let loading = $state(false)
  let error = $state('')
  let paying = $state(false)
  let copied = $state(false)
  let nwcInput = $state(loadNwcConnection() ?? '')
  let nwcActive = $state(!!loadNwcConnection())

  const qrSvg = $derived.by(() => {
    if (!bolt11) return ''
    try {
      const qr = qrcode(0, 'M')
      qr.addData(bolt11.toUpperCase())
      qr.make()
      return qr.createSvgTag({ scalable: true })
    } catch {
      return ''
    }
  })

  async function startPayment() {
    loading = true
    error = ''
    try {
      const inv = await fetchPremiumInvoice()
      bolt11 = inv.bolt11
      hash = inv.hash
      step = 'pay'
      // Poll for payment in background; flip to success when confirmed.
      let active = true
      const done = pollPremiumPaid(hash, () => active)
      done.then((paid) => {
        if (paid) step = 'success'
      })
      // Cleanup on unmount / re-call
      return () => { active = false }
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      loading = false
    }
  }

  async function payWithExtension() {
    if (!webln || paying) return
    paying = true
    error = ''
    try {
      await webln.enable()
      await webln.sendPayment(bolt11)
      // Relay confirms via polling sub — give it a moment then mark success.
      setTimeout(() => { if (step === 'pay') step = 'success' }, 2000)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      paying = false
    }
  }

  async function copyInvoice() {
    await navigator.clipboard.writeText(bolt11)
    copied = true
    setTimeout(() => (copied = false), 2000)
  }

  function saveNwc() {
    if (nwcInput.trim()) {
      saveNwcConnection(nwcInput.trim())
      nwcActive = true
      step = 'info'
    }
  }

  function removeNwc() {
    clearNwcConnection()
    nwcInput = ''
    nwcActive = false
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="modal-overlay" onclick={onClose}>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal" onclick={(e) => e.stopPropagation()}>
    <button class="close" onclick={onClose}>✕</button>

    {#if step === 'info'}
      <h2>⚡ zapclub Premium</h2>
      <p class="price">2,100 sats / month</p>
      <ul class="features">
        <li>✓ Up to 5 DJ slots on your clubs</li>
        <li>✓ Entry-fee clubs (Sats gate)</li>
        <li>✓ Featured listing in the directory</li>
        <li>✓ Private / invite-only clubs</li>
      </ul>
      {#if nwcActive}
        <p class="nwc-badge">⚡ NWC auto-renew active</p>
      {/if}
      {#if error}<p class="err">{error}</p>{/if}
      <div class="actions">
        <button class="btn btn-primary" onclick={startPayment} disabled={loading || !auth.canSign}>
          {loading ? '…' : 'Subscribe for 2,100 sats'}
        </button>
        {#if !auth.canSign}
          <p class="hint">Sign in to subscribe.</p>
        {/if}
        <button class="btn btn-ghost btn-sm nwc-btn" onclick={() => (step = 'nwc')}>
          {nwcActive ? '⚙ NWC auto-renew' : '⚡ Set up auto-renew (NWC)'}
        </button>
      </div>

    {:else if step === 'pay'}
      <h2>Pay 2,100 sats</h2>
      <p class="sub">Your premium starts immediately after payment.</p>
      {#if qrSvg}
        <div class="qr">
          <!-- eslint-disable-next-line svelte/no-at-html-tags -->
          {@html qrSvg}
        </div>
      {/if}
      <div class="invoice-actions">
        {#if !preferAlby && webln}
          <button class="btn btn-primary" onclick={payWithExtension} disabled={paying}>
            {paying ? '…' : '⚡ Pay with wallet'}
          </button>
        {:else if preferAlby}
          <a class="btn btn-primary" href={`alby:${bolt11}`}>Open in Alby Go</a>
        {/if}
        <a class="btn btn-ghost" href={`lightning:${bolt11}`}>Open in wallet</a>
        <button class="btn btn-ghost btn-sm" onclick={copyInvoice}>
          {copied ? 'Copied!' : 'Copy invoice'}
        </button>
      </div>
      {#if error}<p class="err">{error}</p>{/if}
      <p class="hint">Waiting for payment…</p>

    {:else if step === 'success'}
      <div class="success">
        <span class="check">✓</span>
        <h2>Premium activated!</h2>
        <p>Your subscription is live. Enjoy all premium features.</p>
        <button class="btn btn-primary" onclick={onClose}>Done</button>
      </div>

    {:else if step === 'nwc'}
      <h2>NWC auto-renew</h2>
      <p class="hint">Connect a Nostr Wallet Connect (NWC) compatible wallet (e.g. Alby Hub) to auto-renew 3 days before expiry. Your connection string is stored locally — never sent anywhere.</p>
      <input
        class="nwc-input"
        bind:value={nwcInput}
        placeholder="nostr+walletconnect://…"
        autocomplete="off"
        spellcheck="false"
      />
      <div class="actions">
        <button class="btn btn-primary" onclick={saveNwc} disabled={!nwcInput.trim()}>Save</button>
        {#if nwcActive}
          <button class="btn btn-ghost btn-sm" onclick={removeNwc}>Remove connection</button>
        {/if}
        <button class="btn btn-ghost btn-sm" onclick={() => (step = 'info')}>← Back</button>
      </div>
    {/if}
  </div>
</div>

<style>
  .modal-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 200;
  }
  .modal {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 2rem 2rem 1.5rem;
    width: min(92vw, 380px);
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }
  .close {
    position: absolute;
    top: 0.8rem;
    right: 0.9rem;
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 1rem;
  }
  h2 {
    margin: 0;
    font-size: 1.15rem;
  }
  .price {
    font-size: 1.5rem;
    font-weight: 700;
    color: var(--accent);
    margin: 0;
  }
  .features {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    font-size: 0.9rem;
  }
  .features li {
    color: var(--text-dim);
  }
  .nwc-badge {
    margin: 0;
    font-size: 0.8rem;
    color: var(--accent);
  }
  .actions {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .nwc-btn {
    align-self: flex-start;
    color: var(--text-dim);
  }
  .sub {
    margin: 0;
    font-size: 0.85rem;
    color: var(--text-dim);
  }
  .qr {
    width: 200px;
    height: 200px;
    align-self: center;
    background: white;
    border-radius: 8px;
    padding: 8px;
    box-sizing: border-box;
  }
  .qr :global(svg) {
    width: 100%;
    height: 100%;
  }
  .invoice-actions {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .hint {
    margin: 0;
    font-size: 0.8rem;
    color: var(--text-dim);
    text-align: center;
  }
  .err {
    margin: 0;
    color: var(--danger);
    font-size: 0.82rem;
  }
  .success {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.8rem;
    padding: 1rem 0;
  }
  .check {
    font-size: 2.5rem;
    color: var(--accent);
  }
  .nwc-input {
    width: 100%;
    box-sizing: border-box;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.7rem;
    color: var(--text);
    font-size: 0.82rem;
    font-family: monospace;
  }
  .nwc-input:focus {
    outline: none;
    border-color: var(--accent-2);
  }
</style>

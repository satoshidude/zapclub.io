<script lang="ts">
  import qrcode from 'qrcode-generator'
  import { payModal, hidePay, markPaid } from '../nostr/payModal.svelte'

  // WebLN (Alby browser extension / Alby Hub on desktop) for one-click payment.
  interface WebLN {
    enable(): Promise<void>
    sendPayment(invoice: string): Promise<{ preimage: string }>
  }
  const webln = $derived(
    typeof window !== 'undefined' ? ((window as unknown as { webln?: WebLN }).webln ?? null) : null,
  )

  // On iOS and macOS Safari, prefer the lightning: deep link → it opens the locally
  // installed Alby Go (the iOS app also runs on Apple-Silicon Macs and registers the
  // scheme). On those platforms "Open in Alby Go" is the default action, even if a WebLN
  // extension is present.
  const preferAlby = (() => {
    if (typeof navigator === 'undefined') return false
    const ua = navigator.userAgent
    const iOS = /iPad|iPhone|iPod/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)
    const macSafari = /Macintosh/.test(ua) && /Safari/.test(ua) && !/Chrome|Chromium|Edg|OPR/.test(ua)
    return iOS || macSafari
  })()

  let paying = $state(false)
  let payErr = $state('')

  async function payWithExtension() {
    if (!webln || paying) return
    paying = true
    payErr = ''
    try {
      await webln.enable()
      await webln.sendPayment(payModal.invoice)
      markPaid()
    } catch (e) {
      payErr = String((e as Error)?.message ?? e)
    } finally {
      paying = false
    }
  }

  const qrSrc = $derived.by(() => {
    if (!payModal.invoice) return ''
    try {
      const qr = qrcode(0, 'M')
      qr.addData(payModal.invoice.toUpperCase()) // bolt11 is case-insensitive → smaller QR
      qr.make()
      return qr.createDataURL(4, 8)
    } catch {
      return ''
    }
  })

  // Once paid (WebLN success, verify poll, or manual confirm) flash "Paid!" then close.
  $effect(() => {
    if (!payModal.paid) return
    const t = setTimeout(hidePay, 1600)
    return () => clearTimeout(t)
  })

  // Open the invoice in a wallet app. Driving window.location from the tap handler is
  // more reliable than an <a href="lightning:…"> in iOS Safari, which often does nothing.
  function openWallet() {
    if (payModal.invoice) window.location.href = `lightning:${payModal.invoice}`
  }

  let copied = $state(false)
  async function copy() {
    try {
      await navigator.clipboard.writeText(payModal.invoice)
      copied = true
      setTimeout(() => (copied = false), 1500)
    } catch {
      /* ignore */
    }
  }
</script>

{#if payModal.open}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="backdrop" role="presentation" onclick={hidePay}>
    <div class="sheet" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => e.stopPropagation()}>
      {#if payModal.paid}
        <div class="paid">⚡ Paid! Thank you.</div>
        <button class="cancel" onclick={hidePay}>Close</button>
      {:else}
        <h3>{payModal.label} · {payModal.sats} sats</h3>
        {#if qrSrc}
          <button class="qr-link" onclick={openWallet} title="Open in wallet">
            <img class="qr" src={qrSrc} alt="invoice QR" width="220" height="220" />
          </button>
        {/if}
        <!-- lightning: scheme → opens Alby Go locally (iOS / macOS Safari) or any wallet
             registered for the scheme. Primary/default on Apple Safari. -->
        <button class="btn {preferAlby || !webln ? 'btn-primary' : 'btn-ghost'} big" onclick={openWallet}>
          📲 Open in Alby Go
        </button>
        {#if webln}
          <button class="btn {preferAlby ? 'btn-ghost' : 'btn-primary'} big" onclick={payWithExtension} disabled={paying}>
            {paying ? 'Paying…' : '⚡ Pay now'}
          </button>
          {#if payErr}<p class="err">⚠ {payErr}</p>{/if}
        {/if}
        <button class="copy" onclick={copy}>{copied ? '✓ Copied' : 'Copy invoice'}</button>
        <p class="hint">Pay with the Alby extension, scan the QR, or tap “Open in Alby Go” on mobile.</p>
        <!-- No reliable auto-detect (LNURL endpoint has no verify URL) → confirm manually. -->
        <button class="btn btn-ghost done" onclick={markPaid}>✓ I’ve paid</button>
        <button class="cancel" onclick={hidePay}>Cancel</button>
      {/if}
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 210;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(3px);
    display: grid;
    place-items: center;
    padding: 1rem;
  }
  .sheet {
    width: 100%;
    max-width: 320px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.2rem;
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.55);
    text-align: center;
  }
  h3 {
    margin: 0;
    font-size: 1rem;
  }
  .qr-link {
    align-self: center;
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
  }
  .qr {
    width: 220px;
    height: 220px;
    border-radius: 8px;
    background: #fff;
    padding: 6px;
    display: block;
  }
  .big {
    width: 100%;
    padding: 0.8rem;
    font-size: 1rem;
    justify-content: center;
  }
  .copy {
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: var(--radius-sm);
    padding: 0.5rem;
    font-size: 0.82rem;
    cursor: pointer;
  }
  .copy:hover {
    border-color: var(--accent-2);
    color: var(--text);
  }
  .done {
    width: 100%;
    justify-content: center;
    color: var(--accent);
  }
  .paid {
    color: var(--accent);
    font-weight: 700;
    padding: 1rem 0;
  }
  .hint {
    margin: 0;
    font-size: 0.74rem;
    color: var(--text-dim);
  }
  .err {
    margin: 0;
    font-size: 0.8rem;
    color: var(--danger);
  }
  .cancel {
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 0.85rem;
    padding: 0.3rem;
  }
</style>

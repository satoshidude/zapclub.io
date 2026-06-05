<script lang="ts">
  import qrcode from 'qrcode-generator'
  import { payModal, hidePay } from '../nostr/payModal.svelte'

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
          <a class="qr-link" href={`lightning:${payModal.invoice}`}>
            <img class="qr" src={qrSrc} alt="invoice QR" width="220" height="220" />
          </a>
        {/if}
        <a class="btn btn-primary big" href={`lightning:${payModal.invoice}`}>⚡ Open in wallet</a>
        <button class="copy" onclick={copy}>{copied ? '✓ Copied' : 'Copy invoice'}</button>
        <p class="hint">Scan or open in any Lightning wallet (e.g. Alby Go).</p>
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
  .cancel {
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 0.85rem;
    padding: 0.3rem;
  }
</style>

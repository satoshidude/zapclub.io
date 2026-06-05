<script lang="ts">
  import { sync } from '../../nostr/sync.svelte'
  import { requestZapInvoice, zaps, subscribeZaps } from '../../nostr/zaps.svelte'
  import { showPay } from '../../nostr/payModal.svelte'
  import { useProfile, displayName } from '../../nostr/profiles.svelte'
  import { auth } from '../../nostr/auth.svelte'

  const PRESETS = [21, 100, 500, 2100]
  // Fallback payee when the DJ has no lightning address on their profile. The vote/score
  // still belongs to the DJ (p-tag = DJ pubkey).
  const FALLBACK_LUD16 = 'zapclub@nsnip.io'

  const np = $derived(sync.live)
  const dj = $derived(np?.dj ?? '')
  const djProfile = $derived(dj ? useProfile(dj) : null)
  const lud16 = $derived((djProfile?.lud16 as string) || FALLBACK_LUD16)
  const isSelf = $derived(!!dj && dj === auth.pubkey)
  const show = $derived(!!np && !isSelf)
  // Total sats this DJ has received in zaps (all-time, from 9735 receipts).
  const total = $derived(dj ? zaps.score(dj) : 0)

  // Subscribe to the live DJ's zap receipts so the total is loaded even when they
  // aren't on the stage (the stage subscription only covers stage DJs).
  $effect(() => {
    if (!dj) return
    return subscribeZaps([dj])
  })

  function fmtSats(n: number): string {
    return n >= 1000 ? (n / 1000).toFixed(n >= 10000 ? 0 : 1).replace(/\.0$/, '') + 'k' : String(n)
  }

  let open = $state(false)
  let comment = $state('')
  let custom = $state('')
  let busy = $state(false)
  let error = $state('')

  async function zapNow(sats: number) {
    if (busy || sats <= 0) return
    busy = true
    error = ''
    try {
      const { invoice, verify } = await requestZapInvoice(dj, lud16, sats, comment.trim())
      open = false
      comment = ''
      custom = ''
      showPay(invoice, sats, `Zap ${displayName(dj, djProfile)}`, verify)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }
</script>

{#if show}
  <button class="zap-mini" onclick={() => (open = !open)} disabled={!auth.canSign} title="Zap {displayName(dj, djProfile)} · {total} sats received">
    <span class="bolt">⚡</span>
    <span class="lbl">zap</span>
    {#if total > 0}<span class="score">{fmtSats(total)}</span>{/if}
  </button>

  {#if open}
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <div class="backdrop" role="presentation" onclick={() => (open = false)}>
      <div class="sheet" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => e.stopPropagation()}>
        <h3>⚡ Zap {displayName(dj, djProfile)}</h3>
        <div class="presets">
          {#each PRESETS as amt (amt)}
            <button class="amt" onclick={() => zapNow(amt)} disabled={busy}>{amt}</button>
          {/each}
        </div>
        <div class="custom-row">
          <input class="in" type="number" min="1" inputmode="numeric" placeholder="Custom sats" bind:value={custom} disabled={busy} />
          <button class="btn btn-primary btn-sm" onclick={() => zapNow(Number(custom))} disabled={busy || !(Number(custom) > 0)}>Zap</button>
        </div>
        <input class="in" type="text" maxlength="120" placeholder="Comment (optional)" bind:value={comment} disabled={busy} />
        {#if busy}<p class="msg">Creating invoice…</p>{/if}
        {#if error}<p class="msg err">⚠ {error}</p>{/if}
        <button class="cancel" onclick={() => (open = false)}>Cancel</button>
      </div>
    </div>
  {/if}
{/if}

<style>
  /* Small inline zap chip — sits next to the DJ name. */
  .zap-mini {
    display: inline-flex;
    align-items: center;
    gap: 0.2rem;
    flex: 0 0 auto;
    border: none;
    border-radius: 999px;
    padding: 0.12rem 0.45rem;
    font-size: 0.78rem;
    font-weight: 800;
    color: #1a1205;
    cursor: pointer;
    background: linear-gradient(95deg, var(--amber), #ffd24a);
    animation: zap-pulse 1.6s ease-in-out infinite;
  }
  .zap-mini:disabled {
    opacity: 0.5;
    cursor: not-allowed;
    animation: none;
  }
  @keyframes zap-pulse {
    0%,
    100% {
      box-shadow: 0 0 0 0 rgba(255, 154, 31, 0.5), 0 0 6px rgba(255, 154, 31, 0.4);
    }
    50% {
      box-shadow: 0 0 0 5px rgba(255, 154, 31, 0), 0 0 14px rgba(255, 154, 31, 0.7);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .zap-mini {
      animation: none;
    }
  }
  .bolt {
    font-size: 0.82rem;
    line-height: 1;
  }
  .lbl {
    font-size: 0.74rem;
    font-weight: 800;
  }
  .score {
    font-size: 0.72rem;
    font-weight: 700;
    border-left: 1px solid rgba(0, 0, 0, 0.25);
    padding-left: 0.3rem;
    margin-left: 0.05rem;
  }
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 200;
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
    font-size: 1.05rem;
  }
  .presets {
    display: flex;
    gap: 0.4rem;
  }
  .amt {
    flex: 1;
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text);
    border-radius: var(--radius-sm);
    padding: 0.6rem 0.2rem;
    font-weight: 700;
    cursor: pointer;
  }
  .amt:hover:not(:disabled) {
    border-color: var(--amber);
    color: var(--amber);
  }
  .custom-row {
    display: flex;
    gap: 0.4rem;
  }
  .in {
    flex: 1;
    min-width: 0;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.7rem;
    color: var(--text);
    font-size: 0.88rem;
  }
  .in:focus {
    outline: none;
    border-color: var(--accent-2);
  }
  .msg {
    margin: 0;
    font-size: 0.82rem;
    color: var(--text-dim);
  }
  .msg.err {
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

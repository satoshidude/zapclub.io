<script lang="ts">
  import { sync } from '../../nostr/sync.svelte'
  import { requestZapInvoice, zaps, creditZap, recordMyZap } from '../../nostr/zaps.svelte'
  import { showPay } from '../../nostr/payModal.svelte'
  import { publishZapBroadcast } from '../../nostr/groups'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import { loadNwcConnection } from '../../nostr/premium.svelte'

  // Optional explicit recipient (e.g. the club owner). Defaults to the live DJ.
  // `club` lets a confirmed payment broadcast the zap to the room (kind 20101).
  // `showDj` renders the recipient DJ's avatar + name on the chip (the zap target).
  let { pubkey = '', club = '', showDj = false }: { pubkey?: string; club?: string; showDj?: boolean } = $props()

  const PRESETS = [21, 100, 500, 2100]
  // Fallback payee when the recipient has no lightning address on their profile. The
  // vote/score still belongs to them (p-tag = their pubkey).
  const FALLBACK_LUD16 = 'zapclub@nsnip.io'

  const np = $derived(sync.live)
  const dj = $derived(pubkey || np?.dj || '')
  const djProfile = $derived(dj ? useProfile(dj) : null)
  const lud16 = $derived((djProfile?.lud16 as string) || FALLBACK_LUD16)
  const isSelf = $derived(!!dj && dj === auth.pubkey)
  const show = $derived(!!dj && !isSelf)
  // Total sats this DJ has received in zaps (all-time, from 9735 receipts).
  const total = $derived(dj ? zaps.score(dj) : 0)

  // The score is fed by ClubView's single per-club zap subscription (stage DJs + owner) —
  // this component only reads zaps.score(dj), it does not open its own subscription.

  let open = $state(false)
  let comment = $state('')
  let custom = $state('')
  let busy = $state(false)
  let error = $state('')
  let nwcPaid = $state(false)

  async function zapNow(sats: number) {
    if (busy || sats <= 0) return
    busy = true
    error = ''
    try {
      const { invoice, verify } = await requestZapInvoice(dj, lud16, sats, comment.trim())
      const connStr = loadNwcConnection()
      if (connStr) {
        // NWC connected → pay automatically, no modal
        const { NWCClient } = await import('@getalby/sdk/nwc')
        const client = new NWCClient({ nostrWalletConnectUrl: connStr })
        try {
          await client.payInvoice({ invoice })
          creditZap(dj, sats, invoice)
          recordMyZap(dj, sats)
          if (club && auth.canSign) void publishZapBroadcast(club, dj, sats, invoice)
          open = false
          comment = ''
          custom = ''
          nwcPaid = true
          setTimeout(() => (nwcPaid = false), 2000)
        } finally {
          client.close()
        }
      } else {
        open = false
        comment = ''
        custom = ''
        showPay(invoice, sats, `Zap ${displayName(dj, djProfile)}`, { verify, dj, club })
      }
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }
</script>

{#if show}
  <button class="zap-mini" class:with-dj={showDj} class:nwc-paid={nwcPaid} onclick={() => nwcPaid ? null : (open = !open)} title="Zap {displayName(dj, djProfile)}">
    <span class="bolt">⚡</span>
    {#if nwcPaid}
      <span class="lbl">Paid!</span>
    {:else if showDj}
      <img class="zap-av" src={avatarUrl(dj, djProfile)} alt="" width="16" height="16" />
      <span class="lbl dj-name">{displayName(dj, djProfile)}</span>
    {:else}
      <span class="lbl">zap</span>
      <span class="lbl dj-name divided">{displayName(dj, djProfile)}</span>
    {/if}
    {#if !nwcPaid && total > 0}
      <span class="score">{total >= 1000 ? `${(total / 1000).toFixed(1)}k` : total}</span>
    {/if}
  </button>

  {#if open}
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <div class="backdrop" role="presentation" onclick={() => (open = false)}>
      <div class="sheet" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => e.stopPropagation()}>
        <h3>⚡ Zap {displayName(dj, djProfile)}</h3>
        {#if loadNwcConnection()}
          <p class="nwc-hint">⚡ NWC connected — pays instantly</p>
        {/if}
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
  /* Quiet amber-outline pill — present but not shouting. No animation, no score. */
  .zap-mini {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    flex: 0 0 auto;
    border: 1px solid rgba(255, 178, 64, 0.45);
    border-radius: 999px;
    padding: 0.32rem 0.85rem;
    min-height: 36px;
    font-size: 0.85rem;
    font-weight: 700;
    color: var(--amber);
    cursor: pointer;
    background: rgba(255, 154, 31, 0.08);
    transition: background 0.15s ease, border-color 0.15s ease;
  }
  .zap-mini:hover:not(:disabled) {
    background: rgba(255, 154, 31, 0.18);
    border-color: var(--amber);
  }
  .zap-mini:active:not(:disabled) {
    background: rgba(255, 154, 31, 0.26);
  }
  .zap-mini:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .zap-mini.nwc-paid {
    border-color: #4ec94e;
    color: #4ec94e;
    background: rgba(78, 201, 78, 0.12);
    cursor: default;
  }
  .bolt {
    font-size: 0.95rem;
    line-height: 1;
  }
  .lbl {
    font-size: 0.82rem;
    font-weight: 700;
  }
  .zap-av {
    width: 18px;
    height: 18px;
    border-radius: 999px;
    object-fit: cover;
    background: rgba(0, 0, 0, 0.18);
  }
  .dj-name {
    max-width: 130px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .divided {
    border-left: 1px solid rgba(255, 178, 64, 0.45);
    padding-left: 0.45rem;
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
  .nwc-hint {
    margin: 0;
    font-size: 0.72rem;
    color: #4ec94e;
    font-weight: 600;
  }
  .cancel {
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 0.85rem;
    padding: 0.3rem;
  }
  .score {
    font-size: 0.72rem;
    font-weight: 700;
    color: var(--amber);
    border-left: 1px solid rgba(255, 178, 64, 0.35);
    padding-left: 0.4rem;
    margin-left: 0.05rem;
  }
</style>

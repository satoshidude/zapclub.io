<script lang="ts">
  import { sync } from '../../nostr/sync.svelte'
  import { requestZapInvoice, zaps } from '../../nostr/zaps.svelte'
  import { showPay } from '../../nostr/payModal.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { auth } from '../../nostr/auth.svelte'

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
      showPay(invoice, sats, `Zap ${displayName(dj, djProfile)}`, { verify, dj, club })
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }
</script>

{#if show}
  <button class="zap-mini" class:with-dj={showDj} onclick={() => (open = !open)} title="Zap {displayName(dj, djProfile)} · {total} sats received">
    <span class="bolt">⚡</span>
    {#if showDj}
      <img class="zap-av" src={avatarUrl(dj, djProfile)} alt="" width="16" height="16" />
      <span class="lbl dj-name">{displayName(dj, djProfile)}</span>
    {:else}
      <span class="lbl">zap</span>
    {/if}
    {#if total > 0}<span class="score">{fmtSats(total)} sats</span>{/if}
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
  /* The club's primary tip action — a big golden pill with a glow pulse and a shine sweep. */
  .zap-mini {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    flex: 0 0 auto;
    border: 1px solid rgba(255, 220, 120, 0.6);
    border-radius: 999px;
    padding: 0.5rem 1.1rem;
    min-height: 44px;
    font-size: 0.95rem;
    font-weight: 800;
    color: #1a1205;
    cursor: pointer;
    background: linear-gradient(110deg, #ffb347, var(--amber) 45%, #ffd24a);
    box-shadow: 0 2px 12px rgba(255, 154, 31, 0.35), inset 0 1px 0 rgba(255, 255, 255, 0.45);
    overflow: hidden;
    animation: zap-pulse 1.8s ease-in-out infinite;
    transition: transform 0.15s ease, box-shadow 0.15s ease, filter 0.15s ease;
  }
  /* Periodic shine sweeping across the pill. */
  .zap-mini::after {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    left: -60%;
    width: 38%;
    background: linear-gradient(105deg, transparent, rgba(255, 255, 255, 0.55), transparent);
    transform: skewX(-20deg);
    animation: zap-shine 3.4s ease-in-out infinite;
    pointer-events: none;
  }
  .zap-mini:hover:not(:disabled) {
    transform: translateY(-1px) scale(1.03);
    box-shadow: 0 4px 18px rgba(255, 154, 31, 0.55), inset 0 1px 0 rgba(255, 255, 255, 0.45);
    filter: brightness(1.05);
  }
  .zap-mini:active:not(:disabled) {
    transform: scale(0.97);
  }
  .zap-mini:disabled {
    opacity: 0.5;
    cursor: not-allowed;
    animation: none;
  }
  .zap-mini:disabled::after {
    animation: none;
  }
  @keyframes zap-pulse {
    0%,
    100% {
      box-shadow: 0 0 0 0 rgba(255, 154, 31, 0.5), 0 0 8px rgba(255, 154, 31, 0.4),
        inset 0 1px 0 rgba(255, 255, 255, 0.45);
    }
    50% {
      box-shadow: 0 0 0 7px rgba(255, 154, 31, 0), 0 0 20px rgba(255, 154, 31, 0.75),
        inset 0 1px 0 rgba(255, 255, 255, 0.45);
    }
  }
  @keyframes zap-shine {
    0%,
    55% {
      left: -60%;
    }
    85%,
    100% {
      left: 130%;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .zap-mini,
    .zap-mini::after {
      animation: none;
    }
    /* Static glow so the button still reads as the primary action without motion. */
    .zap-mini {
      box-shadow: 0 0 14px rgba(255, 154, 31, 0.55), inset 0 1px 0 rgba(255, 255, 255, 0.45);
    }
  }
  .bolt {
    font-size: 1.15rem;
    line-height: 1;
    filter: drop-shadow(0 1px 1px rgba(0, 0, 0, 0.25));
  }
  .lbl {
    font-size: 0.88rem;
    font-weight: 800;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .zap-av {
    width: 20px;
    height: 20px;
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
  .score {
    font-size: 0.85rem;
    font-weight: 700;
    border-left: 1px solid rgba(0, 0, 0, 0.25);
    padding-left: 0.45rem;
    margin-left: 0.1rem;
    font-variant-numeric: tabular-nums;
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

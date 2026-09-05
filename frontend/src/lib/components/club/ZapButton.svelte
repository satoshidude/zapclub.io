<script lang="ts">
  import { sync } from '../../nostr/sync.svelte'
  import { requestZapInvoice, zaps } from '../../nostr/zaps.svelte'
  import { showPay } from '../../nostr/payModal.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { auth } from '../../nostr/auth.svelte'

  // Optional explicit recipient (e.g. the club owner). Defaults to the live DJ.
  // `club` lets a confirmed payment broadcast the zap to the room (kind 20101).
  // `showDj` renders the recipient DJ's avatar + name on the chip (the zap target).
  let { pubkey = '', club = '', showDj = false, iconOnly = false, showName = false, showSelf = false }: { pubkey?: string; club?: string; showDj?: boolean; iconOnly?: boolean; showName?: boolean; showSelf?: boolean } = $props()

  const PRESETS = [21, 100, 500, 2100]
  // Fallback payee when the recipient has no lightning address on their profile. The
  // vote/score still belongs to them (p-tag = their pubkey).
  const FALLBACK_LUD16 = 'zapclub@nsnip.io'

  const np = $derived(sync.live)
  const dj = $derived(pubkey || np?.dj || '')
  const djProfile = $derived(dj ? useProfile(dj) : null)
  const lud16 = $derived((djProfile?.lud16 as string) || FALLBACK_LUD16)
  const isSelf = $derived(!!dj && dj === auth.pubkey)
  const show = $derived(!!dj && (showSelf || !isSelf))
  // Total sats this DJ has received in zaps (all-time, from 9735 receipts).
  const total = $derived(dj ? zaps.score(dj) : 0)

  // The score is fed by ClubView's single per-club zap subscription (stage DJs + owner) —
  // this component only reads zaps.score(dj), it does not open its own subscription.

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
  <button
    class="zap-mini"
    class:with-dj={showDj}
    class:icon-only={iconOnly}
    class:with-name={iconOnly && showName}
    class:self-label={isSelf && showSelf}
    onclick={(event) => {
      event.stopPropagation()
      if (!isSelf) open = !open
    }}
    title={isSelf ? displayName(dj, djProfile) : `Zap ${displayName(dj, djProfile)}`}
    aria-label={isSelf ? `Current DJ: ${displayName(dj, djProfile)}` : 'Zap the DJ'}
  >
    {#if iconOnly}
      <svg class="bolt-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M13.2 2 5.5 13h5.7L10.8 22l7.7-11h-5.7L13.2 2z"></path></svg>
      {#if showName}<span class="icon-dj-name lcd-card-title">{displayName(dj, djProfile)}</span>{/if}
    {:else}
      <span class="bolt">⚡</span>
      {#if showDj}
        <img class="zap-av" src={avatarUrl(dj, djProfile)} alt="" width="16" height="16" />
        <span class="lbl dj-name">{displayName(dj, djProfile)}</span>
      {:else}
        <span class="lbl">zap</span>
        <span class="lbl dj-name divided">{displayName(dj, djProfile)}</span>
      {/if}
      {#if total > 0}
        <span class="score">{total >= 1000 ? `${(total / 1000).toFixed(1)}k` : total}</span>
      {/if}
    {/if}
  </button>

  {#if open}
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <div class="backdrop led-modal-backdrop" role="presentation" onclick={() => (open = false)}>
      <div class="sheet led-modal-sheet" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => e.stopPropagation()}>
        <h3 class="led-modal-title">
          <span class="dialog-kicker">⚡ ZAP THE DJ</span>
          <span class="dialog-name">{displayName(dj, djProfile)}</span>
        </h3>
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
  .zap-mini.icon-only {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    min-height: 0;
    padding: 2px;
    border: 0;
    border-radius: 0;
    color: currentColor;
    background: transparent;
  }
  .zap-mini.icon-only:hover:not(:disabled),
  .zap-mini.icon-only:active:not(:disabled) {
    color: #fff;
    background: transparent;
  }
  .zap-mini.icon-only.with-name {
    display: inline-flex;
    justify-content: flex-start;
    gap: 0.3rem;
    width: auto;
    max-width: 128px;
  }
  .zap-mini.icon-only.self-label {
    cursor: default;
  }
  .icon-dj-name {
    max-width: 96px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bolt-icon {
    width: 23px;
    height: 23px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
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
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.35rem;
  }
  .dialog-kicker {
    color: var(--amber);
    font-size: 0.76rem;
    font-weight: 700;
    letter-spacing: 0.08em;
  }
  .dialog-name {
    max-width: 100%;
    overflow: hidden;
    color: var(--text);
    font-size: 1.5rem;
    font-weight: 700;
    line-height: 1.15;
    text-overflow: ellipsis;
    white-space: nowrap;
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
  .score {
    font-size: 0.72rem;
    font-weight: 700;
    color: var(--amber);
    border-left: 1px solid rgba(255, 178, 64, 0.35);
    padding-left: 0.4rem;
    margin-left: 0.05rem;
  }
</style>

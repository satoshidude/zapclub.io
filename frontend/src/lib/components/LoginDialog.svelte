<script lang="ts">
  import qrcode from 'qrcode-generator'
  import { loginDialog, closeLoginDialog } from '../nostr/loginDialog.svelte'
  import { createAccount, loginExtension, loginBunker, loginNsec, startNostrConnect } from '../nostr/nostrLogin'

  // Only offer the NIP-07 extension (Alby/nos2x/Nostash) if present → hide on iOS/Safari.
  const hasExtension = typeof window !== 'undefined' && !!window.nostr

  let view = $state<'main' | 'bunker' | 'nsec'>('main')
  let busy = $state(false)
  let error = $state('')
  let bunkerInput = $state('')
  let nsecInput = $state('')
  let connectUri = $state('')
  let copied = $state(false)

  async function copyUri() {
    try {
      await navigator.clipboard.writeText(connectUri)
      copied = true
      setTimeout(() => (copied = false), 1500)
    } catch {
      /* ignore */
    }
  }

  function doNsec() {
    if (!nsecInput.trim()) return
    busy = true
    error = ''
    try {
      loginNsec(nsecInput)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }

  function openBunker() {
    view = 'bunker'
    error = ''
    try {
      const { uri, done } = startNostrConnect()
      connectUri = uri
      done.catch(() => {
        /* timeout/cancel — user can paste bunker:// */
      })
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    }
  }

  const qrSrc = $derived.by(() => {
    if (!connectUri) return ''
    try {
      const qr = qrcode(0, 'M')
      qr.addData(connectUri)
      qr.make()
      return qr.createDataURL(4, 8)
    } catch {
      return ''
    }
  })

  async function doExtension() {
    busy = true
    error = ''
    try {
      await loginExtension()
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }

  async function doBunker() {
    if (!bunkerInput.trim()) return
    busy = true
    error = ''
    try {
      await loginBunker(bunkerInput)
    } catch (e) {
      error = String((e as Error)?.message ?? e)
    } finally {
      busy = false
    }
  }

  function close() {
    view = 'main'
    error = ''
    bunkerInput = ''
    connectUri = ''
    closeLoginDialog()
  }
</script>

<svelte:window onkeydown={(e) => loginDialog.open && e.key === 'Escape' && close()} />

{#if loginDialog.open}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="backdrop" role="presentation" onclick={close}>
    <div class="sheet" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => e.stopPropagation()}>
      {#if view === 'main'}
        <h3>Sign in to zapclub</h3>
        <button class="btn btn-primary big" onclick={createAccount} disabled={busy}>
          ✨ Create a new account
        </button>
        <button class="btn btn-ghost big" onclick={openBunker} disabled={busy}>
          🔐 Connect a remote signer (bunker)
        </button>
        <button class="btn btn-ghost big" onclick={() => { view = 'nsec'; error = '' }} disabled={busy}>
          🔑 Use a private key (nsec)
        </button>
        {#if hasExtension}
          <button class="btn btn-ghost big" onclick={doExtension} disabled={busy}>
            🧩 Browser extension (NIP-07)
          </button>
        {/if}
        <p class="hint">No email, no password. Your Nostr key is your identity.</p>
      {:else if view === 'bunker'}
        <button class="back" onclick={() => (view = 'main')}>← Back</button>
        <h3>Connect a remote signer</h3>
        {#if qrSrc}
          <a class="qr-link" href={connectUri}>
            <img class="qr" src={qrSrc} alt="QR" width="200" height="200" />
          </a>
          <p class="hint">Scan with your signer app, or paste a bunker:// link below.</p>
          <button class="copy-uri" onclick={copyUri} title={connectUri}>
            {copied ? '✓ Copied' : 'Copy connection link'}
          </button>
        {/if}
        <div class="or">or</div>
        <input
          class="bunker-in"
          bind:value={bunkerInput}
          placeholder="bunker://…"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
        />
        <button class="btn btn-primary big" onclick={doBunker} disabled={busy || !bunkerInput.trim()}>
          Connect
        </button>
      {:else}
        <button class="back" onclick={() => (view = 'main')}>← Back</button>
        <h3>Use a private key</h3>
        <p class="hint">Your nsec stays in this browser. Never paste it on sites you don't trust.</p>
        <input
          class="bunker-in"
          type="password"
          bind:value={nsecInput}
          placeholder="nsec1…"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
        />
        <button class="btn btn-primary big" onclick={doNsec} disabled={busy || !nsecInput.trim()}>
          Sign in
        </button>
      {/if}

      {#if error}<p class="err">⚠ {error}</p>{/if}
      <button class="btn btn-ghost cancel" onclick={close}>Cancel</button>
    </div>
  </div>
{/if}

<style>
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
    max-width: 360px;
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.3rem;
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.55);
  }
  h3 {
    margin: 0 0 0.3rem;
    font-size: 1.1rem;
    text-align: center;
  }
  .big {
    width: 100%;
    padding: 0.8rem;
    font-size: 1rem;
  }
  .hint {
    margin: 0.2rem 0 0;
    font-size: 0.78rem;
    color: var(--text-dim);
    text-align: center;
  }
  .back {
    align-self: flex-start;
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    padding: 0;
    font-size: 0.85rem;
  }
  .qr-link {
    align-self: center;
  }
  .qr {
    width: 200px;
    height: 200px;
    border-radius: 8px;
    background: #fff;
    padding: 6px;
    display: block;
  }
  .copy-uri {
    background: var(--bg-elev-2);
    border: 1px solid var(--border);
    color: var(--text-dim);
    border-radius: var(--radius-sm);
    padding: 0.45rem 0.7rem;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .copy-uri:hover {
    border-color: var(--accent-2);
    color: var(--text);
  }
  .or {
    text-align: center;
    font-size: 0.75rem;
    color: var(--text-dim);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .bunker-in {
    width: 100%;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.6rem 0.7rem;
    color: var(--text);
    font-size: 0.85rem;
  }
  .bunker-in:focus {
    outline: none;
    border-color: var(--accent-2);
  }
  .err {
    margin: 0;
    font-size: 0.8rem;
    color: var(--danger);
  }
  .cancel {
    width: 100%;
  }
</style>

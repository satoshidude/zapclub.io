<script lang="ts">
  import qrcode from 'qrcode-generator'
  import jsQR from 'jsqr'
  import { tick, onDestroy } from 'svelte'
  import { loginDialog, closeLoginDialog } from '../nostr/loginDialog.svelte'
  import { createAccount, loginExtension, loginBunker, loginNsec, startNostrConnect } from '../nostr/nostrLogin'

  // NIP-07 providers inject window.nostr. On Safari (Nostash) it often appears LATE — after
  // page load, or only once the extension is granted access to the site — so detect it
  // reactively (poll while the dialog is open) AND always offer it on Apple/Safari, where the
  // click waits for a late-injected provider (loginExtension polls too).
  let hasExtension = $state(typeof window !== 'undefined' && !!window.nostr)
  const isAppleSafari =
    typeof navigator !== 'undefined' &&
    /iPhone|iPad|iPod|Macintosh/.test(navigator.userAgent) &&
    /Safari/.test(navigator.userAgent) &&
    !/Chrome|Chromium|Edg|OPR|CriOS|FxiOS/.test(navigator.userAgent)
  $effect(() => {
    if (!loginDialog.open || hasExtension) return
    const iv = setInterval(() => {
      if (typeof window !== 'undefined' && window.nostr) hasExtension = true
    }, 400)
    return () => clearInterval(iv)
  })
  const showExtension = $derived(hasExtension || isAppleSafari)

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

  // Camera QR scan: read a bunker:// (or nostrconnect://) connection QR with the device camera.
  // jsQR decodes each video frame (BarcodeDetector isn't on Safari/iOS, where this matters most).
  let scanning = $state(false)
  let scanError = $state('')
  let videoEl = $state<HTMLVideoElement>()
  let stream: MediaStream | null = null
  let rafId = 0

  async function startScan() {
    scanError = ''
    try {
      stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } })
    } catch {
      scanError = 'Camera unavailable or permission denied'
      return
    }
    scanning = true
    await tick() // let the <video> render so we can bind the stream
    if (!videoEl) {
      stopScan()
      return
    }
    videoEl.srcObject = stream
    videoEl.setAttribute('playsinline', '')
    await videoEl.play().catch(() => {})
    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('2d', { willReadFrequently: true })
    const loop = () => {
      if (!scanning || !videoEl || !ctx) return
      if (videoEl.readyState >= 2 && videoEl.videoWidth > 0) {
        canvas.width = videoEl.videoWidth
        canvas.height = videoEl.videoHeight
        ctx.drawImage(videoEl, 0, 0, canvas.width, canvas.height)
        const img = ctx.getImageData(0, 0, canvas.width, canvas.height)
        const code = jsQR(img.data, img.width, img.height)
        if (code && code.data) {
          onScanned(code.data)
          return
        }
      }
      rafId = requestAnimationFrame(loop)
    }
    rafId = requestAnimationFrame(loop)
  }

  function stopScan() {
    scanning = false
    if (rafId) cancelAnimationFrame(rafId)
    rafId = 0
    if (stream) {
      stream.getTracks().forEach((t) => t.stop())
      stream = null
    }
  }

  function onScanned(data: string) {
    stopScan()
    bunkerInput = data.trim()
    // A bunker:// (or nostrconnect://) link connects right away; anything else just fills the field.
    if (/^(bunker|nostrconnect):\/\//i.test(bunkerInput)) void doBunker()
  }

  onDestroy(stopScan)

  function close() {
    stopScan()
    view = 'main'
    error = ''
    scanError = ''
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
        {#if showExtension}
          <button class="btn btn-ghost big" onclick={doExtension} disabled={busy}>
            🧩 Browser extension (NIP-07)
          </button>
          {#if isAppleSafari && !hasExtension}
            <p class="hint">Using Nostash on Safari? Open its icon and allow it for this site, then tap above.</p>
          {/if}
        {/if}
        <p class="hint">No email, no password. Your Nostr key is your identity.</p>
      {:else if view === 'bunker'}
        <button class="back" onclick={() => { stopScan(); view = 'main' }}>← Back</button>
        <h3>Connect a remote signer</h3>
        {#if scanning}
          <!-- svelte-ignore a11y_media_has_caption -->
          <video class="scan-video" bind:this={videoEl} muted autoplay></video>
          <p class="hint">Point the camera at your signer's bunker QR.</p>
          {#if scanError}<p class="err">⚠ {scanError}</p>{/if}
          <button class="btn btn-ghost big" onclick={stopScan}>Cancel scan</button>
        {:else}
          {#if qrSrc}
            <a class="qr-link" href={connectUri}>
              <img class="qr" src={qrSrc} alt="QR" width="160" height="160" />
            </a>
            <p class="hint">Scan with your signer app, or paste / scan a bunker:// link below.</p>
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
          <button class="btn btn-ghost big" onclick={startScan} disabled={busy}>
            📷 Scan bunker QR with camera
          </button>
          {#if scanError}<p class="err">⚠ {scanError}</p>{/if}
        {/if}
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
  .scan-video {
    width: 100%;
    max-width: 280px;
    aspect-ratio: 1;
    align-self: center;
    object-fit: cover;
    border-radius: var(--radius-sm);
    background: #000;
    border: 1px solid var(--border);
  }
  .qr {
    width: 160px;
    height: 160px;
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
    margin-top: 0.6rem;
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

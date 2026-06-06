import type { Event, EventTemplate } from 'nostr-tools/pure'
import { decode } from 'nostr-tools/nip19'
import { minePow } from 'nostr-tools/nip13'
import { AccountManager } from 'applesauce-accounts'
import {
  ExtensionAccount,
  PrivateKeyAccount,
  NostrConnectAccount,
  registerCommonAccountTypes,
} from 'applesauce-accounts/accounts'
import { NostrConnectSigner } from 'applesauce-signers'
import { RelayPool } from 'applesauce-relay'
import { auth, setLoggedIn, setLoggedOut, setProfile, setProfileLoading } from './auth.svelte'
import { fetchProfile } from './pool'
import { goHome } from '../router.svelte'
import { openLoginDialog, closeLoginDialog } from './loginDialog.svelte'
import { resetSync } from './sync.svelte'
import { resetStage } from './stage.svelte'
import { resetQueues } from './queue.svelte'
import { resetChat } from './chat.svelte'
import { resetPlaylists } from './playlists.svelte'
import { resetZaps } from './zaps.svelte'
import type { LoginMethod } from './types'

// ── applesauce: account manager + signer wiring ─────────────────────────────
const STORAGE_KEY = 'zapclub:accounts'

// Dedicated relay pool ONLY for NIP-46 (Bunker) — applesauce uses RxJS. The rest of
// the app traffic keeps going through the nostr-tools pool (pool.ts).
const nip46Pool = new RelayPool()
NostrConnectSigner.subscriptionMethod = (relays, filters) => nip46Pool.subscription(relays, filters)
NostrConnectSigner.publishMethod = (relays, event) => nip46Pool.publish(relays, event)

// Relay for client-initiated connection (nostrconnect://, QR). bunker:// brings its
// own relays. Widely supported NIP-46 relay.
const NIP46_RELAY = 'wss://relay.nsec.app'

const manager = new AccountManager()
registerCommonAccountTypes(manager)

/** Clears all user-bound session state. On logout AND on user switch. */
function resetSession(): void {
  resetSync()
  resetStage()
  resetQueues()
  resetChat()
  resetPlaylists()
  resetZaps()
  goHome()
}

function methodOf(type: string | undefined): LoginMethod {
  switch (type) {
    case 'extension':
      return 'extension'
    case 'nostr-connect':
      return 'connect'
    case 'nsec':
      return 'nstart' // local key (in-browser)
    default:
      return 'connect'
  }
}

async function loadProfile(pubkey: string): Promise<void> {
  setProfileLoading(true)
  try {
    setProfile(await fetchProfile(pubkey))
  } catch (e) {
    console.warn('[profile] load failed', e)
    setProfile(null)
  } finally {
    setProfileLoading(false)
  }
}

function persist(): void {
  try {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ accounts: manager.toJSON(), active: manager.active?.pubkey ?? null }),
    )
  } catch {
    /* ignore */
  }
}

// ── Lightweight "I'm logged in" session (decouples UI login from signer restore) ──
// Proven against iOS-Safari reload-logout: the UI counts as logged in IMMEDIATELY from
// this {pubkey, method}, regardless of whether/when applesauce restores account+signer.
const LITE_KEY = 'zapclub:session'
let intentionalLogout = false

function writeLite(pubkey: string, method: LoginMethod): void {
  try {
    localStorage.setItem(LITE_KEY, JSON.stringify({ pubkey, method }))
  } catch {
    /* ignore */
  }
}
function readLite(): { pubkey: string; method: LoginMethod } | null {
  try {
    const raw = localStorage.getItem(LITE_KEY)
    if (raw) {
      const o = JSON.parse(raw)
      if (o && typeof o.pubkey === 'string') return o
    }
  } catch {
    /* ignore */
  }
  return null
}
function clearLite(): void {
  try {
    localStorage.removeItem(LITE_KEY)
  } catch {
    /* ignore */
  }
}

let started = false

/** Once at app start: restore accounts + mirror the active account to the auth store. */
export function initAuth(): void {
  if (started) return
  started = true

  // 1. Show UI as logged in IMMEDIATELY from the lite session (signer-independent) —
  //    this prevents the iOS-Safari reload-logout even if the applesauce restore lags.
  const lite = readLite()
  if (lite) {
    setLoggedIn(lite.pubkey, lite.method)
    void loadProfile(lite.pubkey)
  }

  // 2. Restore applesauce accounts (for signing). Pick active by pubkey (more stable
  //    than id), fallback: first account.
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const { accounts, active } = JSON.parse(raw) as { accounts: unknown[]; active: string | null }
      manager.fromJSON(accounts as Parameters<typeof manager.fromJSON>[0])
      const want = active ?? lite?.pubkey ?? null
      const acc = (want && manager.accounts.find((a) => a.pubkey === want)) || manager.accounts[0]
      if (acc) manager.setActive(acc)
      // Wake the bunker IMMEDIATELY: a restored NIP-46 signer can only sign after a
      // connect() round-trip. Without this the first sign after reload hangs until
      // timeout. Connect proactively in the background.
      warmSigner()
    }
  } catch (e) {
    console.warn('[auth] restore failed', e)
  }

  // 3. Mirror the active account → app state.
  manager.active$.subscribe((acc) => {
    if (!acc) {
      // Empty-init / failed restore is NOT a logout — otherwise you'd get kicked on
      // reload. Only a user-triggered logout (flag) counts.
      if (intentionalLogout) {
        intentionalLogout = false
        clearLite()
        setLoggedOut()
        resetSession()
      }
      persist()
      return
    }
    if (auth.pubkey && auth.pubkey !== acc.pubkey) resetSession()
    setLoggedIn(acc.pubkey, methodOf(acc.type))
    writeLite(acc.pubkey, methodOf(acc.type))
    void loadProfile(acc.pubkey)
    persist()
  })
  // Persist when the account list changes.
  manager.accounts$.subscribe(() => persist())
}

// ── Login actions (called from LoginDialog.svelte) ──────────────────────────

/** Waits for a NIP-07 provider to appear. Safari extensions (Nostash) inject `window.nostr`
 *  LATE — after page load and often only once the user grants the extension access to the
 *  site — so we poll briefly instead of failing immediately. */
async function waitForNostr(ms = 4000): Promise<void> {
  const start = Date.now()
  while (typeof window !== 'undefined' && !window.nostr && Date.now() - start < ms) {
    await sleep(200)
  }
  if (typeof window === 'undefined' || !window.nostr) {
    throw new Error(
      'No Nostr extension detected. In Safari, open the Nostash icon, allow it for this site, then try again.',
    )
  }
}

/** Browser extension (NIP-07, e.g. Alby/nos2x/Nostash). Waits for a late-injected provider
 *  (Safari/Nostash) before reading the key. */
export async function loginExtension(): Promise<void> {
  await waitForNostr()
  const acc = await ExtensionAccount.fromExtension()
  manager.addAccount(acc)
  manager.setActive(acc)
  closeLoginDialog()
}

/** New account: generate a key in the browser. Zero friction, iOS-friendly. */
export function createAccount(): void {
  const acc = PrivateKeyAccount.generateNew()
  manager.addAccount(acc)
  manager.setActive(acc)
  closeLoginDialog()
}

/** Sign in with an existing private key (nsec). Stored nip-49-capable in the browser. */
export function loginNsec(nsec: string): void {
  const { type, data } = decode(nsec.trim())
  if (type !== 'nsec') throw new Error('Not a valid nsec key')
  const acc = PrivateKeyAccount.fromKey(data as Uint8Array)
  manager.addAccount(acc)
  manager.setActive(acc)
  closeLoginDialog()
}

/** NIP-46 bunker via a `bunker://` string. */
export async function loginBunker(uri: string): Promise<void> {
  const signer = await NostrConnectSigner.fromBunkerURI(uri.trim(), {
    permissions: NostrConnectSigner.buildSigningPermissions([0, 1, 9, 9002, 9007, 9021, 9022]),
  })
  const pubkey = await signer.getPublicKey()
  const acc = new NostrConnectAccount(pubkey, signer)
  manager.addAccount(acc)
  manager.setActive(acc)
  closeLoginDialog()
}

/**
 * Client-initiated NIP-46 (nostrconnect://, QR/deeplink): returns the URI for the QR
 * + a promise that resolves once the signer app confirms the connection.
 */
export function startNostrConnect(): { uri: string; signer: NostrConnectSigner; done: Promise<void> } {
  const signer = new NostrConnectSigner({ relays: [NIP46_RELAY] })
  const uri = signer.getNostrConnectURI({ name: 'zapclub' })
  const done = signer.waitForSigner().then(async () => {
    const pubkey = await signer.getPublicKey()
    const acc = new NostrConnectAccount(pubkey, signer)
    manager.addAccount(acc)
    manager.setActive(acc)
    closeLoginDialog()
  })
  return { uri, signer, done }
}

// ── Public API ──────────────────────────────────────────────────────────────

/** Opens the login modal. */
export function launchLogin(): void {
  openLoginDialog()
}

/** Opens the login modal (signup entry is the "create account" button inside). */
export function launchSignup(): void {
  openLoginDialog()
}

export async function logout(): Promise<void> {
  // Mark a real, user-triggered logout → active$ undefined counts.
  intentionalLogout = true
  clearLite()
  const acc = manager.active
  if (acc) manager.removeAccount(acc) // active$ → setLoggedOut + resetSession
  else {
    setLoggedOut()
    resetSession()
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms))
}

/** Promise with a hard timeout — keeps a NIP-46 signer without an answer from hanging
 *  FOREVER (applesauce's makeRequest has no timeout of its own). */
function withTimeout<T>(p: Promise<T>, ms: number, label: string): Promise<T> {
  return new Promise((resolve, reject) => {
    const id = setTimeout(() => reject(new Error(`${label}: timeout after ${ms}ms`)), ms)
    p.then(
      (v) => {
        clearTimeout(id)
        resolve(v)
      },
      (e) => {
        clearTimeout(id)
        reject(e)
      },
    )
  })
}

/** Connect a restored bunker signer in the background (idempotent). */
function warmSigner(): void {
  const signer = manager.active?.signer
  if (signer instanceof NostrConnectSigner && !signer.isConnected) {
    withTimeout(signer.connect(), 15_000, 'bunker connect')
      .then(() => console.log('[auth] bunker connected'))
      .catch((e) => console.warn('[auth] bunker connect failed', e))
  }
}

/**
 * Signs via the active account. Two failure modes are handled:
 *  – Extension on Safari (window.nostr injected late) → quick retry.
 *  – NIP-46 bunker that must connect first after reload → the first sign triggers the
 *    connect() round-trip; a generous timeout per attempt, else it would hang forever.
 */
// NIP-13 anti-spam proof-of-work. Mine the Sybil-relevant kinds — club join (9021) and chat
// (9) — so mass/throwaway-key spam costs CPU. Bits are slightly above the relay's minimum
// (so the relay can be tuned up without redeploying the client). Mining is synchronous but
// brief at these difficulties (chat ~ms, join < ~1s) and runs once per such event.
const POW_BITS: Record<number, number> = { 9: 12, 9021: 18 }

/** Adds a mined NIP-13 nonce to join/chat events before signing (no-op for other kinds). The
 *  signer re-derives the same id from these exact fields, so the proof-of-work survives. */
function withPow(template: EventTemplate): EventTemplate {
  const bits = POW_BITS[template.kind]
  const pubkey = auth.pubkey
  if (!bits || !pubkey) return template
  const mined = minePow(
    { pubkey, created_at: template.created_at, kind: template.kind, tags: [...template.tags], content: template.content },
    bits,
  )
  return { kind: mined.kind, created_at: mined.created_at, tags: mined.tags, content: mined.content }
}

export async function signEvent(template: EventTemplate): Promise<Event> {
  if (!manager.active) throw new Error('No signer available — please sign in again.')
  template = withPow(template)
  let lastErr: unknown
  for (let i = 0; i < 4; i++) {
    try {
      return (await withTimeout(manager.signer.signEvent(template), 12_000, 'signEvent')) as Event
    } catch (e) {
      lastErr = e
      await sleep(300)
    }
  }
  throw lastErr instanceof Error ? lastErr : new Error('Signing failed')
}

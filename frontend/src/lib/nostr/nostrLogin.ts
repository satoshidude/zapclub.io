import type { Event, EventTemplate } from 'nostr-tools/pure'
import { decode } from 'nostr-tools/nip19'
import { AccountManager } from 'applesauce-accounts'
import {
  ExtensionAccount,
  PrivateKeyAccount,
  NostrConnectAccount,
  registerCommonAccountTypes,
} from 'applesauce-accounts/accounts'
import { ExtensionSigner, NostrConnectSigner } from 'applesauce-signers'
import { RelayPool } from 'applesauce-relay'
import {
  auth,
  setLoggedIn,
  setLoggedOut,
  setProfile,
  setProfileLoading,
  setSignerReady,
} from './auth.svelte'
import { closeClubRelay, fetchProfile } from './pool'
import { goHome } from '../router.svelte'
import { openLoginDialog, closeLoginDialog } from './loginDialog.svelte'
import { resetSync } from './sync.svelte'
import { resetStage, leaveStage } from './stage.svelte'
import { resetPresence } from './presence.svelte'
import { resetQueues } from './queue.svelte'
import { resetPlaylists } from './playlists.svelte'
import { resetZaps } from './zaps.svelte'
import { resetSessionSigner } from './sessionSigner'
import { resetClubAuthState } from './groups'
import { handoffSuccessfulNip07Login, resetAccountWatchState } from './accountWatch.svelte'
import {
  recordLogicalSignRequest,
  recordNip07GetPublicKeyCall,
  recordPhysicalSignEventCall,
} from './signingDiagnostics'
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

/**
 * Exact set of event kinds the browser can ask a remote signer to sign. Keeping this
 * allowlist in one place prevents restored bunker sessions and nostrconnect:// sessions
 * from silently falling back to a permission prompt for every background event.
 */
export const NIP46_SIGNING_KINDS = [
  0, // profile metadata
  1, // public share note
  5, // delete reaction / playlist
  7, // reaction
  9, // group chat
  9000, 9001, 9002, 9007, 9021, 9022, // NIP-29 membership and administration
  9734, // zap request
  20101, 20102, 20103, 20104, // live club interactions signed by the account key
  22242, // NIP-42 relay authentication
  24242, // Blossom authorization
  27235, // NIP-98 HTTP authorization
  30101, 30103, 30104, 30105, 30106, 30107, // persistent club state owned by users
] as const

export const NIP46_PERMISSIONS = NostrConnectSigner.buildSigningPermissions([
  ...NIP46_SIGNING_KINDS,
])

const NIP46_CONNECT_TIMEOUT_MS = 15_000
const SIGN_EVENT_TIMEOUT_MS = 30_000

const manager = new AccountManager()
registerCommonAccountTypes(manager)

/** Clears all user-bound session state. On logout AND on user switch. */
function resetSession(): void {
  resetSync()
  resetStage()
  resetPresence()
  resetQueues()
  resetPlaylists()
  resetZaps()
  resetAccountWatchState()
  resetSessionSigner()
  resetClubAuthState()
  // NIP-42 authentication belongs to one WebSocket connection. Never let a socket
  // authenticated as the previous account survive logout or an account switch.
  closeClubRelay()
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
    // A lite session proves identity only. Writes stay disabled until its account signer
    // has actually been restored and initialized below.
    setLoggedIn(lite.pubkey, lite.method, false)
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
    }
  } catch (e) {
    console.warn('[auth] restore failed', e)
  }

  // 3. Mirror the active account → app state.
  manager.active$.subscribe((acc) => {
    if (!acc) {
      setSignerReady(false)
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
    const ready = !(acc.signer instanceof NostrConnectSigner) || acc.signer.isConnected
    setLoggedIn(acc.pubkey, methodOf(acc.type), ready)
    writeLite(acc.pubkey, methodOf(acc.type))
    void loadProfile(acc.pubkey)
    if (!ready) {
      // Restore starts eagerly, while ensureSignerReady() below shares this exact
      // connection with the first operation instead of opening another one.
      void ensureSignerReady(acc, false).catch((e) => console.warn('[auth] bunker connect failed', e))
    }
    persist()
  })
  // Persist when the account list changes.
  manager.accounts$.subscribe(() => persist())
}

// ── Login actions (called from LoginDialog.svelte) ──────────────────────────

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

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
  const signer = new ExtensionSigner()
  recordNip07GetPublicKeyCall('nostrLogin')
  const pubkey = await signer.getPublicKey()
  const acc = new ExtensionAccount(pubkey, signer)
  manager.addAccount(acc)
  manager.setActive(acc)
  handoffSuccessfulNip07Login(pubkey)
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
    permissions: [...NIP46_PERMISSIONS],
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
  const uri = signer.getNostrConnectURI({ name: 'zapclub', permissions: [...NIP46_PERMISSIONS] })
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
  // Step off the stage WHILE the signer is still alive: removeAccount tears the signer down,
  // after which resetStage's best-effort `off` can't sign — the DJ would stay stuck on stage
  // for the full 5-minute stage lease. Bounded so a hung NIP-46 bunker can't block logout.
  await withTimeout(leaveStage(), 3000, 'logout: leave stage').catch(() => {})
  const acc = manager.active
  if (acc) manager.removeAccount(acc) // active$ → setLoggedOut + resetSession
  else {
    setLoggedOut()
    resetSession()
  }
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

type ManagedAccount = NonNullable<ReturnType<typeof manager.getActive>>

type Nip46FlightStatus = 'pending' | 'timed-out' | 'failed' | 'succeeded'
interface Nip46ConnectionFlight {
  generation: number
  status: Nip46FlightStatus
  promise: Promise<void>
}

// A failed/timed-out raw request stays latched. Only a later explicit signer operation may
// replace the signer instance and open one new flight; background warm-up/AUTH never retries it.
const nip46ConnectionFlights = new WeakMap<NostrConnectSigner, Nip46ConnectionFlight>()
const nip46ConnectionGenerations = new WeakMap<NostrConnectSigner, number>()

function currentNip46Flight(signer: NostrConnectSigner, flight: Nip46ConnectionFlight): boolean {
  return nip46ConnectionFlights.get(signer) === flight
    && nip46ConnectionGenerations.get(signer) === flight.generation
}

function activeSignerMatches(account: ManagedAccount, signer: NostrConnectSigner): boolean {
  return manager.active?.id === account.id && account.signer === signer
}

function startNip46Connection(
  account: ManagedAccount,
  signer: NostrConnectSigner,
  afterClose?: Promise<void>,
): Nip46ConnectionFlight {
  const generation = (nip46ConnectionGenerations.get(signer) ?? 0) + 1
  nip46ConnectionGenerations.set(signer, generation)
  const flight: Nip46ConnectionFlight = {
    generation,
    status: 'pending',
    promise: Promise.resolve(),
  }
  const connect = afterClose
    ? afterClose.then(() => signer.connect(undefined, [...NIP46_PERMISSIONS]))
    : signer.connect(undefined, [...NIP46_PERMISSIONS])
  flight.promise = connect
    .then(
      () => {
        // A timed-out or replaced flight may still complete inside applesauce. It must never
        // make the current account ready; only the current, still-pending generation may do so.
        if (!currentNip46Flight(signer, flight) || flight.status !== 'pending') return
        flight.status = 'succeeded'
        nip46ConnectionFlights.delete(signer)
        if (activeSignerMatches(account, signer)) {
          setSignerReady(true)
          console.log('[auth] bunker connected')
        }
      },
      (error) => {
        if (currentNip46Flight(signer, flight) && flight.status === 'pending') {
          flight.status = 'failed'
          if (activeSignerMatches(account, signer)) setSignerReady(false)
        }
        throw error
      },
    )
  nip46ConnectionFlights.set(signer, flight)
  // Warm-up deliberately does not own the lifetime of the raw promise.
  void flight.promise.catch(() => {})
  return flight
}

function restartNip46Connection(
  account: ManagedAccount,
  staleSigner: NostrConnectSigner,
): Nip46ConnectionFlight {
  nip46ConnectionFlights.delete(staleSigner)
  nip46ConnectionGenerations.set(staleSigner, (nip46ConnectionGenerations.get(staleSigner) ?? 0) + 1)
  const close = staleSigner.close()

  // applesauce close() does not clear its pending request map. Reusing that instance would let
  // a late old response mutate the new connection, so explicit recovery gets a fresh instance
  // with the same client key and connection metadata.
  const signer = new NostrConnectSigner({
    relays: [...staleSigner.relays],
    remote: staleSigner.remote,
    pubkey: account.pubkey,
    signer: staleSigner.signer,
    onAuth: staleSigner.onAuth,
  })
  account.signer = signer
  if (manager.active?.id === account.id) setSignerReady(false)
  return startNip46Connection(account, signer, close)
}

async function ensureSignerReady(account: ManagedAccount, allowExplicitReconnect: boolean): Promise<void> {
  let signer = account.signer
  if (!(signer instanceof NostrConnectSigner)) {
    if (manager.active?.id === account.id) setSignerReady(true)
    return
  }

  let flight = nip46ConnectionFlights.get(signer)
  if (flight && (flight.status === 'timed-out' || flight.status === 'failed')) {
    if (!allowExplicitReconnect) {
      throw new Error('Bunker reconnect requires an explicit user action')
    }
    flight = restartNip46Connection(account, signer)
    signer = account.signer as NostrConnectSigner
  }

  if (!flight && signer.isConnected) {
    if (manager.active?.id === account.id) setSignerReady(true)
    return
  }

  if (manager.active?.id === account.id) setSignerReady(false)
  if (!flight) flight = startNip46Connection(account, signer)

  try {
    await withTimeout(flight.promise, NIP46_CONNECT_TIMEOUT_MS, 'bunker connect')
  } catch (error) {
    if (
      error instanceof Error
      && error.message.includes('timeout')
      && currentNip46Flight(signer, flight)
      && flight.status === 'pending'
    ) {
      flight.status = 'timed-out'
      if (activeSignerMatches(account, signer)) setSignerReady(false)
    }
    throw error
  }
  if (manager.active?.id !== account.id) throw new Error('Active signer changed while connecting')
}

export type SignerFailureCode = 'timeout' | 'rejected' | 'unavailable' | 'invalid' | 'failed'

export class SignerOperationError extends Error {
  constructor(
    public readonly code: SignerFailureCode,
    message: string,
    cause?: unknown,
  ) {
    super(message, { cause })
    this.name = 'SignerOperationError'
  }
}

function classifySignerError(error: unknown): SignerOperationError {
  if (error instanceof SignerOperationError) return error
  const detail = error instanceof Error ? error.message : String(error)
  const normalized = detail.toLowerCase()

  if (normalized.includes('timeout')) {
    return new SignerOperationError(
      'timeout',
      'The signer did not answer in time. The request was not repeated.',
      error,
    )
  }
  if (/reject|denied|declin|cancel/.test(normalized)) {
    return new SignerOperationError('rejected', 'Signing was rejected in the signer.', error)
  }
  if (/no signer|no active|active signer changed|missing signer|extension missing|not connected|disconnect|closed|network/.test(normalized)) {
    return new SignerOperationError('unavailable', 'The signer is unavailable. Reconnect it and try again.', error)
  }
  if (/invalid event|wrong pubkey|mismatch|modified event/.test(normalized)) {
    return new SignerOperationError('invalid', 'The signer returned an invalid event or a different account.', error)
  }
  return new SignerOperationError('failed', `Signing failed: ${detail}`, error)
}

/** Signs once via the active account. A timed-out request may still complete remotely, so it
 * is never retried automatically; only a later explicit application action can try again. */
export async function signEvent(
  template: EventTemplate,
  options: { allowNip46Reconnect?: boolean } = {},
): Promise<Event> {
  recordLogicalSignRequest(template, 'nostrLogin')
  const account = manager.active
  if (!account) {
    throw new SignerOperationError('unavailable', 'No signer available — please sign in again.')
  }

  try {
    await ensureSignerReady(account, options.allowNip46Reconnect !== false)
    if (manager.active?.id !== account.id) throw new Error('Active signer changed before signing')
    recordPhysicalSignEventCall(template, 'nostrLogin')
    const signed = (await withTimeout(
      manager.signer.signEvent(template),
      SIGN_EVENT_TIMEOUT_MS,
      'signEvent',
    )) as Event
    if (signed.pubkey !== account.pubkey) {
      throw new Error('Signer returned an event for the wrong pubkey')
    }
    return signed
  } catch (error) {
    throw classifySignerError(error)
  }
}

/**
 * NIP-44 self-encryption: encrypt plaintext using own pubkey as recipient.
 * Works with NIP-07 extensions, nsec, and NIP-46 bunkers that expose nip44.
 * Throws if the active signer doesn't support NIP-44 (old extensions).
 */
export async function nip44EncryptSelf(plaintext: string): Promise<string> {
  const account = manager.active
  if (!account || !auth.pubkey) throw new Error('Not signed in')
  await ensureSignerReady(account, true)
  if (manager.active?.id !== account.id) throw new Error('Active signer changed before encryption')
  const n44 = manager.signer.nip44
  if (!n44) throw new Error('Signer does not support NIP-44 encryption')
  return await withTimeout(n44.encrypt(auth.pubkey, plaintext), 10_000, 'nip44Encrypt')
}

/**
 * NIP-44 self-decryption: decrypt ciphertext that was encrypted to own pubkey.
 */
export async function nip44DecryptSelf(ciphertext: string): Promise<string> {
  const account = manager.active
  if (!account || !auth.pubkey) throw new Error('Not signed in')
  await ensureSignerReady(account, true)
  if (manager.active?.id !== account.id) throw new Error('Active signer changed before decryption')
  const n44 = manager.signer.nip44
  if (!n44) throw new Error('Signer does not support NIP-44 decryption')
  return await withTimeout(n44.decrypt(auth.pubkey, ciphertext), 10_000, 'nip44Decrypt')
}

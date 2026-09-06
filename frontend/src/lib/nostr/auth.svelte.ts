import { npubEncode } from 'nostr-tools/nip19'
import type { ProfileMetadata, LoginMethod } from './types'

interface AuthState {
  pubkey: string | null
  npub: string | null
  method: LoginMethod
  signerReady: boolean
  profile: ProfileMetadata | null
  profileLoading: boolean
}

const state = $state<AuthState>({
  pubkey: null,
  npub: null,
  method: null,
  signerReady: false,
  profile: null,
  profileLoading: false,
})

export interface AuthSigningState {
  pubkey: string | null
  method: LoginMethod
  signerReady: boolean
  canSign: boolean
}

type AuthSigningStateListener = (state: Readonly<AuthSigningState>) => void
const signingStateListeners = new Set<AuthSigningStateListener>()

function signingStateSnapshot(): AuthSigningState {
  return {
    pubkey: state.pubkey,
    method: state.method,
    signerReady: state.signerReady,
    canSign: state.pubkey !== null && state.method !== 'readOnly' && state.signerReady,
  }
}

function notifySigningStateChanged(): void {
  const snapshot = signingStateSnapshot()
  for (const listener of [...signingStateListeners]) {
    try {
      listener(snapshot)
    } catch (error) {
      console.warn('[auth] signing-state listener failed', error)
    }
  }
}

/** Explicit module-level notification for non-Svelte consumers of signer readiness. */
export function onAuthSigningStateChange(listener: AuthSigningStateListener): () => void {
  signingStateListeners.add(listener)
  return () => signingStateListeners.delete(listener)
}

export const auth = {
  get pubkey() {
    return state.pubkey
  },
  get npub() {
    return state.npub
  },
  get method() {
    return state.method
  },
  get signerReady() {
    return state.signerReady
  },
  get profile() {
    return state.profile
  },
  get profileLoading() {
    return state.profileLoading
  },
  get isLoggedIn() {
    return state.pubkey !== null
  },
  /** True only when a writable account has an initialized signer. */
  get canSign() {
    return state.pubkey !== null && state.method !== 'readOnly' && state.signerReady
  },
}

export function setLoggedIn(
  pubkey: string,
  method: LoginMethod,
  signerReady = method !== 'readOnly',
): void {
  const changed = state.pubkey !== pubkey || state.method !== method || state.signerReady !== signerReady
  console.log(`[zc:auth] login: ${pubkey.slice(0, 8)} method=${method}`)
  state.pubkey = pubkey
  state.npub = npubEncode(pubkey)
  state.method = method
  state.signerReady = signerReady
  if (changed) notifySigningStateChanged()
}

export function setLoggedOut(): void {
  const changed = state.pubkey !== null || state.method !== null || state.signerReady
  console.log(`[zc:auth] logout: ${state.pubkey?.slice(0, 8) ?? 'none'}`)
  state.pubkey = null
  state.npub = null
  state.method = null
  state.signerReady = false
  state.profile = null
  if (changed) notifySigningStateChanged()
}

export function setSignerReady(ready: boolean): void {
  if (state.signerReady === ready) return
  state.signerReady = ready
  notifySigningStateChanged()
}

export function setProfile(profile: ProfileMetadata | null): void {
  state.profile = profile
}

export function setProfileLoading(loading: boolean): void {
  state.profileLoading = loading
}

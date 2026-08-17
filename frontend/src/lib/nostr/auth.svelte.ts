import { npubEncode } from 'nostr-tools/nip19'
import type { ProfileMetadata, LoginMethod } from './types'

interface AuthState {
  pubkey: string | null
  npub: string | null
  method: LoginMethod
  profile: ProfileMetadata | null
  profileLoading: boolean
}

const state = $state<AuthState>({
  pubkey: null,
  npub: null,
  method: null,
  profile: null,
  profileLoading: false,
})

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
  get profile() {
    return state.profile
  },
  get profileLoading() {
    return state.profileLoading
  },
  get isLoggedIn() {
    return state.pubkey !== null
  },
  /** In read-only mode there is no signer (window.nostr) available. */
  get canSign() {
    return state.pubkey !== null && state.method !== 'readOnly'
  },
}

export function setLoggedIn(pubkey: string, method: LoginMethod): void {
  console.log(`[zc:auth] login: ${pubkey.slice(0, 8)} method=${method}`)
  state.pubkey = pubkey
  state.npub = npubEncode(pubkey)
  state.method = method
}

export function setLoggedOut(): void {
  console.log(`[zc:auth] logout: ${state.pubkey?.slice(0, 8) ?? 'none'}`)
  state.pubkey = null
  state.npub = null
  state.method = null
  state.profile = null
}

export function setProfile(profile: ProfileMetadata | null): void {
  state.profile = profile
}

export function setProfileLoading(loading: boolean): void {
  state.profileLoading = loading
}

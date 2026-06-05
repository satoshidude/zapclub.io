/** NIP-01 kind:0 metadata (the subset zapclub uses). */
export interface ProfileMetadata {
  name?: string
  display_name?: string
  about?: string
  picture?: string
  banner?: string
  nip05?: string
  /** Lightning address (LNURL) — basis for later NIP-57 zaps. */
  lud16?: string
  website?: string
  [key: string]: unknown
}

export type LoginMethod = 'extension' | 'connect' | 'readOnly' | 'nstart' | null

// ── NIP-29 clubs ────────────────────────────────────────────────────────────

/** Parsed from kind:39000 club metadata. */
export interface Club {
  /** Group id (d-tag) — unique on the relay. */
  id: string
  name: string
  about?: string
  picture?: string
  /** open = anyone may join; closed = invite only. */
  open: boolean
  /** publicly readable (vs private). */
  isPublic: boolean
  /** Member count (only filled in club lists, for sorting/display). */
  memberCount?: number
  /** Owner/creator (first admin, kind 39001). pubkey (hex). */
  owner?: string
}

export interface ClubMember {
  pubkey: string
  /** Roles from 39002, e.g. "moderator". */
  roles: string[]
}

export interface ChatMessage {
  id: string
  pubkey: string
  content: string
  createdAt: number
}

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

// ── Phase 3: stage / queue / sync ─────────────────────────────────────────

/** A DJ on the stage (from kind:30102). */
export interface StageDj {
  pubkey: string
  /** Stage join time (created_at of first on-stage) — ordering / round-robin. */
  since: number
}

/** A track in a DJ queue. */
export interface QueueTrack {
  videoId: string
  title: string
  duration: number
  /** false = already played/disabled → out of rotation (greyed). undefined/true = active. */
  active?: boolean
}

/** A DJ's queue (kind:30103, replaceable per DJ/club). */
export interface DjQueue {
  dj: string
  tracks: QueueTrack[]
  updatedAt: number
}

/** now_playing sync state (kind:30100). */
export interface NowPlaying {
  /** YouTube video id (from "yt:<id>"). */
  videoId: string
  /** Track start in Unix-ms (conductor clock). */
  startedAt: number
  /** Event send time in Unix-ms — for offset calibration. */
  sentAt: number
  /** Track length in seconds (0 = unknown). */
  duration: number
  status: 'playing' | 'paused'
  /** pubkey of the DJ whose track is playing (round-robin). */
  dj: string
  /** Round-robin position (djIndex = pos%n, trackIndex = pos/n). */
  pos: number
  /** "Artist – Title" (display). */
  title: string
}

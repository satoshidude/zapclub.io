import { pool, PROFILE_RELAYS } from './pool'
import { signEvent } from './nostrLogin'

// Track "likes" = NIP-25 reactions (kind 7) that live on PUBLIC relays (like profiles,
// playlists and zaps), tagged with a fixed hashtag so the home page can list the most-
// liked tracks across all clubs. Each like records the track and the club it played in.
const KIND_REACTION = 7
const APP_TAG = 'zapclublike'

const LIKED_KEY = 'zapclub:likedTracks'
function loadLiked(): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(LIKED_KEY) || '[]') as string[])
  } catch {
    return new Set()
  }
}

const state = $state<{ liked: Set<string> }>({ liked: loadLiked() })

export const likes = {
  has(videoId: string): boolean {
    return state.liked.has(videoId)
  },
}

function persist(): void {
  try {
    localStorage.setItem(LIKED_KEY, JSON.stringify([...state.liked]))
  } catch {
    /* ignore */
  }
}

function nowSec(): number {
  return Math.floor(Date.now() / 1000)
}

export interface LikeTarget {
  videoId: string
  title: string
  clubId: string
  clubName: string
}

/** Publishes a like for a track (idempotent per video locally). */
export async function likeTrack(t: LikeTarget): Promise<void> {
  if (!t.videoId || state.liked.has(t.videoId)) return
  state.liked = new Set(state.liked).add(t.videoId)
  persist()
  const signed = await signEvent({
    kind: KIND_REACTION,
    created_at: nowSec(),
    content: '🔥',
    tags: [
      ['t', APP_TAG],
      ['r', `yt:${t.videoId}`],
      ['title', t.title || t.videoId],
      ['club', t.clubId],
      ['club_name', t.clubName || ''],
    ],
  })
  await Promise.allSettled(pool.publish(PROFILE_RELAYS, signed))
}

export interface TopTrack {
  videoId: string
  title: string
  clubId: string
  clubName: string
  likes: number
}

/** Most-liked tracks across all clubs, newest club context wins, deduped by liker. */
export async function fetchTopLikes(limit = 10): Promise<TopTrack[]> {
  const evs = await pool.querySync(
    PROFILE_RELAYS,
    { kinds: [KIND_REACTION], '#t': [APP_TAG] },
    { maxWait: 5000 },
  )
  const byVid = new Map<
    string,
    { likers: Set<string>; title: string; clubId: string; clubName: string; latest: number }
  >()
  for (const ev of evs) {
    const r = ev.tags.find((t) => t[0] === 'r')?.[1] ?? ''
    if (!r.startsWith('yt:')) continue
    const vid = r.slice(3)
    const title = ev.tags.find((t) => t[0] === 'title')?.[1] ?? vid
    const clubId = ev.tags.find((t) => t[0] === 'club')?.[1] ?? ''
    const clubName = ev.tags.find((t) => t[0] === 'club_name')?.[1] ?? ''
    let e = byVid.get(vid)
    if (!e) {
      e = { likers: new Set(), title, clubId, clubName, latest: ev.created_at }
      byVid.set(vid, e)
    }
    e.likers.add(ev.pubkey)
    if (ev.created_at >= e.latest) {
      e.latest = ev.created_at
      e.title = title
      if (clubId) {
        e.clubId = clubId
        e.clubName = clubName
      }
    }
  }
  return [...byVid.entries()]
    .map(([videoId, e]) => ({ videoId, title: e.title, clubId: e.clubId, clubName: e.clubName, likes: e.likers.size }))
    .sort((a, b) => b.likes - a.likes)
    .slice(0, limit)
}

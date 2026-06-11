// Thin LiveKit client wrapper for zapclub.io live A/V sessions.
// Mirrors the shape of player/youtube.ts (connect, publish, disconnect).
//
// The JWT is obtained from the relay's NIP-98-authenticated token endpoint:
//   GET https://relay.zapclub.io/.well-known/nip29/livekit/<groupId>
// which returns { url, token }.  The caller must be a member; staged DJs
// (+ owner/mod) receive canPublish:true; others are subscribe-only.

import {
  Room,
  RoomEvent,
  RemoteTrack,
  RemoteTrackPublication,
  RemoteParticipant,
  Track,
  type TrackPublication,
  type Participant,
} from 'livekit-client'

const RELAY_HTTP = 'https://relay.zapclub.io'

export interface LivekitRemoteTrack {
  track: RemoteTrack
  participant: RemoteParticipant
}

export interface LivekitClient {
  /** Attach a callback for new remote tracks (audio or video). */
  onRemoteTrack: (cb: (t: LivekitRemoteTrack) => void) => void
  /** Start publishing camera and/or microphone. Returns whether audio was captured. Audio failures are non-fatal. */
  publishLocal: (opts: { video: boolean; audio?: boolean }) => Promise<{ hasAudio: boolean }>
  /** Stop publishing — leaves the room if only publishing. */
  stopPublishing: () => Promise<void>
  /** Disconnect fully and release all resources. */
  disconnect: () => Promise<void>
  /** The underlying Room (for attaching track elements directly). */
  room: Room
}

/** Fetch a LiveKit token from the relay token endpoint (NIP-98 authenticated). */
export async function fetchToken(groupId: string): Promise<{ url: string; token: string }> {
  const url = `${RELAY_HTTP}/.well-known/nip29/livekit/${groupId}`
  const { signEvent } = await import('../nostr/nostrLogin')
  const { auth } = await import('../nostr/auth.svelte')

  if (!auth.pubkey) throw new Error('Not signed in')

  const authEvent = {
    kind: 27235,
    created_at: Math.floor(Date.now() / 1000),
    tags: [
      ['u', url],
      ['method', 'GET'],
    ],
    content: '',
    pubkey: auth.pubkey,
  }
  const signed = await signEvent(authEvent)
  const res = await fetch(url, {
    method: 'GET',
    headers: { Authorization: `Nostr ${btoa(JSON.stringify(signed))}` },
  })
  if (!res.ok) throw new Error(`LiveKit token request failed: ${res.status}`)
  return res.json() as Promise<{ url: string; token: string }>
}

/** Connect to the LiveKit room for the given club and return a client handle. */
export async function connectLivekit(groupId: string): Promise<LivekitClient> {
  const { url, token } = await fetchToken(groupId)

  const room = new Room({
    adaptiveStream: true,
    dynacast: true,
  })

  await room.connect(url, token)

  let onRemoteCb: ((t: LivekitRemoteTrack) => void) | null = null

  room.on(RoomEvent.TrackSubscribed, (track: RemoteTrack, _pub: RemoteTrackPublication, participant: RemoteParticipant) => {
    onRemoteCb?.({ track, participant })
  })

  const client: LivekitClient = {
    room,
    onRemoteTrack(cb) {
      onRemoteCb = cb
      // Replay tracks already subscribed before this callback was registered
      // (publisher already in room when viewer joins).
      room.remoteParticipants.forEach((participant) => {
        participant.trackPublications.forEach((pub) => {
          if (pub.track && pub.isSubscribed) {
            cb({ track: pub.track as RemoteTrack, participant })
          }
        })
      })
    },
    async publishLocal({ video, audio }) {
      if (video) {
        try {
          await room.localParticipant.setCameraEnabled(true)
        } catch (e) {
          const msg = (e as Error)?.message ?? ''
          if (msg.includes('not found') || msg.includes('NotFound') || msg.includes('Requested device'))
            throw new Error('Camera not found. Make sure your camera is connected and the browser has permission to use it.')
          throw e
        }
      }
      let hasAudio = false
      if (audio) {
        try {
          await room.localParticipant.setMicrophoneEnabled(true)
          hasAudio = true
        } catch (e) {
          console.warn('[zapclub] mic unavailable, video-only:', e)
        }
      }
      return { hasAudio }
    },
    async stopPublishing() {
      await room.localParticipant.setMicrophoneEnabled(false)
      await room.localParticipant.setCameraEnabled(false)
    },
    async disconnect() {
      await room.disconnect()
    },
  }

  return client
}

/** Attach a RemoteTrack to a <video> or <audio> element. Returns a cleanup fn. */
export function attachTrack(track: RemoteTrack, el: HTMLVideoElement | HTMLAudioElement): () => void {
  track.attach(el)
  return () => track.detach(el)
}

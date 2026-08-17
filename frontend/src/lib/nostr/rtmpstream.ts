import { auth } from './auth.svelte'
import { signEvent } from './nostrLogin'
import { publishClub } from './groups'

const RELAY_HTTP = 'https://relay.zapclub.io'
const RELAY_WS = 'wss://relay.zapclub.io'
export const KIND_STREAM = 30110

// Build a base64-encoded NIP-98 auth token for WebSocket query-param auth.
// WebSocket upgrades are GET requests — browsers cannot set custom headers.
async function nip98Token(path: string): Promise<string> {
  const pk = auth.pubkey
  if (!pk) throw new Error('Not signed in')
  const url = `${RELAY_HTTP}${path}`
  const ev = {
    kind: 27235,
    created_at: Math.floor(Date.now() / 1000),
    tags: [['u', url], ['method', 'GET']],
    content: '',
    pubkey: pk,
  }
  const signed = await signEvent(ev)
  return btoa(JSON.stringify(signed))
}

/** Publish a kind 30110 stream-status event so all club members see the stream link. */
async function publishStreamStatus(
  club: string,
  type: 'rtmp' | 'radio',
  status: 'live' | 'ended',
  watchURL = '',
): Promise<void> {
  const tags: string[][] = [['h', club], ['d', club], ['type', type], ['status', status]]
  if (watchURL) tags.push(['watch', watchURL])
  await publishClub({ kind: KIND_STREAM, created_at: Math.floor(Date.now() / 1000), tags, content: '' })
}

// Preferred MIME types for MediaRecorder — relay consumers accept audio/webm.
function bestMimeType(): string {
  for (const t of ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg;codecs=opus']) {
    if (MediaRecorder.isTypeSupported(t)) return t
  }
  return ''
}

// Capture the current tab's audio via getDisplayMedia. Requires a user gesture.
// The browser will show a dialog asking what to share — the user should pick this tab.
// Video is requested at minimum size (required by some browsers) then discarded.
async function captureTabAudio(): Promise<MediaStream> {
  const stream = await navigator.mediaDevices.getDisplayMedia({
    audio: true,
    video: { width: 1, height: 1, frameRate: 1 },
  })
  // Drop the video track — we only need audio.
  stream.getVideoTracks().forEach((t) => { t.stop(); stream.removeTrack(t) })
  if (stream.getAudioTracks().length === 0) {
    throw new Error('No audio track captured — make sure to enable "Share tab audio" in the dialog')
  }
  return stream
}

// Open a WebSocket to the relay with NIP-98 query-param auth.
async function openWebSocket(path: string): Promise<WebSocket> {
  const token = await nip98Token(path)
  const ws = new WebSocket(`${RELAY_WS}${path}?auth=${encodeURIComponent(token)}`)
  ws.binaryType = 'arraybuffer'
  await new Promise<void>((resolve, reject) => {
    const t = setTimeout(() => reject(new Error('WebSocket connection timeout')), 10_000)
    ws.onopen = () => { clearTimeout(t); resolve() }
    ws.onerror = () => { clearTimeout(t); reject(new Error('WebSocket connection failed')) }
  })
  return ws
}

// ── RTMP streaming (browser audio → relay WebSocket → ffmpeg → Twitch RTMP) ──

export interface RtmpSession {
  stop(): Promise<void>
}

export async function startRtmpStream(
  club: string,
  clubName: string,
  server: string,
  key: string,
  watchURL = '',
): Promise<RtmpSession> {
  const stream = await captureTabAudio()

  let ws: WebSocket
  try {
    ws = await openWebSocket(`/rtmp/push/${club}`)
  } catch (e) {
    stream.getTracks().forEach((t) => t.stop())
    throw e
  }

  // First message: config so relay knows where to push RTMP.
  ws.send(JSON.stringify({ server, key, clubName }))

  const mimeType = bestMimeType()
  const recorder = new MediaRecorder(stream, { mimeType, audioBitsPerSecond: 128_000 })
  recorder.ondataavailable = (e) => {
    if (e.data.size > 0 && ws.readyState === WebSocket.OPEN) ws.send(e.data)
  }
  recorder.start(500)

  await publishStreamStatus(club, 'rtmp', 'live', watchURL)

  return {
    stop: async () => {
      recorder.stop()
      stream.getTracks().forEach((t) => t.stop())
      ws.close()
      await publishStreamStatus(club, 'rtmp', 'ended')
    },
  }
}

// ── Webradio (browser audio → relay WebSocket → HTTP fan-out for listeners) ──

export interface RadioSession {
  stop(): Promise<void>
}

export async function startRadioStream(club: string): Promise<RadioSession> {
  const stream = await captureTabAudio()

  let ws: WebSocket
  try {
    ws = await openWebSocket(`/radio/push/${club}`)
  } catch (e) {
    stream.getTracks().forEach((t) => t.stop())
    throw e
  }

  const mimeType = bestMimeType()
  const recorder = new MediaRecorder(stream, { mimeType, audioBitsPerSecond: 128_000 })
  recorder.ondataavailable = (e) => {
    if (e.data.size > 0 && ws.readyState === WebSocket.OPEN) ws.send(e.data)
  }
  recorder.start(500)

  const radioURL = `${RELAY_HTTP}/radio/${club}`
  await publishStreamStatus(club, 'radio', 'live', radioURL)

  return {
    stop: async () => {
      recorder.stop()
      stream.getTracks().forEach((t) => t.stop())
      ws.close()
      await publishStreamStatus(club, 'radio', 'ended')
    },
  }
}

// ── Config persistence (Twitch stream key saved locally, never sent to relay) ──

const LS_KEY = (club: string) => `zapclub:rtmp:${club}`

export function loadRtmpConfig(club: string): { server: string; key: string; watchURL: string } {
  try {
    const raw = localStorage.getItem(LS_KEY(club))
    if (raw) {
      const p = JSON.parse(raw)
      return { server: p.server ?? '', key: p.key ?? '', watchURL: p.watchURL ?? '' }
    }
  } catch { /* ignore */ }
  return { server: '', key: '', watchURL: '' }
}

export function saveRtmpConfig(club: string, server: string, key: string, watchURL = ''): void {
  try { localStorage.setItem(LS_KEY(club), JSON.stringify({ server, key, watchURL })) } catch { /* ignore */ }
}

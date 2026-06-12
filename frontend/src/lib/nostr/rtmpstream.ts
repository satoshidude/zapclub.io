import { auth } from './auth.svelte'
import { signEvent } from './nostrLogin'
import { publishClub } from './groups'

const RELAY_HTTP = 'https://relay.zapclub.io'
export const KIND_STREAM = 30110

async function nip98Header(url: string, method = 'POST'): Promise<string> {
  const pk = auth.pubkey
  if (!pk) throw new Error('Not signed in')
  const ev = {
    kind: 27235,
    created_at: Math.floor(Date.now() / 1000),
    tags: [['u', url], ['method', method]],
    content: '',
    pubkey: pk,
  }
  const signed = await signEvent(ev)
  return `Nostr ${btoa(JSON.stringify(signed))}`
}

/** Publish a kind 30110 stream-status event to the club relay. */
async function publishStreamStatus(
  club: string,
  type: 'rtmp' | 'radio',
  status: 'live' | 'ended',
  watchURL = '',
): Promise<void> {
  const tags: string[][] = [
    ['h', club],
    ['d', club],
    ['type', type],
    ['status', status],
  ]
  if (watchURL) tags.push(['watch', watchURL])
  await publishClub({
    kind: KIND_STREAM,
    created_at: Math.floor(Date.now() / 1000),
    tags,
    content: '',
  })
}

export async function startRtmpStream(
  club: string,
  clubName: string,
  server: string,
  key: string,
  watchURL = '',
): Promise<void> {
  const url = `${RELAY_HTTP}/rtmp/start`
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: await nip98Header(url) },
    body: JSON.stringify({ club, clubName, server, key }),
  })
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`)
  // Broadcast the public watch URL so all club members see the stream link.
  await publishStreamStatus(club, 'rtmp', 'live', watchURL)
}

export async function stopRtmpStream(club: string): Promise<void> {
  const url = `${RELAY_HTTP}/rtmp/stop`
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: await nip98Header(url) },
    body: JSON.stringify({ club }),
  })
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`)
  await publishStreamStatus(club, 'rtmp', 'ended')
}

export async function startRadioStream(club: string): Promise<void> {
  const url = `${RELAY_HTTP}/radio/start?club=${encodeURIComponent(club)}`
  const res = await fetch(url, {
    method: 'POST',
    headers: { Authorization: await nip98Header(url) },
  })
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`)
  const watchURL = `${RELAY_HTTP}/radio/${club}`
  await publishStreamStatus(club, 'radio', 'live', watchURL)
}

export async function stopRadioStream(club: string): Promise<void> {
  const url = `${RELAY_HTTP}/radio/stop?club=${encodeURIComponent(club)}`
  const res = await fetch(url, {
    method: 'POST',
    headers: { Authorization: await nip98Header(url) },
  })
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`)
  await publishStreamStatus(club, 'radio', 'ended')
}

const LS_KEY = (club: string) => `zapclub:rtmp:${club}`

export function loadRtmpConfig(club: string): { server: string; key: string; watchURL: string } {
  try {
    const raw = localStorage.getItem(LS_KEY(club))
    if (raw) {
      const parsed = JSON.parse(raw)
      return { server: parsed.server ?? '', key: parsed.key ?? '', watchURL: parsed.watchURL ?? '' }
    }
  } catch { /* ignore */ }
  return { server: '', key: '', watchURL: '' }
}

export function saveRtmpConfig(club: string, server: string, key: string, watchURL = ''): void {
  try {
    localStorage.setItem(LS_KEY(club), JSON.stringify({ server, key, watchURL }))
  } catch { /* ignore */ }
}

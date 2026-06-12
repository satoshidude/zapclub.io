import { auth } from './auth.svelte'
import { signEvent } from './nostrLogin'

const RELAY_HTTP = 'https://relay.zapclub.io'

async function nip98Header(url: string): Promise<string> {
  const pk = auth.pubkey
  if (!pk) throw new Error('Not signed in')
  const ev = {
    kind: 27235,
    created_at: Math.floor(Date.now() / 1000),
    tags: [['u', url], ['method', 'POST']],
    content: '',
    pubkey: pk,
  }
  const signed = await signEvent(ev)
  return `Nostr ${btoa(JSON.stringify(signed))}`
}

export async function startRtmpStream(
  club: string,
  clubName: string,
  server: string,
  key: string,
): Promise<void> {
  const url = `${RELAY_HTTP}/rtmp/start`
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: await nip98Header(url) },
    body: JSON.stringify({ club, clubName, server, key }),
  })
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`)
}

export async function stopRtmpStream(club: string): Promise<void> {
  const url = `${RELAY_HTTP}/rtmp/stop`
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: await nip98Header(url) },
    body: JSON.stringify({ club }),
  })
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`)
}

const LS_KEY = (club: string) => `zapclub:rtmp:${club}`

export function loadRtmpConfig(club: string): { server: string; key: string } {
  try {
    const raw = localStorage.getItem(LS_KEY(club))
    if (raw) return JSON.parse(raw)
  } catch { /* ignore */ }
  return { server: '', key: '' }
}

export function saveRtmpConfig(club: string, server: string, key: string): void {
  try {
    localStorage.setItem(LS_KEY(club), JSON.stringify({ server, key }))
  } catch { /* ignore */ }
}

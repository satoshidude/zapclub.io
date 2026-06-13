import { auth } from './auth.svelte'
import { signEvent } from './nostrLogin'
import { publishClub } from './groups'
import { KIND_STREAM } from './groups'

const RELAY_HTTP = 'https://relay.zapclub.io'
const STREAM_BASE = 'https://stream.zapclub.io'

async function nip98Header(path: string): Promise<string> {
  const pk = auth.pubkey
  if (!pk) throw new Error('Not signed in')
  const url = `${RELAY_HTTP}${path}`
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

async function radioRequest(clubID: string, action: 'start' | 'stop'): Promise<void> {
  const path = `/radio/${clubID}/${action}`
  const authHeader = await nip98Header(path)
  const res = await fetch(`${RELAY_HTTP}${path}`, {
    method: 'POST',
    headers: { Authorization: authHeader },
  })
  if (!res.ok) throw new Error(await res.text())
}

async function publishStreamStatus(club: string, status: 'live' | 'ended', watchURL = ''): Promise<void> {
  const tags: string[][] = [['h', club], ['d', club], ['type', 'radio'], ['status', status]]
  if (watchURL) tags.push(['watch', watchURL])
  await publishClub({ kind: KIND_STREAM, created_at: Math.floor(Date.now() / 1000), tags, content: '' })
}

export async function startRadioStream(club: string): Promise<void> {
  await radioRequest(club, 'start')
  await publishStreamStatus(club, 'live', `${STREAM_BASE}/${club}`)
}

export async function stopRadioStream(club: string): Promise<void> {
  await radioRequest(club, 'stop')
  await publishStreamStatus(club, 'ended')
}

export function radioStreamURL(club: string): string {
  return `${STREAM_BASE}/${club}`
}

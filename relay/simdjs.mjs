// Multi-DJ round-robin simulation. Adds N extra DJs to a club: each joins (9021),
// goes on stage (30102) and fills a queue (30103). The browser conductor then
// interleaves all DJs' queues round-robin. Verifies the relay accepts member
// stage/queue writes and that the stage shows all DJs.
//
// Run from relay/ with node_modules symlinked to a nostr-tools install:
//   RELAY_URL=wss://relay.zapclub.io CLUB=<id> node simdjs.mjs
import { finalizeEvent, generateSecretKey, getPublicKey } from 'nostr-tools/pure'

const URL = process.env.RELAY_URL || 'wss://relay.zapclub.io'
const CLUB = process.env.CLUB
if (!CLUB) { console.error('set CLUB=<group id>'); process.exit(1) }
const now = () => Math.floor(Date.now() / 1000)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

function conn(sk) {
  const ws = new WebSocket(URL)
  const pend = new Map()
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data.toString())
    if (m[0] === 'AUTH') ws.send(JSON.stringify(['AUTH', finalizeEvent({ kind: 22242, created_at: now(), tags: [['relay', URL], ['challenge', m[1]]], content: '' }, sk)]))
    else if (m[0] === 'OK') { const p = pend.get(m[1]); if (p) { pend.delete(m[1]); p([m[2], m[3]]) } }
    else if (m[0] === 'EVENT') { const p = pend.get('r:' + m[1]); if (p) p.got.push(m[2]) }
    else if (m[0] === 'EOSE') { const p = pend.get('r:' + m[1]); if (p) { pend.delete('r:' + m[1]); p.res(p.got) } }
  }
  return new Promise((res) => { ws.onopen = () => setTimeout(() => res({
    pub: getPublicKey(sk),
    ev: (t) => { const e = finalizeEvent(t, sk); return new Promise((r) => { pend.set(e.id, r); ws.send(JSON.stringify(['EVENT', e])) }) },
    query: (filter) => new Promise((r) => { const id = 'q' + Math.random(); pend.set('r:' + id, { res: r, got: [] }); ws.send(JSON.stringify(['REQ', id, filter])) }),
  }), 500) })
}
const ok = (r) => (r[0] ? 'OK' : 'REJECT ' + r[1])

// Distinct, well-known video ids so the round-robin is visually obvious.
const DJS = [
  { name: 'DJ Gangnam', tracks: [['9bZkp7q19f0', 'PSY - Gangnam Style', 252], ['kJQP7kiw5Fk', 'Luis Fonsi - Despacito', 281]] },
  { name: 'DJ Funk', tracks: [['OPf0YbXqDm0', 'Mark Ronson - Uptown Funk', 270], ['60ItHLz5WEA', 'Alan Walker - Faded', 212]] },
  { name: 'DJ Shape', tracks: [['JGwWNGJdvx8', 'Ed Sheeran - Shape of You', 263]] },
  { name: 'DJ Queen', tracks: [['fJ9rUzIMcZQ', 'Queen - Bohemian Rhapsody', 355]] },
]

console.log('club', CLUB, '| relay', URL)
let i = 0
for (const dj of DJS) {
  i++
  const sk = generateSecretKey()
  const c = await conn(sk)
  const since = now() + i // later than the browser DJ → stays after it in the order
  // 1. join (open club → auto-add)
  const j = await c.ev({ kind: 9021, created_at: now(), tags: [['h', CLUB]], content: '' })
  await sleep(500)
  // 2. on stage
  const s = await c.ev({ kind: 30102, created_at: now(), tags: [['h', CLUB], ['d', CLUB], ['since', String(since)]], content: 'on' })
  // 3. queue
  const trackTags = dj.tracks.map((t) => ['track', `yt:${t[0]}`, t[1], String(t[2])])
  const q = await c.ev({ kind: 30103, created_at: now(), tags: [['h', CLUB], ['d', CLUB], ...trackTags], content: '' })
  console.log(`${dj.name} (${c.pub.slice(0, 8)}…): join ${ok(j)} | stage ${ok(s)} | queue ${ok(q)} (${dj.tracks.length} tracks)`)
  await sleep(300)
}

await sleep(800)
// Verify: how many DJs are on stage now (30102), how many queues (30103)?
const host = await conn(generateSecretKey())
const stages = await host.query({ kinds: [30102], '#h': [CLUB] })
const queues = await host.query({ kinds: [30103], '#h': [CLUB] })
const onStage = stages.filter((e) => e.content === 'on')
console.log(`\nstage events: ${stages.length} (on: ${onStage.length}) | queues: ${queues.length}`)
console.log('DJs on stage:', onStage.map((e) => e.pubkey.slice(0, 8)).join(', '))
console.log(onStage.length >= 4 ? '✓ sim DJs are on stage — open the browser conductor to see the round-robin' : '✗ sim DJs not all on stage')
process.exit(0)

// E2E smoke test for the zapclub NIP-29 relay. Verifies the two lessons that
// code review can't catch, plus membership write-protection:
//   1. open club auto-join (9021 without approval) — relay29 must be on master
//      (v0.5.1 inverts open/closed and breaks this)
//   2. now_playing (kind 30100) ReplaceEvent dedup — two writes → exactly ONE row
//   3. non-members cannot write content events
//
// Run: RELAY_URL=ws://127.0.0.1:3334 NODE_PATH=<nostr-tools dir> node grouptest.mjs
import { finalizeEvent, generateSecretKey, getPublicKey } from 'nostr-tools/pure'

const URL = process.env.RELAY_URL || 'ws://127.0.0.1:3334'
const now = () => Math.floor(Date.now() / 1000)
const G = 'zc' + Math.random().toString(16).slice(2, 16)
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
  }), 400) })
}
const ok = (r) => (r[0] ? 'OK' : 'REJECT ' + r[1])
let failures = 0
const assert = (cond, msg) => { console.log((cond ? '  ✓ ' : '  ✗ FAIL ') + msg); if (!cond) failures++ }

const hsk = generateSecretKey(), msk = generateSecretKey(), ssk = generateSecretKey()
const host = await conn(hsk), mem = await conn(msk), stranger = await conn(ssk)
console.log('club', G)

// 1. create + open/public metadata
await host.ev({ kind: 9007, created_at: now(), tags: [['h', G]], content: '' })
await host.ev({ kind: 9002, created_at: now(), tags: [['h', G], ['name', 'E2E Club'], ['open'], ['public']], content: '' })
await sleep(600)

// 2. member self-joins an OPEN club without approval
const join = await mem.ev({ kind: 9021, created_at: now(), tags: [['h', G]], content: '' })
console.log('JOIN (open) ->', ok(join))
await sleep(600)
const members = (await host.query({ kinds: [39002], '#d': [G] }))
const memberPubs = members.flatMap((e) => e.tags.filter((t) => t[0] === 'p').map((t) => t[1]))
assert(memberPubs.includes(mem.pub), 'open club auto-join: member is in 39002')

// 3. now_playing ReplaceEvent dedup — two writes, expect ONE row, the latest
console.log('np write 1 ->', ok(await mem.ev({ kind: 30100, created_at: now(), tags: [['h', G], ['d', G], ['track', 'yt:AAA'], ['pos', '0']], content: 'Artist – Track One' })))
await sleep(300)
console.log('np write 2 ->', ok(await mem.ev({ kind: 30100, created_at: now() + 1, tags: [['h', G], ['d', G], ['track', 'yt:BBB'], ['pos', '1']], content: 'Artist – Track Two' })))
await sleep(600)
const npByH = await host.query({ kinds: [30100], '#h': [G] })
console.log('  query #h returned', npByH.length, '| query #d returned', (await host.query({ kinds: [30100], '#d': [G] })).length)
const np = npByH
assert(np.length === 1, `now_playing dedup: exactly 1 row (got ${np.length})`)
assert(np[0]?.content === 'Artist – Track Two', 'now_playing keeps the latest version')

// 4. non-member write is rejected
const strangerWrite = await stranger.ev({ kind: 30100, created_at: now(), tags: [['h', G], ['d', G], ['track', 'yt:EVIL']], content: 'intruder' })
assert(strangerWrite[0] === false, 'non-member write rejected: ' + ok(strangerWrite))

// 5. Superadmin HTTP API (NIP-98): ban + purge + replay + unban + delete-club.
//    Only runs when ADMIN_SK (whose pubkey the relay was booted with as RELAY_SUPERADMIN)
//    and ADMIN_URL are set — see e2e.sh, which wires it all up.
let cleaned = false
if (process.env.ADMIN_SK && process.env.ADMIN_URL) {
  const ADMIN_URL = process.env.ADMIN_URL
  const ASK = Uint8Array.from(Buffer.from(process.env.ADMIN_SK, 'hex'))
  // Returns {status, auth, body}; pass reuseAuth to replay an existing NIP-98 header.
  const adminReq = async (path, method, body, reuseAuth) => {
    const url = ADMIN_URL + path
    const auth = reuseAuth || ('Nostr ' + Buffer.from(JSON.stringify(
      finalizeEvent({ kind: 27235, created_at: now(), tags: [['u', url], ['method', method]], content: '' }, ASK),
    )).toString('base64'))
    const headers = { Authorization: auth }
    if (body) headers['Content-Type'] = 'application/json'
    const res = await fetch(url, { method, headers, body: body ? JSON.stringify(body) : undefined })
    return { status: res.status, auth, body: await res.text() }
  }
  console.log('\n-- admin API (NIP-98) --')

  const noAuth = await fetch(ADMIN_URL + '/admin/bans')
  assert(noAuth.status === 401, 'admin without auth → 401 (got ' + noAuth.status + ')')

  const ban = await adminReq('/admin/ban', 'POST', { pubkey: mem.pub, reason: 'e2e' })
  assert(ban.status === 200, 'ban → 200 (got ' + ban.status + ' ' + ban.body.slice(0, 60) + ')')
  await sleep(500)

  const afterBan = await mem.ev({ kind: 9, created_at: now(), tags: [['h', G]], content: 'still here?' })
  assert(afterBan[0] === false, 'banned member write rejected: ' + ok(afterBan))

  const replay = await adminReq('/admin/ban', 'POST', { pubkey: mem.pub }, ban.auth)
  assert(replay.status === 401, 'NIP-98 token replay rejected → 401 (got ' + replay.status + ')')

  const unban = await adminReq('/admin/unban', 'POST', { pubkey: mem.pub })
  assert(unban.status === 200, 'unban → 200 (got ' + unban.status + ')')
  await sleep(400)
  const afterUnban = await mem.ev({ kind: 9, created_at: now(), tags: [['h', G]], content: 'back' })
  assert(afterUnban[0] === true, 'unbanned member can write again: ' + ok(afterUnban))

  const del = await adminReq('/admin/delete-club', 'POST', { groupId: G })
  assert(del.status === 200, 'delete-club → 200 (got ' + del.status + ')')
  await sleep(600)
  const metaAfter = await host.query({ kinds: [39000], '#d': [G] })
  assert(metaAfter.length === 0, 'club metadata gone after delete-club (got ' + metaAfter.length + ')')
  cleaned = true
}

if (!cleaned) await host.ev({ kind: 9008, created_at: now(), tags: [['h', G]], content: '' }) // delete group (cleanup)
console.log(failures === 0 ? '\nALL PASSED' : `\n${failures} FAILED`)
process.exit(failures === 0 ? 0 : 1)

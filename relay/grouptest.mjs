// E2E smoke test for the zapclub NIP-29 relay. Verifies the two lessons that
// code review can't catch, plus membership write-protection:
//   1. open club auto-join (9021 without approval) — relay29 must be on master
//      (v0.5.1 inverts open/closed and breaks this)
//   2. now_playing (kind 30100) ReplaceEvent dedup — two writes → exactly ONE row
//   3. non-members cannot write content events
//
// Run: RELAY_URL=ws://127.0.0.1:3334 NODE_PATH=<nostr-tools dir> node grouptest.mjs
import { finalizeEvent, generateSecretKey, getPublicKey } from 'nostr-tools/pure'
import { minePow } from 'nostr-tools/nip13'

// PoW the relay requires (must be ≥ RELAY_POW_CHAT it boots with). Join (9021) isn't gated.
const POWBITS = { 9: 12 }

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
  const send = (e) => new Promise((r) => { pend.set(e.id, r); ws.send(JSON.stringify(['EVENT', e])) })
  return new Promise((res) => { ws.onopen = () => setTimeout(() => res({
    pub: getPublicKey(sk),
    // ev() mines NIP-13 PoW for join/chat (as the real client does); evRaw() skips it.
    ev: (t) => {
      const bits = POWBITS[t.kind]
      let tt = t
      if (bits) { const m = minePow({ pubkey: getPublicKey(sk), created_at: t.created_at, kind: t.kind, tags: [...(t.tags || [])], content: t.content }, bits); tt = { kind: m.kind, created_at: m.created_at, tags: m.tags, content: m.content } }
      return send(finalizeEvent(tt, sk))
    },
    evRaw: (t) => send(finalizeEvent(t, sk)),
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

// 2a. LEAVE then REJOIN (open club). relay29 stores a remove-user record on a 9022 leave
// and then (buggily) bars ALL future joins by that pubkey — clearRemovalBarOnJoin must
// clear that stale record so the rejoin re-adds the member. Regression guard for the
// "can't rejoin after leaving" bug.
await mem.ev({ kind: 9022, created_at: now(), tags: [['h', G]], content: '' })
await sleep(600)
const afterLeave = (await host.query({ kinds: [39002], '#d': [G] }))
  .flatMap((e) => e.tags.filter((t) => t[0] === 'p').map((t) => t[1]))
assert(!afterLeave.includes(mem.pub), 'leave removes member from 39002')
const rejoin = await mem.ev({ kind: 9021, created_at: now() + 1, tags: [['h', G]], content: '' })
console.log('REJOIN (after leave) ->', ok(rejoin))
await sleep(600)
const afterRejoin = (await host.query({ kinds: [39002], '#d': [G] }))
  .flatMap((e) => e.tags.filter((t) => t[0] === 'p').map((t) => t[1]))
assert(afterRejoin.includes(mem.pub), 'rejoin after leave re-adds member to 39002')

// 2b. NIP-13 PoW: chat without proof-of-work is rejected, with PoW accepted.
const noPow = await mem.evRaw({ kind: 9, created_at: now(), tags: [['h', G]], content: 'no pow' })
assert(noPow[0] === false && /pow/i.test(noPow[1] || ''), 'chat without PoW rejected: ' + ok(noPow))
const yesPow = await mem.ev({ kind: 9, created_at: now(), tags: [['h', G]], content: 'mined' })
assert(yesPow[0] === true, 'chat with PoW accepted: ' + ok(yesPow))

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

// 4b. Paid-club entry gate (relay-enforced). Owner marks the club paid (30101); the test
//     controls the "zapper" key so it can mint valid/invalid receipts. Then a joiner must
//     present a valid 9735 to join.
const zsk = generateSecretKey(), zpub = getPublicKey(zsk) // the club's entry LNURL zapper
await host.ev({ kind: 30101, created_at: now(), tags: [['h', G], ['d', G], ['access', 'paid'], ['price', '5'], ['lud16', 'club@test'], ['zapper', zpub]], content: '' })
await sleep(500)
const jsk = generateSecretKey(), joiner = await conn(jsk)
// build a 9735 receipt: embeds a 9734 signed by `signWith`, receipt signed by `byZapper`.
const receipt = (signWith, byZapper, amountMsat, clubTag) => {
  const zr = finalizeEvent({ kind: 9734, created_at: now(), tags: [['amount', String(amountMsat)], ['p', zpub], ['h', clubTag], ['club_entry', clubTag]], content: '' }, signWith)
  return finalizeEvent({ kind: 9735, created_at: now(), tags: [['p', zpub], ['bolt11', 'lnbcfake'], ['description', JSON.stringify(zr)]], content: '' }, byZapper)
}
const joinWith = (j, proof) => j.ev({ kind: 9021, created_at: now(), tags: proof ? [['h', G], ['proof', JSON.stringify(proof)]] : [['h', G]], content: '' })

assert((await joinWith(joiner))[0] === false, 'paid join WITHOUT proof rejected')
const wrongSigner = await joinWith(joiner, receipt(jsk, generateSecretKey(), 5000, G)) // receipt not by the zapper
assert(wrongSigner[0] === false, 'paid join with non-zapper receipt rejected: ' + ok(wrongSigner))
const tooLow = await joinWith(joiner, receipt(jsk, zsk, 4000, G)) // 4 sats < 5
assert(tooLow[0] === false, 'paid join with too-low amount rejected: ' + ok(tooLow))
const notMine = await joinWith(joiner, receipt(generateSecretKey(), zsk, 5000, G)) // 9734 signed by someone else
assert(notMine[0] === false, 'paid join with someone else’s payment rejected: ' + ok(notMine))
// Stale receipt (>10min old) rejected — limits post-restart replay of an old proof.
const staleZr = finalizeEvent({ kind: 9734, created_at: now() - 700, tags: [['amount', '5000'], ['p', zpub], ['h', G], ['club_entry', G]], content: '' }, jsk)
const staleRec = finalizeEvent({ kind: 9735, created_at: now() - 700, tags: [['p', zpub], ['bolt11', 'lnbcstale'], ['description', JSON.stringify(staleZr)]], content: '' }, zsk)
const stale = await joinWith(joiner, staleRec)
assert(stale[0] === false && /expired/i.test(stale[1] || ''), 'paid join with a stale (>10min) receipt rejected: ' + ok(stale))
const good = receipt(jsk, zsk, 5000, G)
const okJoin = await joinWith(joiner, good)
assert(okJoin[0] === true, 'paid join with a valid receipt accepted: ' + ok(okJoin))
await sleep(400)
const paidMembers = (await host.query({ kinds: [39002], '#d': [G] })).flatMap((e) => e.tags.filter((t) => t[0] === 'p').map((t) => t[1]))
assert(paidMembers.includes(joiner.pub), 'paid joiner is now a member')
await joiner.ev({ kind: 9022, created_at: now(), tags: [['h', G]], content: '' }) // leave
await sleep(400)
const replay = await joinWith(joiner, good) // try to rejoin reusing the SAME receipt
assert(replay[0] === false && /already used/i.test(replay[1] || ''), 'replayed entry proof rejected: ' + ok(replay))

// 4c. Server conductor: with a DJ on stage and a non-empty queue, the RELAY itself (not any
//     client) publishes now_playing and advances the round-robin — the autonomous-playback
//     core. Also verifies the relay honors a skip-request (kind 30107). Long track durations
//     keep it deterministic (no time-based auto-advance/loop during the test). Needs RELAY_PK.
if (process.env.RELAY_PK) {
  const RPK = process.env.RELAY_PK
  const C = 'zc' + Math.random().toString(16).slice(2, 16)
  await host.ev({ kind: 9007, created_at: now(), tags: [['h', C]], content: '' })
  await host.ev({ kind: 9002, created_at: now(), tags: [['h', C], ['name', 'Conductor'], ['open'], ['public']], content: '' })
  await sleep(500)
  // host steps on stage (30102) and posts a 2-track queue (30103). Long durations → only an
  // explicit skip advances during the test window.
  await host.ev({ kind: 30102, created_at: now(), tags: [['h', C], ['d', C], ['since', String(now())]], content: '' })
  await host.ev({ kind: 30103, created_at: now(), tags: [['h', C], ['d', C], ['track', 'yt:VIDfirst001', 'First', '300'], ['track', 'yt:VIDsecond02', 'Second', '300']], content: '' })
  await sleep(4000) // conductor tick is 2.5s → it bootstraps now_playing within a tick
  const npA = (await host.query({ kinds: [30100], '#h': [C] })).find((e) => e.pubkey === RPK)
  assert(!!npA, 'conductor: the RELAY published now_playing')
  assert(!!npA && npA.tags.find((t) => t[0] === 'dj')?.[1] === host.pub, 'conductor: now_playing dj = the stage DJ')
  const firstTrack = npA && npA.tags.find((t) => t[0] === 'track')?.[1]
  const pos0 = (npA && npA.tags.find((t) => t[0] === 'pos')?.[1]) || '0'
  // a skip-request for the running track → the relay advances to the next track.
  await host.ev({ kind: 30107, created_at: now(), tags: [['h', C], ['d', C], ['pos', pos0]], content: '' })
  await sleep(4000)
  const npB = (await host.query({ kinds: [30100], '#h': [C] })).find((e) => e.pubkey === RPK)
  const trackB = npB && npB.tags.find((t) => t[0] === 'track')?.[1]
  assert(!!npB && trackB !== firstTrack, 'conductor: advanced to the next track on a skip-request (30107)')
  // role validation: a plain MEMBER (not owner/mod, not the playing DJ) cannot skip.
  await mem.ev({ kind: 9021, created_at: now(), tags: [['h', C]], content: '' }) // mem joins C
  await sleep(800)
  const posB = (npB && npB.tags.find((t) => t[0] === 'pos')?.[1]) || '1'
  await mem.ev({ kind: 30107, created_at: now(), tags: [['h', C], ['d', C], ['pos', posB]], content: '' })
  await sleep(4000)
  const npD = (await host.query({ kinds: [30100], '#h': [C] })).find((e) => e.pubkey === RPK)
  assert(!!npD && npD.tags.find((t) => t[0] === 'track')?.[1] === trackB, 'conductor: a non-mod member’s skip-request is IGNORED (role validation)')
  // broken-track quorum: 2 distinct members report the running track unplayable → relay skips it.
  await stranger.ev({ kind: 9021, created_at: now(), tags: [['h', C]], content: '' }) // stranger joins C
  await sleep(800)
  const curVid = (trackB || '').replace('yt:', '')
  await mem.ev({ kind: 20102, created_at: now(), tags: [['h', C]], content: curVid })
  await stranger.ev({ kind: 20102, created_at: now(), tags: [['h', C]], content: curVid })
  await sleep(4000)
  const npE = (await host.query({ kinds: [30100], '#h': [C] })).find((e) => e.pubkey === RPK)
  assert(!!npE && npE.tags.find((t) => t[0] === 'track')?.[1] !== trackB, 'conductor: broken-track quorum (2 members) skips the unplayable track')
  await host.ev({ kind: 9008, created_at: now(), tags: [['h', C]], content: '' }) // cleanup
}

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

  // Listener analytics: a member's presence beat (kind 20100) is recorded and surfaces in
  // /admin/listeners as a live listener of the club.
  await mem.ev({ kind: 20100, created_at: now(), tags: [['h', G]], content: '' })
  await sleep(400)
  const lis = await adminReq('/admin/listeners', 'GET')
  let lj = {}
  try { lj = JSON.parse(lis.body) } catch { /* ignore */ }
  const clubL = (lj.clubs || []).find((c) => c.id === G)
  assert(lis.status === 200 && !!clubL && clubL.live.includes(mem.pub), 'listeners: member shows as live in the club')
  assert(!!clubL && clubL.seen.some((s) => s.pubkey === mem.pub), 'listeners: member appears in the 24h seen list')

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

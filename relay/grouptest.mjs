// E2E smoke test for the zapclub NIP-29 relay. Verifies the two lessons that
// code review can't catch, plus membership write-protection:
//   1. open club auto-join (9021 without approval) — relay29 must be on master
//      (v0.5.1 inverts open/closed and breaks this)
//   2. now_playing (kind 30100) ReplaceEvent dedup — two writes → exactly ONE row
//   3. non-members cannot write content events
//   4. no more than three DJs can occupy a club stage
//
// Run: RELAY_URL=ws://127.0.0.1:3334 NODE_PATH=<nostr-tools dir> node grouptest.mjs
import { finalizeEvent, generateSecretKey, getPublicKey } from 'nostr-tools/pure'
import { minePow } from 'nostr-tools/nip13'

// PoW the relay requires (must match or exceed the defaults it boots with).
const POWBITS = { 9: 12, 9021: 15 }

const URL = process.env.RELAY_URL || 'ws://127.0.0.1:3334'
const RELAY_PK = process.env.RELAY_PK || ''
const SESSION_MARKER = 'zapclub-session-v1'
const now = () => Math.floor(Date.now() / 1000)
const G = 'zc' + Math.random().toString(16).slice(2, 16)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

function conn(sk, authenticate = true) {
  const ws = new WebSocket(URL)
  const pend = new Map()
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data.toString())
    if (m[0] === 'AUTH' && authenticate) ws.send(JSON.stringify(['AUTH', finalizeEvent({ kind: 22242, created_at: now(), tags: [['relay', URL], ['challenge', m[1]]], content: '' }, sk)]))
    else if (m[0] === 'OK') { const p = pend.get(m[1]); if (p) { pend.delete(m[1]); p([m[2], m[3]]) } }
    else if (m[0] === 'EVENT') { const p = pend.get('r:' + m[1]); if (p) p.got.push(m[2]) }
    else if (m[0] === 'EOSE') { const p = pend.get('r:' + m[1]); if (p) { if (p.live) p.ready(); else { pend.delete('r:' + m[1]); p.res({ events: p.got, closed: '' }) } } }
    else if (m[0] === 'CLOSED') { const p = pend.get('r:' + m[1]); if (p) { pend.delete('r:' + m[1]); p.res?.({ events: p.got, closed: m[2] || '' }); p.ready?.() } }
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
    sendEvent: send,
    queryResult: (filter) => new Promise((r) => { const id = 'q' + Math.random(); pend.set('r:' + id, { res: r, got: [] }); ws.send(JSON.stringify(['REQ', id, filter])) }),
    query: (filter) => new Promise((r) => { const id = 'q' + Math.random(); pend.set('r:' + id, { res: (result) => r(result.events), got: [] }); ws.send(JSON.stringify(['REQ', id, filter])) }),
    watch: (filter) => {
      const id = 's' + Math.random(), got = []
      let markReady
      const ready = new Promise((r) => { markReady = r })
      pend.set('r:' + id, { got, live: true, ready: markReady })
      ws.send(JSON.stringify(['REQ', id, filter]))
      return { got, ready, close: () => { pend.delete('r:' + id); ws.send(JSON.stringify(['CLOSE', id])) } }
    },
  }), 400) })
}
const sessionEvent = (principal, sessionSK, template) => finalizeEvent({
  ...template,
  tags: [...(template.tags || []), ['client', SESSION_MARKER], ['p', principal]],
}, sessionSK)
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

// 2c. The public stream stays public, but chat + member roster require current membership.
// Direct id queries must not bypass the history filter, and an already-open subscription must
// stop receiving immediately after a kick.
await sleep(400)
const memberChat = await mem.query({ kinds: [9], '#h': [G] })
const mined = memberChat.find((e) => e.content === 'mined')
assert(!!mined, 'member can read chat history')
const strangerChat = await stranger.queryResult({ kinds: [9], '#h': [G] })
assert(/restricted/i.test(strangerChat.closed), 'non-member chat subscription rejected')
const strangerMembers = await stranger.queryResult({ kinds: [39002], '#d': [G] })
assert(/restricted/i.test(strangerMembers.closed), 'non-member member-roster subscription rejected')
const publicMemberCounts = await stranger.query({ kinds: [30112], '#h': [G] })
const publicMemberCount = publicMemberCounts.at(-1)
assert(publicMemberCount?.pubkey === RELAY_PK, 'public member aggregate is relay-authored')
assert(publicMemberCount?.tags.find((t) => t[0] === 'count')?.[1] === '2', 'public member aggregate reports two members')
assert(!publicMemberCount?.tags.some((t) => t[0] === 'p'), 'public member aggregate exposes no identities')
const forgedMemberCount = await mem.evRaw({ kind: 30112, created_at: now(), tags: [['d', G], ['h', G], ['count', '99'], ['sent_at', String(Date.now())]], content: '' })
assert(forgedMemberCount[0] === false && /relay-authored/i.test(forgedMemberCount[1] || ''), 'client-forged member aggregate is rejected')
const directLeak = mined ? await stranger.query({ ids: [mined.id] }) : []
assert(directLeak.length === 0, 'direct event-id query cannot leak a chat message')

// NIP-29 membership mutations also carry identities. Join requests may additionally carry
// paid-entry proof material, so they are an owner/moderator inbox rather than member history.
const memberHistory = await mem.query({ kinds: [9000, 9001, 9022], '#h': [G] })
assert(memberHistory.length > 0, 'current member can read protected membership history')
const strangerMemberHistory = await stranger.queryResult({ kinds: [9000, 9001, 9022], '#h': [G] })
assert(/restricted/i.test(strangerMemberHistory.closed), 'non-member membership-history subscription rejected')
const directMemberLeak = memberHistory[0] ? await stranger.query({ ids: [memberHistory[0].id] }) : []
assert(directMemberLeak.length === 0, 'direct event-id query cannot leak a membership mutation')
const ownerJoinRequests = await host.query({ kinds: [9021], '#h': [G] })
const memberJoinRequests = await mem.queryResult({ kinds: [9021], '#h': [G] })
const strangerJoinRequests = await stranger.queryResult({ kinds: [9021], '#h': [G] })
assert(ownerJoinRequests.some((event) => event.pubkey === mem.pub), 'owner can read the protected join-request inbox')
assert(/restricted/i.test(memberJoinRequests.closed), 'plain member cannot read join-request identities/proofs')
assert(/restricted/i.test(strangerJoinRequests.closed), 'non-member cannot read join-request identities/proofs')
const directJoinLeak = ownerJoinRequests[0] ? await stranger.query({ ids: [ownerJoinRequests[0].id] }) : []
assert(directJoinLeak.length === 0, 'direct event-id query cannot leak a join request')

const liveChat = mem.watch({ kinds: [9], '#h': [G], since: now() })
const liveMembership = mem.watch({ kinds: [9000, 9001, 9022], '#h': [G], since: now() })
await liveChat.ready
await liveMembership.ready
await host.ev({ kind: 9, created_at: now(), tags: [['h', G]], content: 'before kick' })
await sleep(300)
assert(liveChat.got.some((e) => e.content === 'before kick'), 'member receives live chat')
await host.ev({ kind: 9001, created_at: now(), tags: [['h', G], ['p', mem.pub]], content: '' })
await sleep(300)
assert(liveMembership.got.some((event) => event.kind === 9001 && event.tags.some((tag) => tag[0] === 'p' && tag[1] === mem.pub)),
  'removed member receives its own exact live kick transition')
const unrelatedPub = getPublicKey(generateSecretKey())
await host.ev({ kind: 9001, created_at: now() + 1, tags: [['h', G], ['p', unrelatedPub]], content: '' })
await sleep(300)
assert(!liveMembership.got.some((event) => event.tags.some((tag) => tag[0] === 'p' && tag[1] === unrelatedPub)),
  'removed member receives no later membership transitions on the open subscription')
await host.ev({ kind: 9, created_at: now(), tags: [['h', G]], content: 'after kick' })
await sleep(300)
assert(!liveChat.got.some((e) => e.content === 'after kick'), 'kick revokes an already-open chat subscription')
liveChat.close()
liveMembership.close()
const kickedSessionPresence = await mem.sendEvent(sessionEvent(mem.pub, generateSecretKey(), {
  kind: 20100, created_at: now(), tags: [['h', G]], content: '',
}))
assert(kickedSessionPresence[0] === false && /current club members/i.test(kickedSessionPresence[1] || ''),
  'session event is rejected immediately after membership revocation: ' + ok(kickedSessionPresence))
const selfPutWatch = mem.watch({ kinds: [9000, 9001], '#h': [G], '#p': [mem.pub], since: now() - 1 })
await selfPutWatch.ready
const postKickRejoin = await mem.ev({ kind: 9021, created_at: now() + 1, tags: [['h', G]], content: '' })
await sleep(500)
assert(postKickRejoin[0] === true, 'kicked member can rejoin an open club')
assert(selfPutWatch.got.some((event) => {
  const targets = event.tags.filter((tag) => tag[0] === 'p')
  return event.kind === 9000 && targets.length > 0 && targets.every((tag) => tag[1] === mem.pub)
}),
  'non-member #p=self subscription receives only its generated auto-join put-user transition')
selfPutWatch.close()

// 2d. High-frequency presence/stage leases may use a throwaway page key, but only while the
// socket is NIP-42 authenticated as the p-tagged current member. The event stays normally signed
// by that page key; stale/future events and exact event replays are rejected.
console.log('\n-- connection-bound session events --')
const presenceSessionSK = generateSecretKey()
const sessionPresence = sessionEvent(mem.pub, presenceSessionSK, {
  kind: 20100, created_at: now(), tags: [['h', G]], content: '',
})
const acceptedPresence = await mem.sendEvent(sessionPresence)
assert(acceptedPresence[0] === true, 'member session-key presence accepted: ' + ok(acceptedPresence))
const replayedPresence = await mem.sendEvent(sessionPresence)
assert(replayedPresence[0] === false && /already accepted/i.test(replayedPresence[1] || ''),
  'exact session event replay rejected: ' + ok(replayedPresence))

const unauthSession = await conn(generateSecretKey(), false)
const unauthPresence = await unauthSession.sendEvent(sessionEvent(mem.pub, generateSecretKey(), {
  kind: 20100, created_at: now(), tags: [['h', G]], content: '',
}))
assert(unauthPresence[0] === false && /auth-required/i.test(unauthPresence[1] || ''),
  'session event without NIP-42 rejected: ' + ok(unauthPresence))
const wrongPrincipal = await mem.sendEvent(sessionEvent(host.pub, generateSecretKey(), {
  kind: 20100, created_at: now(), tags: [['h', G]], content: '',
}))
assert(wrongPrincipal[0] === false && /does not match/i.test(wrongPrincipal[1] || ''),
  'session p tag must match the authenticated socket: ' + ok(wrongPrincipal))
const stalePresence = await mem.sendEvent(sessionEvent(mem.pub, generateSecretKey(), {
  kind: 20100, created_at: now() - 70, tags: [['h', G]], content: '',
}))
assert(stalePresence[0] === false && /too old/i.test(stalePresence[1] || ''),
  'stale session event rejected: ' + ok(stalePresence))
const futurePresence = await mem.sendEvent(sessionEvent(mem.pub, generateSecretKey(), {
  kind: 20100, created_at: now() + 60, tags: [['h', G]], content: '',
}))
assert(futurePresence[0] === false && /future/i.test(futurePresence[1] || ''),
  'session event over 30 seconds in the future rejected: ' + ok(futurePresence))
const mainKeyPresence = await mem.evRaw({ kind: 20100, created_at: now(), tags: [['h', G]], content: '' })
assert(mainKeyPresence[0] === true, 'ordinary main-key presence remains compatible: ' + ok(mainKeyPresence))

// 2e. Listener sessions are independent of login/member presence. An unauthenticated browser
// can heartbeat an open club; clients receive only the relay-authored aggregate count.
const listener = await conn(generateSecretKey(), false)
const listenerCounts = host.watch({ kinds: [20106], '#h': [G], since: now() })
const rawListenerBeats = host.watch({ kinds: [20105], '#h': [G], since: now() })
await listenerCounts.ready
await rawListenerBeats.ready
const listenerOn = await listener.evRaw({ kind: 20105, created_at: now(), tags: [['h', G], ['state', 'on']], content: '' })
assert(listenerOn[0] === true, 'logged-out club page can register an anonymous listener session: ' + ok(listenerOn))
await sleep(600)
const liveCount = listenerCounts.got.at(-1)
assert(liveCount?.kind === 20106 && liveCount.pubkey === RELAY_PK, 'listener aggregate is emitted by the relay conductor')
assert(liveCount?.tags.find((t) => t[0] === 'count')?.[1] === '1', 'listener aggregate reports one live session')
assert(rawListenerBeats.got.length === 0, 'individual anonymous listener heartbeats are not exposed')
const forgedCount = await mem.evRaw({ kind: 20106, created_at: now(), tags: [['h', G], ['count', '99'], ['sent_at', String(Date.now())]], content: '' })
assert(forgedCount[0] === false && /relay-authored/i.test(forgedCount[1] || ''), 'client-forged listener aggregate is rejected')
const malformedListener = await listener.evRaw({ kind: 20105, created_at: now(), tags: [['h', G], ['state', 'maybe']], content: '' })
assert(malformedListener[0] === false && /listener update/i.test(malformedListener[1] || ''), 'malformed listener heartbeat is rejected')
await listener.evRaw({ kind: 20105, created_at: now(), tags: [['h', G], ['state', 'off']], content: '' })
await sleep(600)
assert(listenerCounts.got.at(-1)?.tags.find((t) => t[0] === 'count')?.[1] === '0', 'listener departure updates the aggregate to zero')
listenerCounts.close()
rawListenerBeats.close()

// 3. Playback state, play-log and Auto-DJ control are relay-authored ONLY — even a MEMBER's
//    write is rejected. Guards against forged canonical state and per-author tombstones. The
//    ReplaceEvent dedup of the relay's OWN writes is asserted in the conductor section below.
const memNp = await mem.ev({ kind: 30100, created_at: now(), tags: [['h', G], ['d', G], ['track', 'yt:AAA'], ['pos', '0']], content: 'member now_playing' })
assert(memNp[0] === false && /relay-authored/.test(memNp[1] || ''), 'member now_playing (30100) write rejected: ' + ok(memNp))
const memPlay = await mem.ev({ kind: 1313, created_at: now(), tags: [['h', G], ['p', mem.pub], ['pos', '0']], content: 'yt:AAA' })
assert(memPlay[0] === false && /relay-authored/.test(memPlay[1] || ''), 'member play-log (1313) write rejected: ' + ok(memPlay))
const memAutoCtrl = await mem.ev({ kind: 30111, created_at: now(), tags: [['h', G], ['d', G], ['armed', '0']], content: '' })
assert(memAutoCtrl[0] === false && /relay-authored/.test(memAutoCtrl[1] || ''), 'member Auto-DJ control (30111) write rejected: ' + ok(memAutoCtrl))
const npNone = await host.query({ kinds: [30100], '#h': [G] })
assert(npNone.length === 0, `no now_playing stored from a member write (got ${npNone.length})`)
const autoCtrlNone = await host.query({ kinds: [30111], '#h': [G] })
assert(autoCtrlNone.length === 0, `no Auto-DJ control stored from a member write (got ${autoCtrlNone.length})`)

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

let settledTrackClub = ''
let settledTrackVoter = ''

// 4c. Server conductor: with a DJ on stage and a non-empty queue, the RELAY itself (not any
//     client) publishes now_playing and advances the round-robin — the autonomous-playback
//     core. Also verifies the relay honors a skip-request (kind 30107). Long track durations
//     keep it deterministic (no time-based auto-advance/loop during the test). Needs RELAY_PK.
if (process.env.RELAY_PK) {
  const RPK = process.env.RELAY_PK
  const C = 'zc' + Math.random().toString(16).slice(2, 16)
  settledTrackClub = C
  await host.ev({ kind: 9007, created_at: now(), tags: [['h', C]], content: '' })
  await host.ev({ kind: 9002, created_at: now(), tags: [['h', C], ['name', 'Conductor'], ['open'], ['public']], content: '' })
  await sleep(500)
  // The DJ's QUEUE is the single source of truth: round-robin plays the top ACTIVE (not-`off`)
  // track; a played track is marked `off` (client does this in prod — simulated here) and drops
  // out; a re-activated track plays again. NO hidden relay played-set. Long durations → only an
  // explicit skip advances during the test window.
  const TT = [['VIDfirst001', 'First'], ['VIDsecond02', 'Second'], ['VIDthird0003', 'Third'], ['VIDfourth004', 'Fourth']]
  const postQueue = (off) =>
    host.ev({
      kind: 30103,
      created_at: now(),
      tags: [['h', C], ['d', C], ...TT.map(([id, t]) => (off.includes(id) ? ['track', `yt:${id}`, t, '300', 'off'] : ['track', `yt:${id}`, t, '300']))],
      content: '',
    })
  const npNow = async () => (await host.query({ kinds: [30100], '#h': [C] })).find((e) => e.pubkey === RPK)
  const trackOf = (np) => np && np.tags.find((t) => t[0] === 'track')?.[1]
  const posOf = (np) => (np && np.tags.find((t) => t[0] === 'pos')?.[1]) || '0'
  const skip = async (np) => { await host.ev({ kind: 30107, created_at: now(), tags: [['h', C], ['d', C], ['pos', posOf(np)]], content: '' }); await sleep(4000) }

  await host.ev({ kind: 30102, created_at: now(), tags: [['h', C], ['d', C], ['since', String(now())]], content: '' })
  await postQueue([]) // all active
  await sleep(4000) // conductor tick is 2.5s → it bootstraps now_playing within a tick
  let np = await npNow()
  const npRows = (await host.query({ kinds: [30100], '#h': [C] })).filter((e) => e.pubkey === RPK)
  assert(!!np, 'conductor: the RELAY published now_playing')
  assert(npRows.length === 1, `conductor: now_playing dedup — exactly 1 relay row (got ${npRows.length})`)
  assert(!!np && np.tags.find((t) => t[0] === 'dj')?.[1] === host.pub, 'conductor: now_playing dj = the stage DJ')
  assert(trackOf(np) === 'yt:VIDfirst001', 'conductor: plays the top track of the queue (position 1)')

  // Played track marked `off` → skip advances to the next ACTIVE track (off track drops out).
  await postQueue(['VIDfirst001'])
  await skip(np); np = await npNow()
  assert(trackOf(np) === 'yt:VIDsecond02', 'conductor: skip → next ACTIVE track (the off track is skipped)')

  // Queue order is the truth: mark Second off too → top active is now Third.
  await postQueue(['VIDfirst001', 'VIDsecond02'])
  await skip(np); np = await npNow()
  assert(trackOf(np) === 'yt:VIDthird0003', 'conductor: top active track is next (queue is the source of truth)')

  // RE-ACTIVATION: put First back to active (off→on) + mark Third off → a skip plays First AGAIN.
  // This is the key rule: a re-activated track at the top replays; the visible queue always wins.
  await postQueue(['VIDsecond02', 'VIDthird0003'])
  await skip(np); np = await npNow()
  assert(trackOf(np) === 'yt:VIDfirst001', 'conductor: a re-activated track replays (visible queue wins, no hidden played-set)')

  // role validation: a plain MEMBER (not owner/mod, not the playing DJ) cannot skip.
  await mem.ev({ kind: 9021, created_at: now(), tags: [['h', C]], content: '' })
  await sleep(800)
  await mem.ev({ kind: 30107, created_at: now(), tags: [['h', C], ['d', C], ['pos', posOf(np)]], content: '' })
  await sleep(4000)
  np = await npNow()
  assert(trackOf(np) === 'yt:VIDfirst001', 'conductor: a non-mod member’s skip-request is IGNORED (role validation)')

  // Vibemeter: the playing DJ cannot influence their own track. Three reactions from other
  // members can skip it, but one account cannot react twice inside the shared 10-second
  // cooldown. The resulting -1 credibility snapshot is relay-signed NIP-78 app data and
  // publicly queryable by its p tag.
  const voter = await conn(generateSecretKey())
  const bangerVoter = await conn(generateSecretKey())
  settledTrackVoter = bangerVoter.pub
  await Promise.all([
    stranger.ev({ kind: 9021, created_at: now(), tags: [['h', C]], content: '' }),
    voter.ev({ kind: 9021, created_at: now(), tags: [['h', C]], content: '' }),
    bangerVoter.ev({ kind: 9021, created_at: now(), tags: [['h', C]], content: '' }),
  ])
  await sleep(800)
  const moodTags = [['h', C], ['pos', posOf(np)]]
  const ownVote = await host.ev({ kind: 20104, created_at: now(), tags: [...moodTags, ['v', 'skip']], content: '' })
  assert(ownVote[0] === false && /own track/i.test(ownVote[1] || ''), 'vibemeter: the playing DJ cannot vote on their own track')
  await bangerVoter.ev({ kind: 20104, created_at: now(), tags: [...moodTags, ['v', 'banger']], content: '' })
  await mem.ev({ kind: 20104, created_at: now(), tags: [...moodTags, ['v', 'skip']], content: '' })
  const tooSoon = await mem.ev({ kind: 20104, created_at: now(), tags: [...moodTags, ['v', 'banger']], content: '' })
  assert(tooSoon[0] === false && /10 seconds/i.test(tooSoon[1] || ''), 'vibemeter: Banger and Skip share the 10-second cooldown')
  await stranger.ev({ kind: 20104, created_at: now(), tags: [...moodTags, ['v', 'skip']], content: '' })
  await voter.ev({ kind: 20104, created_at: now(), tags: [...moodTags, ['v', 'skip']], content: '' })
  await sleep(4000)
  np = await npNow()
  assert(trackOf(np) === 'yt:VIDfourth004', 'vibemeter: three skips advance to the next active track')
  const cred = (await host.query({ kinds: [30078], authors: [RPK], '#h': ['zapclub-credibility'], '#d': [`zapclub:credibility:${host.pub}`], '#p': [host.pub] }))[0]
  const credTag = (name) => Number(cred?.tags.find((t) => t[0] === name)?.[1])
  assert(!!cred && credTag('score') === -1 && credTag('skipped') === 1, 'credibility: relay-signed NIP-78 snapshot records exactly one minus point')

  // broken-track quorum: 2 members report the running track (Fourth) unplayable → relay skips it.
  // Re-activate First so there is a deterministic next track.
  await postQueue(['VIDsecond02', 'VIDthird0003', 'VIDfourth004'])
  await mem.ev({ kind: 20102, created_at: now(), tags: [['h', C]], content: 'VIDfourth004' })
  await stranger.ev({ kind: 20102, created_at: now(), tags: [['h', C]], content: 'VIDfourth004' })
  await sleep(4000)
  np = await npNow()
  assert(trackOf(np) === 'yt:VIDfirst001', 'conductor: broken-track quorum (2 members) skips to the next active track')

  // All tracks off → the stream stops to the lobby (nothing active to play; no auto-loop).
  await postQueue(['VIDfirst001', 'VIDsecond02', 'VIDthird0003', 'VIDfourth004'])
  await skip(np); np = await npNow()
  assert(trackOf(np) === 'yt:VIDfirst001', 'conductor: all tracks off → stops to the lobby (no active track, no loop)')
  await host.ev({ kind: 9008, created_at: now(), tags: [['h', C]], content: '' }) // cleanup
}

// 4c-bis. Anti-loop regression: a single ACTIVE track that the client NEVER marks `off` must play
//     once then stop to the lobby — not loop. (stop() used to clear the last videoID, so the next
//     bootstrap re-picked the just-played track → the last song looped.) A short duration makes the
//     relay auto-advance on its own clock; we never mark it off, then assert `pos` didn't climb.
if (process.env.RELAY_PK) {
  const RPK = process.env.RELAY_PK
  const L = 'zc' + Math.random().toString(16).slice(2, 16)
  console.log('\n-- conductor anti-loop --')
  await host.ev({ kind: 9007, created_at: now(), tags: [['h', L]], content: '' })
  await host.ev({ kind: 9002, created_at: now(), tags: [['h', L], ['name', 'Loop'], ['open'], ['public']], content: '' })
  await sleep(500)
  await host.ev({ kind: 30102, created_at: now(), tags: [['h', L], ['d', L], ['since', String(now())]], content: '' })
  // One ACTIVE track, 1s duration; never marked `off`.
  await host.ev({ kind: 30103, created_at: now(), tags: [['h', L], ['d', L], ['track', 'yt:LOOPtrack01', 'Only', '1']], content: '' })
  const npL = async () => (await host.query({ kinds: [30100], '#h': [L] })).find((e) => e.pubkey === RPK)
  const posN = (np) => Number((np && np.tags.find((t) => t[0] === 'pos')?.[1]) || '0')
  await sleep(4000) // relay plays the track (tick 2.5s)
  const first = await npL()
  assert(!!first && first.tags.find((t) => t[0] === 'track')?.[1] === 'yt:LOOPtrack01', 'anti-loop: the single track started')
  const pos1 = posN(first)
  await sleep(9000) // 1s track ends; several ticks pass — buggy code would re-pick and loop
  assert(posN(await npL()) === pos1, `anti-loop: last track did NOT loop (pos stayed ${pos1}, got ${posN(await npL())})`)
  await host.ev({ kind: 9008, created_at: now(), tags: [['h', L]], content: '' }) // cleanup
}

// 4d. Stage capacity: owner plus two members fill the three slots; a fourth member is rejected.
//     This covers the actual RejectEvent wiring in addition to the stage-gate unit tests.
{
  const S = 'zcs' + Math.random().toString(16).slice(2, 16)
  const [stageHost, stage2, stage3, stage4] = await Promise.all(
    Array.from({ length: 4 }, () => conn(generateSecretKey())),
  )
  console.log('\n-- three-DJ stage cap --')
  await stageHost.ev({ kind: 9007, created_at: now(), tags: [['h', S]], content: '' })
  await stageHost.ev({ kind: 9002, created_at: now(), tags: [['h', S], ['name', 'Three DJs'], ['open'], ['public']], content: '' })
  await Promise.all(
    [stage2, stage3, stage4].map((dj) => dj.ev({ kind: 9021, created_at: now(), tags: [['h', S]], content: '' })),
  )
  await sleep(700)

  const takeSlot = (dj, since) => dj.ev({ kind: 30102, created_at: now(), tags: [['h', S], ['d', S], ['since', String(since)]], content: 'on' })
  const firstStageSession = sessionEvent(stageHost.pub, generateSecretKey(), {
    kind: 30102, created_at: now(), tags: [['h', S], ['d', S], ['since', '1']], content: 'on',
  })
  const firstThree = await Promise.all([
    stageHost.sendEvent(firstStageSession),
    takeSlot(stage2, 2),
    takeSlot(stage3, 3),
  ])
  assert(firstThree.every((result) => result[0] === true), 'stage cap: first three DJs accepted')
  await sleep(500)

  // Rotate the page key while all slots are full. This is an existing-DJ heartbeat, not a
  // fourth participant, and the old author alias must be removed from persistent storage.
  const rotatedStageSession = sessionEvent(stageHost.pub, generateSecretKey(), {
    kind: 30102, created_at: now() + 1, tags: [['h', S], ['d', S], ['since', '1']], content: 'on',
  })
  const rotated = await stageHost.sendEvent(rotatedStageSession)
  assert(rotated[0] === true, 'rotated session key keeps the same effective stage slot: ' + ok(rotated))
  const stageRows = await stageHost.query({ kinds: [30102], '#h': [S] })
  const hostRows = stageRows.filter((event) =>
    event.tags.some((tag) => tag[0] === 'client' && tag[1] === SESSION_MARKER) &&
    event.tags.some((tag) => tag[0] === 'p' && tag[1] === stageHost.pub))
  assert(hostRows.length === 1 && hostRows[0].id === rotatedStageSession.id,
    `stage aliases collapse to exactly the newest effective-principal state (got ${hostRows.length})`)

  const fourth = await takeSlot(stage4, 4)
  assert(fourth[0] === false && /stage is full/i.test(fourth[1] || ''), 'stage cap: fourth DJ rejected: ' + ok(fourth))

  await stageHost.ev({ kind: 9001, created_at: now(), tags: [['h', S], ['p', stage2.pub]], content: '' })
  await sleep(500)
  const replacement = await takeSlot(stage4, 4)
  assert(replacement[0] === true, 'membership revocation immediately frees the former DJ stage slot: ' + ok(replacement))
  const stageAfterRevoke = await stageHost.query({ kinds: [30102], '#h': [S] })
  assert(!stageAfterRevoke.some((event) => event.pubkey === stage2.pub),
    'membership revocation removes the former DJ persistent stage lease')

  await stageHost.ev({ kind: 9008, created_at: now(), tags: [['h', S]], content: '' })
}

// 4e. Private / invite-only clubs.
//     - every owner can set ['closed'] and ['private'] on a 9002
//     - closed group: 9021 is stored but NOT auto-added to 39002 (no auto-join reactor)
//     - owner approves via 9000 put-user → joiner appears in 39002
//     - private club absent from global {kinds:[39000]} listing (no #d filter)
//     Uses a fresh key to avoid the 3-club cap (host has already created G, C, L).
{
  const GP = 'zcp' + Math.random().toString(16).slice(2, 16)
  const privHostSk = generateSecretKey() // fresh key: 0 clubs → no cap hit
  const privHost = await conn(privHostSk)
  const joinerP = await conn(generateSecretKey())
  console.log('\n-- private clubs --')

  await privHost.ev({ kind: 9007, created_at: now(), tags: [['h', GP]], content: '' })
  await privHost.ev({ kind: 9002, created_at: now(), tags: [['h', GP], ['name', 'PrvTest'], ['open'], ['public']], content: '' })
  await sleep(400)

  // Owner sets closed + private → accepted without an account tier.
  const privateOK = await privHost.evRaw({ kind: 9002, created_at: now() + 1, tags: [['h', GP], ['name', 'PrvTest'], ['closed'], ['private']], content: '' })
  assert(privateOK[0] === true, 'private gate: owner can set closed+private: ' + ok(privateOK))
  await sleep(400)

  // Private club must NOT appear in global {kinds:[39000]} listing for non-members.
  // (relay29: private groups are hidden from non-members in global listings; members/owner see them)
  const globalMetaStranger = await stranger.query({ kinds: [39000] })
  assert(!globalMetaStranger.some((e) => e.tags.some((t) => t[0] === 'd' && t[1] === GP)), 'private club absent from global 39000 listing (non-member)')

  // Private club IS findable by direct #d lookup (invite-link access) — no auth required.
  const directMeta = await stranger.query({ kinds: [39000], '#d': [GP] })
  assert(directMeta.some((e) => e.tags.some((t) => t[0] === 'd' && t[1] === GP)), 'private club found by direct #d lookup (non-member)')

  // Joiner self-joins a closed club → 9021 accepted (stored), but no auto-add to 39002.
  await joinerP.ev({ kind: 9021, created_at: now(), tags: [['h', GP]], content: '' })
  await sleep(600)
  const membersAfterJoin = (await privHost.query({ kinds: [39002], '#d': [GP] })).flatMap((e) => e.tags.filter((t) => t[0] === 'p').map((t) => t[1]))
  assert(!membersAfterJoin.includes(joinerP.pub), 'closed club: 9021 does NOT auto-add (no auto-join)')

  // Owner approves via 9000 put-user → joiner appears in 39002.
  await privHost.ev({ kind: 9000, created_at: now(), tags: [['h', GP], ['p', joinerP.pub]], content: '' })
  await sleep(500)
  const membersAfterApprove = (await privHost.query({ kinds: [39002], '#d': [GP] })).flatMap((e) => e.tags.filter((t) => t[0] === 'p').map((t) => t[1]))
  assert(membersAfterApprove.includes(joinerP.pub), 'closed club: owner 9000 put-user admits the joiner')

  await privHost.ev({ kind: 9008, created_at: now(), tags: [['h', GP]], content: '' }) // cleanup
}

// 4d. DJ leaderboard: the conductor's settled real-DJ tracks and Vibemeter aggregate drive
//     GET /leaderboard. Zap broadcasts still feed private payment history but cannot move rank.
//     Needs the conductor identity and HTTP base.
if (process.env.ADMIN_URL && process.env.RELAY_PK) {
  const LB = process.env.ADMIN_URL
  console.log('\n-- DJ leaderboard --')
  const beforeZap = await fetch(LB + '/leaderboard?pubkey=' + host.pub).then((x) => x.json())
  assert(beforeZap.ranked === true && beforeZap.pubkey === host.pub, `leaderboard: settled real DJ is ranked — got ${JSON.stringify(beforeZap)}`)
  assert(Number.isInteger(beforeZap.score) && beforeZap.tracks === 2 && beforeZap.vibeScore === -1 && beforeZap.skipped === 1,
    `leaderboard: only one naturally finished and one community-skipped song count; manual/broken skips do not — got ${JSON.stringify(beforeZap)}`)
  assert(!('sats' in beforeZap) && !('zaps' in beforeZap), 'leaderboard: payment totals are absent from the DJ rank')
  const publicBoard = await fetch(LB + '/leaderboard').then((x) => x.json())
  const ratedTrack = publicBoard.topTracks?.find((track) => track.club === settledTrackClub && track.videoId === 'VIDfirst001')
  assert(ratedTrack?.title === 'First' && ratedTrack.dj === host.pub && ratedTrack.bangers === 1 && ratedTrack.skipped === true,
    `leaderboard: settled Vibemeter result retains title, club and DJ — got ${JSON.stringify(publicBoard.topTracks)}`)
  assert(!JSON.stringify(publicBoard.topTracks).includes(settledTrackVoter), 'leaderboard: track aggregate does not expose voter identities')

  // mem zaps host 210 sats; a duplicate still must not affect payment history or DJ rank.
  const zb = await mem.ev({ kind: 20101, created_at: now(), tags: [['h', G], ['p', host.pub], ['amount', '210'], ['bolt11', 'lnbc_e2e_1']], content: '' })
  assert(zb[0] === true, 'zap history: member zap broadcast (20101) accepted: ' + ok(zb))
  await mem.ev({ kind: 20101, created_at: now() + 1, tags: [['h', G], ['p', host.pub], ['amount', '210'], ['bolt11', 'lnbc_e2e_1']], content: '' })
  await sleep(900)
  const afterZap = await fetch(LB + '/leaderboard?pubkey=' + host.pub).then((x) => x.json())
  assert(afterZap.score === beforeZap.score && afterZap.tracks === beforeZap.tracks &&
    afterZap.vibeScore === beforeZap.vibeScore && afterZap.rank === beforeZap.rank,
    `leaderboard: zap cannot change DJ performance rank — before ${JSON.stringify(beforeZap)}, after ${JSON.stringify(afterZap)}`)

  // A self-zap is ignored by the separate history and likewise cannot change the board.
  await host.ev({ kind: 20101, created_at: now(), tags: [['h', G], ['p', host.pub], ['amount', '9999'], ['bolt11', 'self_e2e']], content: '' })
  await sleep(700)
  const afterSelfZap = await fetch(LB + '/leaderboard?pubkey=' + host.pub).then((x) => x.json())
  assert(afterSelfZap.score === beforeZap.score && afterSelfZap.tracks === beforeZap.tracks,
    `leaderboard: self-zap leaves DJ score unchanged — got ${JSON.stringify(afterSelfZap)}`)
  // A member who reacted but never played a settled song is unranked.
  const nonDJ = await fetch(LB + '/leaderboard?pubkey=' + stranger.pub).then((x) => x.json())
  assert(nonDJ.ranked === false, 'leaderboard: a member without a settled DJ song is unranked')
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

  // Listener analytics use anonymous club-page sessions rather than member presence.
  await listener.evRaw({ kind: 20105, created_at: now(), tags: [['h', G], ['state', 'on']], content: '' })
  await sleep(400)
  const lis = await adminReq('/admin/listeners', 'GET')
  let lj = {}
  try { lj = JSON.parse(lis.body) } catch { /* ignore */ }
  const clubL = (lj.clubs || []).find((c) => c.id === G)
  assert(lis.status === 200 && !!clubL && clubL.live.includes(listener.pub), 'listeners: anonymous session shows as live in the club')
  assert(!!clubL && clubL.seen.some((s) => s.pubkey === listener.pub), 'listeners: anonymous session appears in the 24h seen list')

  const stageBeforeBan = await mem.sendEvent(sessionEvent(mem.pub, generateSecretKey(), {
    kind: 30102, created_at: now(), tags: [['h', G], ['d', G], ['since', String(now())]], content: 'on',
  }))
  assert(stageBeforeBan[0] === true, 'session stage exists before principal ban: ' + ok(stageBeforeBan))
  const banLiveChat = mem.watch({ kinds: [9], '#h': [G], since: now() })
  await banLiveChat.ready

  const ban = await adminReq('/admin/ban', 'POST', { pubkey: mem.pub, reason: 'e2e' })
  assert(ban.status === 200, 'ban → 200 (got ' + ban.status + ' ' + ban.body.slice(0, 60) + ')')
  await sleep(500)

  const afterBan = await mem.ev({ kind: 9, created_at: now(), tags: [['h', G]], content: 'still here?' })
  assert(afterBan[0] === false, 'banned member write rejected: ' + ok(afterBan))
  const sessionAfterBan = await mem.sendEvent(sessionEvent(mem.pub, generateSecretKey(), {
    kind: 20100, created_at: now(), tags: [['h', G]], content: '',
  }))
  assert(sessionAfterBan[0] === false && /banned/i.test(sessionAfterBan[1] || ''),
    'ban applies to a session event’s effective principal: ' + ok(sessionAfterBan))
  const stageAfterBan = await host.query({ kinds: [30102], '#h': [G] })
  assert(!stageAfterBan.some((event) => event.tags.some((tag) => tag[0] === 'p' && tag[1] === mem.pub)),
    'ban purges persistent stage aliases of the effective principal')
  const bannedHistory = await mem.queryResult({ kinds: [9], '#h': [G] })
  assert(/restricted/i.test(bannedHistory.closed), 'banned member cannot read protected chat history')
  await host.ev({ kind: 9, created_at: now(), tags: [['h', G]], content: 'during ban' })
  await sleep(300)
  assert(!banLiveChat.got.some((event) => event.content === 'during ban'),
    'ban blocks protected pushes on an already-open subscription')
  const bannedPublic = await mem.query({ kinds: [30112], '#h': [G] })
  assert(bannedPublic.length > 0, 'banned member can still read public club state')

  const replay = await adminReq('/admin/ban', 'POST', { pubkey: mem.pub }, ban.auth)
  assert(replay.status === 401, 'NIP-98 token replay rejected → 401 (got ' + replay.status + ')')

  const unban = await adminReq('/admin/unban', 'POST', { pubkey: mem.pub })
  assert(unban.status === 200, 'unban → 200 (got ' + unban.status + ')')
  await sleep(400)
  const afterUnban = await mem.ev({ kind: 9, created_at: now(), tags: [['h', G]], content: 'back' })
  assert(afterUnban[0] === true, 'unbanned member can write again: ' + ok(afterUnban))
  await host.ev({ kind: 9, created_at: now(), tags: [['h', G]], content: 'after unban' })
  await sleep(300)
  assert(banLiveChat.got.some((event) => event.content === 'after unban'),
    'unban restores protected pushes on the existing subscription')
  banLiveChat.close()

  // Keep a virtual participant armed across the administrative delete. The endpoint must
  // clear runtime indexes as well as Badger rows; otherwise later conductor ticks resurrect
  // now_playing/play events for a club that no longer exists.
  const deleteWatch = host.watch({
    kinds: [30100, 1313, 30103, 30111, 20106, 30112],
    '#h': [G],
    since: now(),
  })
  await deleteWatch.ready
  const armBeforeDelete = await host.ev({
    kind: 30105,
    created_at: now(),
    tags: [['h', G], ['d', G], ['status', 'armed'], ['track', 'yt:DELETEauto01', 'Delete race', '120']],
    content: 'delete-race',
  })
  assert(armBeforeDelete[0] === true, 'Auto DJ armed before administrative club deletion: ' + ok(armBeforeDelete))
  await sleep(3000)

  const del = await adminReq('/admin/delete-club', 'POST', { groupId: G })
  assert(del.status === 200, 'delete-club → 200 (got ' + del.status + ')')
  await sleep(150)
  const deliveredAtDelete = deleteWatch.got.length
  // Three scheduler cycles are enough to expose a stale active/Auto-DJ snapshot.
  await sleep(7500)
  const metaAfter = await host.query({ kinds: [39000], '#d': [G] })
  assert(metaAfter.length === 0, 'club metadata gone after delete-club (got ' + metaAfter.length + ')')
  const authorityAfter = await host.query({
    kinds: [30100, 1313, 30103, 30111, 20106, 30112],
    '#h': [G],
  })
  assert(authorityAfter.length === 0,
    `deleted club stays silent across later conductor/listener ticks (got ${authorityAfter.length} events)`)
  assert(deleteWatch.got.length === deliveredAtDelete,
    `deleted club emits no new live authority events across later ticks (got ${deleteWatch.got.length - deliveredAtDelete})`)
  deleteWatch.close()
  const listenersAfterDelete = await adminReq('/admin/listeners', 'GET')
  let listenersAfter = {}
  try { listenersAfter = JSON.parse(listenersAfterDelete.body) } catch { /* ignore */ }
  assert(!(listenersAfter.clubs || []).some((club) => club.id === G),
    'administrative delete removes retained listener analytics for the club')

  // host created G plus the two conductor fixture clubs, reaching the account cap. Removing G
  // destructively removes its 9007 row, so the in-memory cap must release that exact count too.
  const replacement = 'zc' + Math.random().toString(16).slice(2, 16)
  const replacementCreate = await host.ev({ kind: 9007, created_at: now(), tags: [['h', replacement]], content: '' })
  assert(replacementCreate[0] === true, 'administrative delete releases the owner club-cap slot: ' + ok(replacementCreate))
  if (replacementCreate[0]) {
    await host.ev({ kind: 9008, created_at: now(), tags: [['h', replacement]], content: '' })
  }
  cleaned = true
}

if (!cleaned) await host.ev({ kind: 9008, created_at: now(), tags: [['h', G]], content: '' }) // delete group (cleanup)
console.log(failures === 0 ? '\nALL PASSED' : `\n${failures} FAILED`)
process.exit(failures === 0 ? 0 : 1)

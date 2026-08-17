import { SimplePool } from 'nostr-tools/pool';
import { useWebSocketImplementation } from 'nostr-tools/relay';
import WebSocket from 'ws';

useWebSocketImplementation(WebSocket);

const RELAY = 'wss://relay.zapclub.io';
const pool = new SimplePool();
const clubId = 'c7ca6a16dd1ed946';
const now = Date.now();

const [npEvs, stageEvs] = await Promise.all([
  pool.querySync([RELAY], { kinds: [30100], '#h': [clubId], limit: 1 }),
  pool.querySync([RELAY], { kinds: [30102], '#h': [clubId], limit: 50 }),
]);

if (npEvs[0]) {
  const np = npEvs[0];
  const t = (tag) => np.tags?.find(t=>t[0]===tag)?.[1];
  const sentAt = Number(t('sent_at') ?? 0);
  const startedAt = Number(t('started_at') ?? 0);
  const dur = Number(t('duration') ?? 0);
  const elapsed = Math.round((now - startedAt) / 1000);
  const ageS = Math.round((now - sentAt) / 1000);
  console.log('Now playing:');
  console.log(`  pos=${t('pos')} dj=${t('dj')?.slice(0,8)} vid=${t('track')} status=${t('status')}`);
  console.log(`  elapsed=${elapsed}s dur=${dur}s remaining=${dur-elapsed}s beatAge=${ageS}s`);
  console.log(`  "${np.content}"`);
}

// Current active DJs
const STALE = 3_600_000;
const byPk = {};
for (const ev of stageEvs) {
  if (!byPk[ev.pubkey] || ev.created_at > byPk[ev.pubkey].created_at) byPk[ev.pubkey] = ev;
}
const active = Object.values(byPk)
  .filter(e => e.content !== 'off' && (now - e.created_at*1000) < STALE)
  .sort((a,b) => Number(a.tags?.find(t=>t[0]==='since')?.[1]??0) - Number(b.tags?.find(t=>t[0]==='since')?.[1]??0));

console.log(`\nActive DJs (${active.length}):`);
for (const d of active) {
  const age = Math.round((now - d.created_at*1000)/60000);
  const since = d.tags?.find(t=>t[0]==='since')?.[1];
  console.log(`  ${d.pubkey.slice(0,8)} hb=${age}min since=${since}`);
}

pool.close([RELAY]);
process.exit(0);

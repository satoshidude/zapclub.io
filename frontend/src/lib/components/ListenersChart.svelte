<script lang="ts">
  import { npubEncode } from 'nostr-tools/nip19'
  import { goUser } from '../router.svelte'
  import { useProfile, displayName, avatarUrl } from '../nostr/profiles.svelte'
  import type { ListenersResp, ClubListeners } from '../nostr/admin'

  let { data, clubName }: { data: ListenersResp; clubName: (id: string) => string } = $props()

  // SVG canvas (scaled to 100% width via viewBox).
  const W = 720
  const H = 150
  const PAD = 6

  const bucket = $derived(data.bucketMs || 300_000)
  const end = $derived(data.generatedAt)
  const start = $derived(data.generatedAt - data.windowMs)

  // Total concurrent listeners per bucket across all clubs (the headline 24h diagram),
  // with missing buckets filled as 0 so the timeline is continuous.
  const totals = $derived.by(() => {
    const m = new Map<number, number>()
    for (const c of data.clubs) for (const s of c.series) m.set(s.t, (m.get(s.t) ?? 0) + s.n)
    const pts: { t: number; n: number }[] = []
    const first = Math.floor(start / bucket) * bucket
    for (let t = first; t <= end; t += bucket) pts.push({ t, n: m.get(t) ?? 0 })
    return pts
  })
  const peak = $derived(totals.reduce((a, p) => Math.max(a, p.n), 0))
  const yMax = $derived(Math.max(1, peak))
  const liveNow = $derived(data.clubs.reduce((a, c) => a + c.live.length, 0))
  const unique24h = $derived.by(() => {
    const set = new Set<string>()
    for (const c of data.clubs) for (const s of c.seen) set.add(s.pubkey)
    return set.size
  })

  const x = (t: number) => PAD + ((t - start) / (end - start)) * (W - 2 * PAD)
  const y = (n: number) => H - PAD - (n / yMax) * (H - 2 * PAD)

  const linePath = $derived(totals.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)} ${y(p.n).toFixed(1)}`).join(' '))
  const areaPath = $derived(
    totals.length
      ? `M${x(totals[0].t).toFixed(1)} ${(H - PAD).toFixed(1)} ` +
          totals.map((p) => `L${x(p.t).toFixed(1)} ${y(p.n).toFixed(1)}`).join(' ') +
          ` L${x(totals[totals.length - 1].t).toFixed(1)} ${(H - PAD).toFixed(1)} Z`
      : '',
  )

  // Per-club sparkline (its own series), same time axis.
  function sparkPath(c: ClubListeners): string {
    if (!c.series.length) return ''
    const m = new Map(c.series.map((s) => [s.t, s.n]))
    const first = Math.floor(start / bucket) * bucket
    const pts: string[] = []
    let i = 0
    for (let t = first; t <= end; t += bucket, i++) {
      const sx = (PAD + ((t - start) / (end - start)) * (W - 2 * PAD)).toFixed(1)
      const sy = (H - PAD - ((m.get(t) ?? 0) / yMax) * (H - 2 * PAD)).toFixed(1)
      pts.push(`${i ? 'L' : 'M'}${sx} ${sy}`)
    }
    return pts.join(' ')
  }

  const hourTicks = $derived.by(() => {
    // every 6h: -24h, -18h, -12h, -6h, now
    const ticks: { x: number; label: string }[] = []
    for (let h = 24; h >= 0; h -= 6) {
      const t = end - h * 3_600_000
      ticks.push({ x: x(t), label: h === 0 ? 'now' : `-${h}h` })
    }
    return ticks
  })

  let expanded = $state<Record<string, boolean>>({})
  const fmtTime = (ms: number) =>
    new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
</script>

<div class="lc">
  <div class="head">
    <h2>Listeners <span class="count">last 24h</span></h2>
    <div class="stats">
      <span class="stat live">🎧 {liveNow} now</span>
      <span class="stat">▲ peak {peak}</span>
      <span class="stat">{unique24h} unique</span>
    </div>
  </div>

  <svg class="chart" viewBox="0 0 {W} {H}" preserveAspectRatio="none" role="img" aria-label="Total listeners over the last 24 hours">
    {#each hourTicks as tk (tk.label)}
      <line x1={tk.x} y1={PAD} x2={tk.x} y2={H - PAD} class="grid" />
    {/each}
    {#if areaPath}<path d={areaPath} class="area" />{/if}
    {#if linePath}<path d={linePath} class="line" />{/if}
  </svg>
  <div class="axis">
    {#each hourTicks as tk (tk.label)}
      <span style="left:{(tk.x / W) * 100}%">{tk.label}</span>
    {/each}
  </div>

  {#if data.clubs.length === 0}
    <p class="dim">No listener activity recorded yet.</p>
  {:else}
    <ul class="clubs">
      {#each data.clubs as c (c.id)}
        <li class="club">
          <button class="row" onclick={() => (expanded = { ...expanded, [c.id]: !expanded[c.id] })}>
            <span class="chev" class:open={expanded[c.id]}>▸</span>
            <span class="cname">{clubName(c.id)}</span>
            <svg class="spark" viewBox="0 0 {W} {H}" preserveAspectRatio="none" aria-hidden="true">
              <path d={sparkPath(c)} class="line" />
            </svg>
            <span class="live-badge" class:on={c.live.length > 0}>{c.live.length} now</span>
            <span class="seen-n">{c.seen.length}</span>
          </button>
          {#if expanded[c.id]}
            <ul class="seen">
              {#each c.seen as s (s.pubkey)}
                {@const p = useProfile(s.pubkey)}
                {@const online = data.generatedAt - s.last < 60_000}
                <li>
                  <img class="av" class:online src={avatarUrl(s.pubkey, p)} alt="" width="20" height="20" />
                  <a class="who" href={`/user/${npubEncode(s.pubkey)}`} onclick={(e) => { e.preventDefault(); goUser(npubEncode(s.pubkey)) }}>{displayName(s.pubkey, p)}</a>
                  <span class="when">{fmtTime(s.first)}–{online ? 'now' : fmtTime(s.last)}</span>
                </li>
              {/each}
            </ul>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .lc {
    margin-bottom: 1rem;
  }
  .head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.6rem;
    flex-wrap: wrap;
  }
  h2 {
    font-size: 1.05rem;
    margin: 0 0 0.6rem;
  }
  .count {
    font-size: 0.8rem;
    color: var(--text-dim);
    font-weight: 600;
  }
  .stats {
    display: flex;
    gap: 0.4rem;
  }
  .stat {
    font-size: 0.72rem;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.1rem 0.5rem;
  }
  .stat.live {
    color: var(--accent);
    border-color: var(--accent);
    font-weight: 600;
  }
  .chart {
    width: 100%;
    height: 150px;
    display: block;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .grid {
    stroke: var(--border);
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }
  .area {
    fill: color-mix(in srgb, var(--accent) 18%, transparent);
    stroke: none;
  }
  .line {
    fill: none;
    stroke: var(--accent);
    stroke-width: 2;
    vector-effect: non-scaling-stroke;
    stroke-linejoin: round;
  }
  .axis {
    position: relative;
    height: 1rem;
    margin-top: 2px;
  }
  .axis span {
    position: absolute;
    transform: translateX(-50%);
    font-size: 0.62rem;
    color: var(--text-dim);
  }
  .clubs {
    list-style: none;
    margin: 0.8rem 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    width: 100%;
    background: none;
    border: none;
    border-top: 1px solid var(--border);
    padding: 0.45rem 0.2rem;
    cursor: pointer;
    color: var(--text);
    text-align: left;
  }
  .chev {
    flex: 0 0 auto;
    color: var(--text-dim);
    font-size: 0.7rem;
    transition: transform 0.15s ease;
  }
  .chev.open {
    transform: rotate(90deg);
  }
  .cname {
    flex: 0 0 28%;
    font-weight: 600;
    font-size: 0.85rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .spark {
    flex: 1;
    height: 26px;
    min-width: 0;
    opacity: 0.7;
  }
  .live-badge {
    flex: 0 0 auto;
    font-size: 0.68rem;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.05rem 0.45rem;
  }
  .live-badge.on {
    color: var(--accent);
    border-color: var(--accent);
    font-weight: 700;
  }
  .seen-n {
    flex: 0 0 1.6rem;
    text-align: right;
    font-size: 0.7rem;
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
  }
  .seen {
    list-style: none;
    margin: 0 0 0.4rem 1.4rem;
    padding: 0.3rem 0 0;
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }
  .seen li {
    display: flex;
    align-items: center;
    gap: 0.45rem;
  }
  .av {
    border-radius: 999px;
    object-fit: cover;
    background: var(--bg-elev-2);
    flex: 0 0 auto;
  }
  .av.online {
    box-shadow: 0 0 0 2px var(--accent);
  }
  .who {
    color: var(--text);
    text-decoration: none;
    font-weight: 600;
    font-size: 0.82rem;
  }
  .who:hover {
    color: var(--accent-2);
  }
  .when {
    margin-left: auto;
    font-size: 0.7rem;
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
  }
  .dim {
    color: var(--text-dim);
  }
</style>

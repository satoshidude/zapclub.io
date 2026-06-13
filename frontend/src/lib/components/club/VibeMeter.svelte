<script lang="ts">
  import { vibeMeter, sendMood, optimisticVote } from '../../nostr/mood.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import { sync } from '../../nostr/sync.svelte'
  import Fireworks from './Fireworks.svelte'

  let { clubId = '' }: { clubId?: string } = $props()

  const pos     = $derived(sync.live?.pos ?? -1)
  const bangers = $derived(pos >= 0 ? vibeMeter.bangerCount(clubId, pos) : 0)
  const skips   = $derived(pos >= 0 ? vibeMeter.skipCount(clubId, pos) : 0)
  const level   = $derived(Math.max(-2, Math.min(2, bangers - skips)))
  const activeIdx = $derived(level + 2)
  const ownVote   = $derived(pos >= 0 ? vibeMeter.ownVote(clubId, pos) : null)

  const COOLDOWN_MS = 60_000
  let lastVoteMs = $state(0)
  $effect(() => { void pos; lastVoteMs = 0 })
  let nowMs = $state(Date.now())
  $effect(() => {
    const t = setInterval(() => (nowMs = Date.now()), 1000)
    return () => clearInterval(t)
  })
  const cooldown = $derived(
    lastVoteMs === 0 ? 0 : Math.max(0, Math.ceil((lastVoteMs + COOLDOWN_MS - nowMs) / 1000))
  )
  const canVote = $derived(auth.canSign && pos >= 0 && !!sync.live && cooldown === 0)

  const NAMES = ['skip', 'meh vibes', 'in the groove', 'heat rising', 'banger']
  const LABEL_COLS = ['#5588ff', '#88ccff', '#cc88ff', '#ffbb44', '#ff2d78']

  // ── SVG gauge geometry ────────────────────────────────────────────────────
  // viewBox: -10 -5 220 150  →  left:-10, top:-5, right:210, bottom:145
  const CX = 100, CY = 110
  const Ro = 85, Ri = 63   // outer / inner arc radii
  const SPAN = 80           // degrees either side of top (160° total)

  // Segment colours (retro: steel-blue, slate-blue, purple, amber-bronze)
  const SEG_COLS = ['#4a6080', '#596e84', '#664ea8', '#8a6a20']

  function ptArr(r: number, deg: number): [number, number] {
    const rad = deg * Math.PI / 180
    return [CX + r * Math.sin(rad), CY - r * Math.cos(rad)]
  }

  function arcPath(from: number, to: number, ro: number, ri: number, gap = 2): string {
    const t1 = from + gap, t2 = to - gap
    const [ox1, oy1] = ptArr(ro, t1), [ox2, oy2] = ptArr(ro, t2)
    const [ix2, iy2] = ptArr(ri, t2), [ix1, iy1] = ptArr(ri, t1)
    const large = Math.abs(t2 - t1) > 180 ? 1 : 0
    const f = (n: number) => n.toFixed(2)
    return `M ${f(ox1)} ${f(oy1)} A ${ro} ${ro} 0 ${large} 1 ${f(ox2)} ${f(oy2)} L ${f(ix2)} ${f(iy2)} A ${ri} ${ri} 0 ${large} 0 ${f(ix1)} ${f(iy1)} Z`
  }

  const SEGS = [
    { from: -SPAN,       to: -SPAN / 2, col: SEG_COLS[0] },
    { from: -SPAN / 2,  to: 0,          col: SEG_COLS[1] },
    { from: 0,           to:  SPAN / 2, col: SEG_COLS[2] },
    { from:  SPAN / 2,  to:  SPAN,      col: SEG_COLS[3] },
  ]

  const TICKS = [
    { angle: -SPAN,      label: '-2' },
    { angle: -SPAN / 2,  label: '-1' },
    { angle:  SPAN / 2,  label: '+1' },
    { angle:  SPAN,      label: '+2' },
  ]

  // Needle rotates from -SPAN (level=-2) to +SPAN (level=+2)
  const needleAngle = $derived(level * (SPAN / 2))

  const activeCol  = $derived(LABEL_COLS[activeIdx])
  const labelName  = $derived(NAMES[activeIdx].toUpperCase())

  // ── Fireworks ─────────────────────────────────────────────────────────────
  let fireworks = $state(false)
  $effect(() => {
    if (pos < 0) return
    void bangers
    if (vibeMeter.checkBanger(clubId, pos)) {
      fireworks = true
      const t = setTimeout(() => (fireworks = false), 2800)
      return () => clearTimeout(t)
    }
  })

  async function vote(v: 'banger' | 'skip') {
    if (!canVote || !auth.pubkey) return
    lastVoteMs = Date.now()
    optimisticVote(clubId, pos, auth.pubkey, v)
    try { await sendMood(clubId, pos, v) } catch { }
  }
</script>

<Fireworks show={fireworks} />

<div class="vm">
  <button class="vbtn skip" class:active={ownVote === 'skip'}
    onclick={() => vote('skip')} disabled={!canVote}
    aria-label="Vote skip">
    <span class="btn-icon">👎</span>
    <span class="btn-lbl">skip</span>
    {#if cooldown > 0}<span class="btn-cd">{cooldown}s</span>{/if}
  </button>

  <div class="gauge-wrap">
    <svg viewBox="-10 -5 220 150" xmlns="http://www.w3.org/2000/svg" class="gauge-svg" aria-hidden="true">
      <defs>
        <filter id="vm-needle-glow" x="-80%" y="-80%" width="260%" height="260%">
          <feGaussianBlur stdDeviation="3" result="blur"/>
          <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
        </filter>
        <filter id="vm-dot-glow" x="-120%" y="-120%" width="340%" height="340%">
          <feGaussianBlur stdDeviation="4" result="blur"/>
          <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
        </filter>
        <filter id="vm-txt-glow" x="-40%" y="-80%" width="180%" height="260%">
          <feGaussianBlur stdDeviation="2.5" result="blur"/>
          <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
        </filter>
      </defs>

      <!-- Arc segments -->
      {#each SEGS as seg}
        <path d={arcPath(seg.from, seg.to, Ro, Ri)} fill={seg.col} opacity="0.88"/>
      {/each}

      <!-- Tick marks (radial lines between inner edge and just outside outer edge) -->
      {#each TICKS as tick}
        {@const [x1, y1] = ptArr(Ri - 5, tick.angle)}
        {@const [x2, y2] = ptArr(Ro + 5, tick.angle)}
        <line
          x1={x1.toFixed(1)} y1={y1.toFixed(1)}
          x2={x2.toFixed(1)} y2={y2.toFixed(1)}
          stroke="#9aabb8" stroke-width="1.5" opacity="0.55"
        />
      {/each}

      <!-- Tick labels outside arc -->
      {#each TICKS as tick}
        {@const [lx, ly] = ptArr(Ro + 17, tick.angle)}
        <text
          x={lx.toFixed(1)} y={ly.toFixed(1)}
          text-anchor="middle" dominant-baseline="middle"
          font-family="'Courier New', monospace"
          font-size="11" font-weight="600"
          fill="#7a9ab0" opacity="0.85"
        >{tick.label}</text>
      {/each}

      <!-- Needle — tonearm style: thick rounded bar, pivot at center, tip near arc -->
      <g transform="translate({CX} {CY})">
        <g style="transform: rotate({needleAngle}deg); transition: transform 0.45s cubic-bezier(0.34,1.56,0.64,1); transform-origin: 0 0">
          <line x1="0" y1="10" x2="0" y2="-75"
            stroke="#a060e8" stroke-width="5.5" stroke-linecap="round"
            filter="url(#vm-needle-glow)"
          />
          <!-- Stylus tip dot -->
          <circle cx="0" cy="-75" r="3.5" fill="#c899ff"/>
        </g>
      </g>

      <!-- Pivot circle (bright green, layered) -->
      <circle cx={CX} cy={CY} r="9"  fill="#00ff88" filter="url(#vm-dot-glow)" opacity="0.6"/>
      <circle cx={CX} cy={CY} r="8"  fill="#00cc66"/>
      <circle cx={CX} cy={CY} r="5"  fill="#0a0a0f"/>
      <circle cx={CX} cy={CY} r="3"  fill="#00ff88"/>

      <!-- State label -->
      <text
        x={CX} y={CY + 30}
        text-anchor="middle" dominant-baseline="middle"
        font-family="'Courier New', monospace"
        font-size="11" font-weight="700" letter-spacing="2"
        fill={activeCol}
        filter="url(#vm-txt-glow)"
      >{labelName}</text>
    </svg>
  </div>

  <button class="vbtn banger" class:active={ownVote === 'banger'}
    onclick={() => vote('banger')} disabled={!canVote}
    aria-label="Vote banger">
    <span class="btn-icon">🔥</span>
    <span class="btn-lbl">banger</span>
    {#if cooldown > 0}<span class="btn-cd">{cooldown}s</span>{/if}
  </button>
</div>

<style>
  .vm {
    background: #0a0a0f;
    border-radius: 12px;
    padding: 0.4rem 0.6rem;
    width: 100%;
    display: flex;
    flex-direction: row;
    align-items: center;
    justify-content: center;
    gap: 0.6rem;
    box-sizing: border-box;
  }

  .gauge-wrap {
    flex: 1 1 0;
    min-width: 0;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .gauge-svg {
    display: block;
    width: 100%;
    height: auto;
  }

  /* ── Vote buttons ── */
  .vbtn {
    flex: 0 0 72px;
    align-self: stretch;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.25rem;
    padding: 0.5rem 0.25rem;
    border-radius: 10px;
    border: 1.5px solid transparent;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s, transform 0.1s, box-shadow 0.15s;
    min-height: 80px;
  }
  .vbtn:hover:not(:disabled) { transform: scale(1.05); }
  .vbtn:active:not(:disabled) { transform: scale(0.95); }
  .vbtn:disabled { opacity: 0.35; cursor: default; }

  .btn-icon { font-size: 1.3rem; line-height: 1; }
  .btn-lbl {
    font-size: 0.62rem;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .btn-cd {
    font-family: 'Courier New', monospace;
    font-size: 0.68rem;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    opacity: 0.7;
    margin-top: 0.1rem;
  }

  .vbtn.skip {
    background: #0d1625;
    border-color: #1e3050;
    color: #5a8ab8;
  }
  .vbtn.skip:hover:not(:disabled) {
    background: #111d35;
    border-color: #2a4a70;
    color: #7aaad8;
  }
  .vbtn.skip.active {
    background: #0e1e3a;
    border-color: #4488ff;
    color: #88bbff;
    box-shadow: 0 0 12px rgba(68,136,255,.25), inset 0 0 8px rgba(68,136,255,.08);
  }

  .vbtn.banger {
    background: #1a1008;
    border-color: #3a2010;
    color: #b07830;
  }
  .vbtn.banger:hover:not(:disabled) {
    background: #221508;
    border-color: #5a3018;
    color: #d09040;
  }
  .vbtn.banger.active {
    background: #2a1010;
    border-color: #ff2d78;
    color: #ff9060;
    box-shadow: 0 0 12px rgba(255,45,120,.25), inset 0 0 8px rgba(255,45,120,.08);
  }

  @media (max-width: 380px) {
    .vm {
      flex-direction: column;
      padding: 0.4rem 0.6rem 0.5rem;
    }
    .gauge-wrap { max-width: 220px; width: 100%; }
    .vbtn {
      flex: 1;
      flex-direction: row;
      align-self: auto;
      min-height: 44px;
      gap: 0.4rem;
      padding: 0.4rem 0.5rem;
    }
    .btn-icon { font-size: 1rem; }
  }
</style>

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

  // ── Per-minute cooldown ──────────────────────────────────────────────────────
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

  // ── Segment metadata ─────────────────────────────────────────────────────────
  const COLS  = ['#4488ff','#77ccff','#cc88ff','#ffbb44','#ff2d78']
  const NAMES = ['skip','meh vibes','in the groove','heat rising','banger']

  const activeCol = $derived(COLS[activeIdx])

  // ── Canvas gauge ─────────────────────────────────────────────────────────────
  let canvasEl = $state<HTMLCanvasElement | undefined>()
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let gauge: any = null

  $effect(() => {
    const el = canvasEl
    if (!el) return
    let alive = true
    import('canvas-gauges').then((mod: Record<string, unknown>) => {
      if (!alive || !el) return
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const RadialGauge = mod.RadialGauge as any
      gauge = new RadialGauge({
        renderTo: el,
        width: 400,  // high-res internal buffer; CSS scales it down
        height: 210, // tight crop — arc sits in top 85% with these angles
        units: false,
        title: false,
        minValue: -2,
        maxValue: 2,
        majorTicks: ['-2','-1','0','+1','+2'],
        minorTicks: 0,
        strokeTicks: false,
        ticksAngle: 155,
        startAngle: 102,
        colorPlate: '#0a0a0f',
        colorPlateEnd: '#0a0a0f',
        borders: false,
        borderShadowWidth: 0,
        needleType: 'arrow',
        needleWidth: 3,
        needleCircleSize: 9,
        needleCircleOuter: true,
        needleCircleInner: false,
        colorNeedle: '#ff2d78',
        colorNeedleEnd: '#ff2d78aa',
        colorNeedleShadowUp: 'rgba(0,255,180,0.15)',
        colorNeedleShadowDown: 'rgba(0,0,0,0.4)',
        colorNeedleCircleOuter: '#00ffb4',
        colorNeedleCircleOuterEnd: '#00ffb455',
        colorNeedleCircleInner: '#0a0a0f',
        colorNeedleCircleInnerEnd: '#0a0a0f',
        valueBox: false,
        colorMajorTicks: '#334',
        colorMinorTicks: '#222',
        colorNumbers: '#556',
        highlights: [
          { from: -2, to: -1, color: '#4488ff55' },
          { from: -1, to:  0, color: '#77ccff55' },
          { from:  0, to:  1, color: '#cc88ff55' },
          { from:  1, to:  2, color: '#ffbb4455' },
        ],
        highlightsWidth: 15,
        fontNumbers: 'Courier New',
        highDpiSupport: true,
        animationDuration: 450,
        animationRule: 'bounce',
        value: level,
      }).draw()
      // canvas-gauges sets inline width/height — reset so CSS can control the size
      el.style.width = '100%'
      el.style.height = 'auto'
    })
    return () => { alive = false; gauge = null }
  })

  $effect(() => { const v = level; if (gauge) gauge.value = v })

  // ── Fireworks ─────────────────────────────────────────────────────────────────
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
  <!-- Desktop: skip | gauge | banger  — Mobile: gauge on top, buttons below -->
  <button class="vbtn skip" class:active={ownVote === 'skip'}
    onclick={() => vote('skip')} disabled={!canVote}
    aria-label="Vote skip">
    <span class="btn-icon">👎</span>
    <span class="btn-lbl">skip</span>
    {#if cooldown > 0}<span class="btn-cd">{cooldown}s</span>{/if}
  </button>

  <div class="gauge-wrap">
    <canvas bind:this={canvasEl} class="gauge-canvas"></canvas>
    <div class="vm-lbl" style="color:{activeCol};text-shadow:0 0 8px {activeCol}55">
      {NAMES[activeIdx]}
    </div>
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
  /* Row: skip | gauge | banger — fills full card width */
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

  /* ── Gauge — grows to fill remaining space ── */
  .gauge-wrap {
    position: relative;
    flex: 1 1 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: stretch;
  }

  .gauge-canvas {
    display: block;
    width: 100% !important;
    height: auto !important;
  }

  /* label overlaid in the arc center */
  .vm-lbl {
    position: absolute;
    left: 50%;
    bottom: 28%;
    transform: translateX(-50%);
    white-space: nowrap;
    font-family: 'Courier New', monospace;
    font-size: 0.75rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    font-weight: 700;
    pointer-events: none;
    transition: color 0.3s, text-shadow 0.3s;
  }

  /* ── Buttons — tall cards flanking the gauge ── */
  .vbtn {
    flex: 0 0 72px;
    align-self: stretch;       /* fills the full row height */
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
  .btn-lbl  {
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

  /* Skip — left side */
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

  /* Banger — right side */
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

  /* Mobile: stack gauge above, buttons in a row below */
  @media (max-width: 380px) {
    .vm {
      flex-direction: column;
      padding: 0.4rem 0.6rem 0.5rem;
    }
    .gauge-wrap { max-width: 200px; width: 100%; }
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

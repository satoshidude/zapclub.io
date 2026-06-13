<script lang="ts">
  import { tweened } from 'svelte/motion'
  import { backOut } from 'svelte/easing'
  import { vibeMeter, sendMood, optimisticVote } from '../../nostr/mood.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import { sync } from '../../nostr/sync.svelte'
  import Fireworks from './Fireworks.svelte'

  let { clubId = '' }: { clubId?: string } = $props()

  const pos     = $derived(sync.live?.pos ?? -1)
  const bangers = $derived(pos >= 0 ? vibeMeter.bangerCount(clubId, pos) : 0)
  const skips   = $derived(pos >= 0 ? vibeMeter.skipCount(clubId, pos) : 0)
  const level   = $derived(Math.max(-2, Math.min(2, bangers - skips)))
  const activeIdx    = $derived(level + 2)
  // which arc segment is "lit": -2→0, -1→1, 0→none, +1→2, +2→3
  const activeSegIdx = $derived(level === 0 ? -1 : level < 0 ? level + 2 : level + 1)
  const ownVote      = $derived(pos >= 0 ? vibeMeter.ownVote(clubId, pos) : null)

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

  // Single-word labels — keep them short so SVG buttons fit alongside
  const NAMES      = ['skip', 'meh', 'groove', 'fire', 'banger']
  const LABEL_COLS = ['#6688cc', '#9988ff', '#cc77ff', '#ffaa22', '#ff5533']

  // ── SVG gauge geometry ──────────────────────────────────────────────────────
  // viewBox "-10 -5 220 165": y from -5 to 160, room for inline buttons below pivot
  const CX = 100, CY = 112
  const Ro = 85, Ri = 62
  const SPAN = 78   // degrees either side of top (156° total)

  // Button geometry (in SVG user units), vertically centred on mood label (y=144)
  const BTN_Y  = 137   // rect top
  const BTN_H  = 14    // rect height  → center at BTN_Y + BTN_H/2 = 144
  const BTN_W  = 46    // rect width
  const SKIP_X = 8     // left edge of skip rect
  const BNG_X  = 146   // left edge of banger rect

  // zapclub palette — dim (base) and neon (active) per segment
  const SEGS_DEF = [
    { from: -SPAN,      to: -SPAN / 2, dim: '#1a3258', neon: '#1a7fff' },
    { from: -SPAN / 2,  to: 0,         dim: '#251852', neon: '#7733ee' },
    { from: 0,          to:  SPAN / 2, dim: '#531490', neon: '#cc22ff' },
    { from:  SPAN / 2,  to:  SPAN,     dim: '#7a4800', neon: '#ff9900' },
  ]

  function ptArr(r: number, deg: number): [number, number] {
    const rad = deg * Math.PI / 180
    return [CX + r * Math.sin(rad), CY - r * Math.cos(rad)]
  }

  function arcPath(from: number, to: number, ro: number, ri: number, gap = 2.5): string {
    const t1 = from + gap, t2 = to - gap
    const [ox1, oy1] = ptArr(ro, t1), [ox2, oy2] = ptArr(ro, t2)
    const [ix2, iy2] = ptArr(ri, t2), [ix1, iy1] = ptArr(ri, t1)
    const large = Math.abs(t2 - t1) > 180 ? 1 : 0
    const f = (n: number) => n.toFixed(2)
    return `M ${f(ox1)} ${f(oy1)} A ${ro} ${ro} 0 ${large} 1 ${f(ox2)} ${f(oy2)} L ${f(ix2)} ${f(iy2)} A ${ri} ${ri} 0 ${large} 0 ${f(ix1)} ${f(iy1)} Z`
  }

  const TICKS = [
    { angle: -SPAN,     type: 'zzz'   },
    { angle: -SPAN / 2, type: 'meh'   },
    { angle:  0,        type: 'vinyl' },
    { angle:  SPAN / 2, type: 'wave'  },
    { angle:  SPAN,     type: 'bolt'  },
  ]

  // Needle points to segment centres, not boundaries.
  // Centres at ±1·SPAN/4 (inner segs) and ±3·SPAN/4 (outer segs); 0 = neutral.
  const NEEDLE_ANGLES = [-3, -1, 0, 1, 3].map(x => x * SPAN / 4)
  const needleAngle = $derived(NEEDLE_ANGLES[activeIdx])
  const needleTween = tweened(0, { duration: 480, easing: backOut })
  $effect(() => { needleTween.set(needleAngle) })

  const activeCol = $derived(LABEL_COLS[activeIdx])
  const labelName = $derived(NAMES[activeIdx].toUpperCase())

  // Hover state for SVG-native buttons (CSS :hover unreliable on SVG in Svelte scoped styles)
  let skipHover   = $state(false)
  let bangerHover = $state(false)

  // Button label text (no emoji prefix — icons drawn as SVG paths beside the text)
  const skipTxt   = $derived(cooldown > 0 ? `${cooldown}s` : skips   > 0 ? `${skips}`   : 'skip')
  const bangerTxt = $derived(cooldown > 0 ? `${cooldown}s` : bangers > 0 ? `${bangers}` : 'banger')

  // ── Fireworks ────────────────────────────────────────────────────────────────
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
  <div class="gauge-wrap">
    <svg viewBox="-10 -5 220 165" xmlns="http://www.w3.org/2000/svg" class="gauge-svg">
      <defs>
        <!-- Neon glow for active arc segment -->
        <filter id="vm-seg-glow" x="-18%" y="-18%" width="136%" height="136%">
          <feGaussianBlur in="SourceGraphic" stdDeviation="6" result="b1"/>
          <feGaussianBlur in="SourceGraphic" stdDeviation="2.5" result="b2"/>
          <feMerge><feMergeNode in="b1"/><feMergeNode in="b2"/><feMergeNode in="SourceGraphic"/></feMerge>
        </filter>
        <!-- userSpaceOnUse: absolute coords prevent zero-width-bbox collapse on vertical line -->
        <filter id="vm-needle-glow" x="-15" y="-90" width="30" height="115" filterUnits="userSpaceOnUse">
          <feGaussianBlur stdDeviation="3.5" result="blur"/>
          <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
        </filter>
        <filter id="vm-dot-glow" x="-120%" y="-120%" width="340%" height="340%">
          <feGaussianBlur stdDeviation="4" result="blur"/>
          <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
        </filter>
        <filter id="vm-txt-glow" x="-50%" y="-100%" width="200%" height="300%">
          <feGaussianBlur stdDeviation="2" result="blur"/>
          <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
        </filter>
        <filter id="vm-btn-glow" x="-20%" y="-50%" width="140%" height="200%">
          <feGaussianBlur stdDeviation="2" result="blur"/>
          <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
        </filter>
      </defs>

      <!-- Arc segments — active: neon + glow; others: dim -->
      {#each SEGS_DEF as seg, i}
        {@const active = i === activeSegIdx}
        <path
          d={arcPath(seg.from, seg.to, Ro, Ri)}
          fill={active ? seg.neon : seg.dim}
          opacity={active ? 1 : 0.5}
          filter={active ? 'url(#vm-seg-glow)' : undefined}
          style="transition: fill 0.35s ease, opacity 0.35s ease"
        />
      {/each}

      <!-- Tick marks: thicker purple line at center, thin grey at boundaries -->
      {#each TICKS as tick}
        {@const [x1, y1] = ptArr(Ri - 4, tick.angle)}
        {@const [x2, y2] = ptArr(Ro + 4, tick.angle)}
        <line
          x1={x1.toFixed(1)} y1={y1.toFixed(1)}
          x2={x2.toFixed(1)} y2={y2.toFixed(1)}
          stroke={tick.angle === 0 ? '#8855bb' : '#6a80a0'}
          stroke-width={tick.angle === 0 ? 2.5 : 1.5}
          opacity={tick.angle === 0 ? 0.85 : 0.6}
        />
      {/each}

      <!-- Tick icons outside arc — geometric, turntable-palette -->
      {#each TICKS as tick}
        {@const [lx, ly] = ptArr(Ro + 18, tick.angle)}
        <g transform="translate({lx.toFixed(1)},{ly.toFixed(1)})">
          {#if tick.type === 'vinyl'}
            <!-- Mini turntable record — same style as the header logo -->
            <circle r="6"   fill="#110822" stroke="#8e30eb" stroke-width="1.4"/>
            <circle r="4.2" fill="none"    stroke="#a855f7" stroke-width="0.6" opacity="0.5"/>
            <circle r="2.5" fill="none"    stroke="#a855f7" stroke-width="0.4" opacity="0.3"/>
            <circle r="1.8" fill="#22c55e"/>
            <circle r="0.7" fill="#110822"/>
          {:else if tick.type === 'zzz'}
            <!-- Three shrinking horizontal bars = zzz / no energy -->
            <line x1="-4"   y1="3"   x2="4"   y2="3"   stroke="#4a6699" stroke-width="1.3" stroke-linecap="round"/>
            <line x1="-3"   y1="0"   x2="3"   y2="0"   stroke="#4a6699" stroke-width="1.3" stroke-linecap="round"/>
            <line x1="-1.5" y1="-3"  x2="1.5" y2="-3"  stroke="#4a6699" stroke-width="1.3" stroke-linecap="round"/>
          {:else if tick.type === 'meh'}
            <!-- Two parallel lines = neutral / meh -->
            <line x1="-3.8" y1="-1.8" x2="3.8" y2="-1.8" stroke="#7060aa" stroke-width="1.3" stroke-linecap="round"/>
            <line x1="-3.8" y1=" 1.8" x2="3.8" y2=" 1.8" stroke="#7060aa" stroke-width="1.3" stroke-linecap="round"/>
          {:else if tick.type === 'wave'}
            <!-- Sine wave = in the groove -->
            <path d="M-5,0 Q-2.5,-4 0,0 Q2.5,4 5,0" fill="none" stroke="#9066cc" stroke-width="1.5" stroke-linecap="round"/>
          {:else if tick.type === 'bolt'}
            <!-- Lightning bolt = banger -->
            <path d="M1.5,-5.5 L-2,0 L2,0 L-1.5,5.5" fill="none" stroke="#cc8822" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>
          {/if}
        </g>
      {/each}

      <!-- Needle — tonearm: SVG translate+rotate -->
      <g transform="translate({CX} {CY}) rotate({$needleTween})">
        <line x1="0" y1="11" x2="0" y2="-74"
          stroke="#a050e8" stroke-width="5" stroke-linecap="round"
          filter="url(#vm-needle-glow)"
        />
        <circle cx="0" cy="-74" r="3.5" fill="#cc88ff"/>
      </g>

      <!-- Pivot — layered green dot -->
      <circle cx={CX} cy={CY} r="9"  fill="#00ff88" filter="url(#vm-dot-glow)" opacity="0.55"/>
      <circle cx={CX} cy={CY} r="8"  fill="#00cc66"/>
      <circle cx={CX} cy={CY} r="5"  fill="#08080e"/>
      <circle cx={CX} cy={CY} r="3"  fill="#00ff88"/>

      <!-- Mood label — centred between the two buttons -->
      <text
        x={CX} y={BTN_Y + BTN_H / 2}
        text-anchor="middle" dominant-baseline="middle"
        font-family="'Courier New', monospace"
        font-size="11" font-weight="700" letter-spacing="2.5"
        fill={activeCol} filter="url(#vm-txt-glow)"
      >{labelName}</text>

      <!-- ── Skip button (SVG-native, left of label) ── -->
      <g
        role="button" tabindex="0" aria-label="Vote skip"
        aria-disabled={!canVote}
        onclick={() => vote('skip')}
        onkeydown={(e) => e.key === 'Enter' && vote('skip')}
        onmouseenter={() => (skipHover = true)}
        onmouseleave={() => (skipHover = false)}
        opacity={!canVote ? 0.35 : 1}
        style="cursor: {canVote ? 'pointer' : 'default'}"
      >
        <rect
          x={SKIP_X} y={BTN_Y} width={BTN_W} height={BTN_H} rx="4"
          fill={ownVote === 'skip' ? '#0e2040' : (skipHover ? '#12243e' : '#0d1a30')}
          stroke={ownVote === 'skip' ? '#4477dd' : '#1e3050'}
          stroke-width="1.5"
          filter={ownVote === 'skip' ? 'url(#vm-btn-glow)' : undefined}
        />
        <!-- Skip icon: ✕ cross (reject), or mini timer during cooldown -->
        <g transform="translate({SKIP_X + 10},{BTN_Y + BTN_H / 2})" style="pointer-events:none">
          {#if cooldown > 0}
            <circle r="3.2" fill="none" stroke={ownVote === 'skip' ? '#88bbff' : '#5070a0'} stroke-width="1"/>
            <line x1="0" y1="-2.2" x2="0" y2="0" stroke={ownVote === 'skip' ? '#88bbff' : '#5070a0'} stroke-width="1" stroke-linecap="round"/>
          {:else}
            <line x1="-2.5" y1="-2.5" x2="2.5" y2="2.5" stroke={ownVote === 'skip' ? '#88bbff' : '#6090c0'} stroke-width="1.5" stroke-linecap="round"/>
            <line x1="2.5" y1="-2.5" x2="-2.5" y2="2.5" stroke={ownVote === 'skip' ? '#88bbff' : '#6090c0'} stroke-width="1.5" stroke-linecap="round"/>
          {/if}
        </g>
        <text
          x={SKIP_X + 20} y={BTN_Y + BTN_H / 2}
          text-anchor="start" dominant-baseline="middle"
          font-family="system-ui, sans-serif" font-size="9" font-weight="600"
          fill={ownVote === 'skip' ? '#88bbff' : '#6090c0'}
          style="pointer-events: none"
        >{skipTxt}</text>
      </g>

      <!-- ── Banger button (SVG-native, right of label) ── -->
      <g
        role="button" tabindex="0" aria-label="Vote banger"
        aria-disabled={!canVote}
        onclick={() => vote('banger')}
        onkeydown={(e) => e.key === 'Enter' && vote('banger')}
        onmouseenter={() => (bangerHover = true)}
        onmouseleave={() => (bangerHover = false)}
        opacity={!canVote ? 0.35 : 1}
        style="cursor: {canVote ? 'pointer' : 'default'}"
      >
        <rect
          x={BNG_X} y={BTN_Y} width={BTN_W} height={BTN_H} rx="4"
          fill={ownVote === 'banger' ? '#251808' : (bangerHover ? '#201408' : '#1a1005')}
          stroke={ownVote === 'banger' ? '#dd8800' : '#3a2808'}
          stroke-width="1.5"
          filter={ownVote === 'banger' ? 'url(#vm-btn-glow)' : undefined}
        />
        <!-- Banger icon: lightning bolt, or mini timer during cooldown -->
        <g transform="translate({BNG_X + 10},{BTN_Y + BTN_H / 2})" style="pointer-events:none">
          {#if cooldown > 0}
            <circle r="3.2" fill="none" stroke={ownVote === 'banger' ? '#ffcc44' : '#9a6020'} stroke-width="1"/>
            <line x1="0" y1="-2.2" x2="0" y2="0" stroke={ownVote === 'banger' ? '#ffcc44' : '#9a6020'} stroke-width="1" stroke-linecap="round"/>
          {:else}
            <path d="M1.2,-3.5 L-1.5,0 L1.5,0 L-1.2,3.5"
              fill="none"
              stroke={ownVote === 'banger' ? '#ffcc44' : '#cc8822'}
              stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
          {/if}
        </g>
        <text
          x={BNG_X + 20} y={BTN_Y + BTN_H / 2}
          text-anchor="start" dominant-baseline="middle"
          font-family="system-ui, sans-serif" font-size="9" font-weight="600"
          fill={ownVote === 'banger' ? '#ffcc44' : '#c08030'}
          style="pointer-events: none"
        >{bangerTxt}</text>
      </g>
    </svg>
  </div>
</div>

<style>
  .vm {
    background: #08080e;
    border-radius: 12px;
    border: 1px solid #1a1a2e;
    width: 100%;
    box-sizing: border-box;
    padding: 0.4rem;
  }

  .gauge-wrap {
    width: 100%;
  }

  .gauge-svg {
    display: block;
    width: 100%;
    height: auto;
  }
</style>

<script lang="ts">
  import { tweened } from 'svelte/motion'
  import { backOut } from 'svelte/easing'
  import { vibeMeter, sendMood, optimisticVote, BANGER_MAX, SKIP_THRESHOLD } from '../../nostr/mood.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import { sync } from '../../nostr/sync.svelte'
  import Fireworks from './Fireworks.svelte'

  let { clubId = '', isMember = false }: { clubId?: string; isMember?: boolean } = $props()

  const pos     = $derived(sync.live?.pos ?? -1)
  const bangers = $derived(pos >= 0 ? vibeMeter.bangerCount(clubId, pos) : 0)
  const skips   = $derived(pos >= 0 ? vibeMeter.skipCount(clubId, pos) : 0)
  const level   = $derived(Math.max(-2, Math.min(2, bangers - skips)))
  const activeIdx    = $derived(level + 2)
  const ownVote      = $derived(pos >= 0 ? vibeMeter.ownVote(clubId, pos) : null)
  let sending = $state(false)

  const cooldownSeconds = $derived(pos >= 0 ? vibeMeter.cooldownSeconds() : 0)
  const canVote   = $derived(auth.canSign && isMember && pos >= 0 && !!sync.live && cooldownSeconds === 0 && !sending)
  const canSkip   = $derived(canVote && skips < SKIP_THRESHOLD)
  const canBanger = $derived(canVote && bangers < BANGER_MAX)

  // Single-word labels — keep them short so SVG buttons fit alongside
  const NAMES = ['skip', 'meh', 'groove', 'fire', 'banger']

  // ── SVG gauge geometry ──────────────────────────────────────────────────────
  // viewBox "-10 -5 220 165": y from -5 to 160, room for inline buttons below pivot
  const CX = 100, CY = 112
  const Ro = 85, Ri = 62
  const SPAN = 78   // degrees either side of top (156° total)

  // Fine LCD blocks follow a classic VU progression: green operating range,
  // yellow high-energy range, red peak range.
  const LED_COUNT = 17
  const LED_STEP = SPAN * 2 / LED_COUNT
  const METER_LEDS = Array.from({ length: LED_COUNT }, (_, i) => ({
    from: -SPAN + i * LED_STEP,
    to: -SPAN + (i + 1) * LED_STEP,
  }))
  const METER_PALETTES = {
    green: ['#75ef9d', '#36a765', '#17482f'],
    yellow: ['#ffe16a', '#c4a632', '#554815'],
    red: ['#ff7478', '#c84a50', '#572126'],
  } as const

  function meterFill(index: number, distance: number): string {
    const palette = index <= 8
      ? METER_PALETTES.green
      : index <= 12
        ? METER_PALETTES.yellow
        : METER_PALETTES.red
    return palette[distance === 0 ? 0 : distance === 1 ? 1 : 2]
  }

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

  // Needle points to segment centres, not boundaries.
  // Centres at ±1·SPAN/4 (inner segs) and ±3·SPAN/4 (outer segs); 0 = neutral.
  const NEEDLE_ANGLES = [-3, -1, 0, 1, 3].map(x => x * SPAN / 4)
  const needleAngle = $derived(NEEDLE_ANGLES[activeIdx])
  const activeLedIdx = $derived(Math.round(
    ((needleAngle + SPAN) / (SPAN * 2)) * (LED_COUNT - 1),
  ))
  const needleTween = tweened(0, { duration: 480, easing: backOut })
  $effect(() => { needleTween.set(needleAngle) })

  const labelName = $derived(NAMES[activeIdx].toUpperCase())

  const skipTxt   = $derived(skips > 0 ? `SKIP ${skips}/${SKIP_THRESHOLD}` : 'SKIP')
  const bangerTxt = $derived(bangers > 0 ? `BANGER ${bangers}/${BANGER_MAX}` : 'BANGER')
  const readyTxt = $derived(
    !auth.canSign ? 'SIGN IN TO VOTE'
        : !isMember ? 'JOIN TO VOTE'
          : sending ? 'SENDING…'
          : cooldownSeconds > 0 ? `NEXT VOTE IN ${cooldownSeconds}s` : 'RATE THE DJ',
  )

  // ── Fireworks (unified: brief on each banger vote, longer on threshold) ──────
  let fireworks = $state(false)
  let fwTimer: ReturnType<typeof setTimeout> | null = null
  function showFireworks(ms: number) {
    fireworks = true
    if (fwTimer) clearTimeout(fwTimer)
    fwTimer = setTimeout(() => { fireworks = false }, ms)
  }
  $effect(() => {
    if (pos < 0) return
    void bangers
    if (vibeMeter.checkBanger(clubId, pos)) showFireworks(2800)
  })

  // ── Needle shake ─────────────────────────────────────────────────────────────
  let shakeOffset = $state(0)
  async function triggerShake() {
    const steps = [6, -6, 4, -4, 2, -1, 0]
    for (const a of steps) {
      shakeOffset = a
      await new Promise<void>((r) => setTimeout(r, 55))
    }
    shakeOffset = 0
  }

  // ── Reaction ──────────────────────────────────────────────────────────────────
  async function vote(v: 'banger' | 'skip') {
    if (!canVote || !auth.pubkey) return
    if (v === 'skip' && !canSkip) return
    if (v === 'banger' && !canBanger) return
    sending = true
    try {
      await sendMood(clubId, pos, v)
      optimisticVote(clubId, pos, auth.pubkey, v)
      if (v === 'banger') {
        showFireworks(1000)
        void triggerShake()
      }
    } catch (e) {
      console.warn('[vibe] reaction failed:', e)
    } finally {
      sending = false
    }
  }
</script>

<Fireworks show={fireworks} />

<div class="vm led-zone">
  <div class="scanlines" aria-hidden="true"></div>
  <div class="vm-head lcd-card-heading"><span class="vm-title lcd-card-title">Vibe Meter</span></div>

  <div class="gauge-wrap">
    <svg viewBox="-10 -5 220 162" xmlns="http://www.w3.org/2000/svg" class="gauge-svg">
      <!-- Fine LED arc — the selected position and its neighbours are brightest. -->
      {#each METER_LEDS as seg, i}
        {@const distance = Math.abs(i - activeLedIdx)}
        <path
          d={arcPath(seg.from, seg.to, Ro, Ri, 1.1)}
          fill={meterFill(i, distance)}
          opacity={distance === 0 ? 1 : distance === 1 ? 0.82 : 0.48}
          style="transition: fill 0.35s ease, opacity 0.35s ease"
        />
      {/each}

      <!-- Needle — tonearm: SVG translate+rotate; shakeOffset adds brief jitter after a vote -->
      <g transform="translate({CX} {CY}) rotate({$needleTween + shakeOffset})">
        <line x1="0" y1="11" x2="0" y2="-74"
          stroke="#f1f3f4" stroke-width="4.5" stroke-linecap="round"
        />
        <circle cx="0" cy="-74" r="3.5" fill="#ffffff"/>
      </g>

      <!-- Pivot — layered LCD dot -->
      <circle cx={CX} cy={CY} r="9"  fill="#f1f3f4" opacity="0.34"/>
      <circle cx={CX} cy={CY} r="8"  fill="#a5adb3"/>
      <circle cx={CX} cy={CY} r="5"  fill="#171a1d"/>
      <circle cx={CX} cy={CY} r="3"  fill="#ffffff"/>

      <!-- The current mood owns the visual centre, as in the reference panel. -->
      <text
        x={CX} y="145"
        text-anchor="middle" dominant-baseline="middle"
        font-family="'DotGothic16', ui-monospace, monospace"
        font-size="21" font-weight="700" letter-spacing="1.6"
        fill="#f1f3f4"
      >{labelName}</text>

    </svg>

    <div class="meter-actions">
      <button class="meter-action skip" class:active={ownVote === 'skip'} onclick={() => vote('skip')} disabled={!canSkip} aria-label="Vote skip">
        <svg class="action-icon" viewBox="0 0 32 32" aria-hidden="true">
          <path d="M3 7l9 9-9 9V7zm11 0 9 9-9 9V7z" fill="currentColor"></path>
          <path d="M27 7v18" fill="none" stroke="currentColor" stroke-width="2.8" stroke-linecap="round"></path>
        </svg>
        <span>{skipTxt}</span>
      </button>

      <span class="action-divider" aria-hidden="true"></span>

      <button class="meter-action banger" class:active={ownVote === 'banger'} onclick={() => vote('banger')} disabled={!canBanger} aria-label="Vote banger">
        <svg class="action-icon" viewBox="0 0 32 32" aria-hidden="true">
          <path d="M16 2.5l3 6.3 6.7-2.5-2.5 6.6 6.3 3.1-6.3 3.1 2.5 6.6-6.7-2.5-3 6.3-3-6.3-6.7 2.5 2.5-6.6L2.5 16l6.3-3.1-2.5-6.6L13 8.8 16 2.5z" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"></path>
          <path d="m17.5 9.5-5 7h4L14.5 23l5-7h-4l2-6.5z" fill="currentColor"></path>
        </svg>
        <span>{bangerTxt}</span>
      </button>
    </div>
    <div class="vote-state" class:rate-ready={readyTxt === 'RATE THE DJ'} class:cooling={cooldownSeconds > 0}>{readyTxt}</div>
  </div>
</div>

<style>
  .vm {
    position: relative;
    overflow: hidden;
    background: transparent;
    border-radius: 0;
    border: 0;
    width: 100%;
    height: 100%;
    box-sizing: border-box;
    padding: 0.55rem 0.75rem 0.65rem;
    color: var(--lcd-text);
    box-shadow: none;
    font-family: 'DotGothic16', ui-monospace, monospace;
    text-shadow: none;
  }
  .vm > :not(.scanlines) {
    position: relative;
    z-index: 1;
  }
  .scanlines {
    position: absolute;
    inset: 0;
    z-index: 0;
    display: none;
    pointer-events: none;
  }
  .vm-head {
    padding-top: 0.3rem;
    padding-inline: 0.4rem;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .vm-title {
    display: inline-block;
  }
  .gauge-wrap {
    width: 100%;
  }

  .gauge-svg {
    display: block;
    width: 100%;
    height: 138px;
  }
  .meter-actions {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 1px minmax(0, 1fr);
    align-items: center;
    gap: 0.45rem;
    margin-top: -0.25rem;
    padding: 0 0.35rem 0.35rem;
  }
  .meter-action {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.55rem;
    min-width: 0;
    min-height: 42px;
    padding: 0 0.3rem;
    border: 0;
    color: var(--lcd-text-soft);
    background: transparent;
    font-family: inherit;
    font-size: clamp(0.88rem, 1.55vw, 1rem);
    font-weight: 600;
    letter-spacing: 0.08em;
    white-space: nowrap;
    cursor: pointer;
  }
  .meter-action:hover:not(:disabled),
  .meter-action:focus-visible,
  .meter-action.active {
    color: var(--lcd-text-bright);
  }
  .meter-action:focus-visible {
    outline: 1px dashed var(--lcd-text);
    outline-offset: 2px;
  }
  .meter-action:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .action-icon {
    flex: 0 0 32px;
    width: 32px;
    height: 32px;
  }
  .action-divider {
    width: 1px;
    height: 24px;
    background: rgba(241, 243, 244, 0.2);
  }
  .vote-state {
    min-height: 1rem;
    margin-top: -0.15rem;
    text-align: center;
    color: var(--lcd-text-soft);
    font-size: 0.62rem;
    letter-spacing: 0.15em;
  }
  .vote-state.cooling {
    color: var(--lcd-text);
  }
  .vote-state.rate-ready {
    font-size: calc(0.62rem + 3px);
  }
  @media (max-width: 760px) {
    .gauge-wrap {
      max-width: 360px;
      margin: 0 auto;
    }
    .gauge-svg {
      height: 148px;
    }
    .meter-actions {
      margin-top: -0.4rem;
    }
  }
</style>

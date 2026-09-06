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
  const ownTrack     = $derived(!!auth.pubkey && sync.live?.dj === auth.pubkey)
  const voteStateId  = $derived(`vibe-vote-state-${clubId}`)
  let sending = $state(false)

  const cooldownSeconds = $derived(pos >= 0 ? vibeMeter.cooldownSeconds() : 0)
  const canVote   = $derived(auth.canSign && isMember && pos >= 0 && !!sync.live && !ownTrack && cooldownSeconds === 0 && !sending)
  const canSkip   = $derived(canVote && skips < SKIP_THRESHOLD)
  const canBanger = $derived(canVote && bangers < BANGER_MAX)

  // Shared mood labels for the gauge.
  const NAMES = ['skip', 'meh', 'groove', 'fire', 'banger']

  // ── SVG gauge geometry ──────────────────────────────────────────────────────
  // The viewBox includes the mood label below the pivot.
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

  const readyTxt = $derived(
    !sync.live || pos < 0 ? 'Waiting for a track'
      : ownTrack ? 'Your track — no vote'
        : !auth.canSign ? 'Sign in to vote'
          : !isMember ? 'Join to vote'
          : sending ? 'Sending…'
          : cooldownSeconds > 0 ? `Next vote in ${cooldownSeconds}s`
          : ownVote ? `${ownVote === 'banger' ? 'Banger' : 'Skip'} · Your vote`
          : 'Rate the DJ',
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
    const voteClub = clubId
    const votePos = pos
    const voter = auth.pubkey
    sending = true
    try {
      await sendMood(voteClub, votePos, v)
      optimisticVote(voteClub, votePos, voter, v)
      if (v === 'banger' && clubId === voteClub && sync.live?.pos === votePos) {
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
        class="meter-label"
        x={CX} y="145"
        text-anchor="middle" dominant-baseline="middle"
        font-size="21" font-weight="400" letter-spacing="0.08em"
      >{labelName}</text>

    </svg>

    <div class="meter-actions">
      <button type="button" class="meter-action skip" class:active={ownVote === 'skip'} onclick={() => vote('skip')} disabled={!canSkip} aria-pressed={ownVote === 'skip'} aria-label="Vote skip" aria-describedby={voteStateId}>
        <span class="action-label">
          <svg class="action-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <polygon points="5 4 15 12 5 20 5 4"></polygon>
            <line x1="19" y1="4" x2="19" y2="20"></line>
          </svg>
          <span>Skip</span>
        </span>
        <span class="action-count">{skips}/{SKIP_THRESHOLD}</span>
      </button>

      <button type="button" class="meter-action banger" class:active={ownVote === 'banger'} onclick={() => vote('banger')} disabled={!canBanger} aria-pressed={ownVote === 'banger'} aria-label="Vote banger" aria-describedby={voteStateId}>
        <span class="action-label">
          <svg class="action-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M8.5 14.5A6 6 0 0 1 9 7a8 8 0 0 0 4-5 8 8 0 0 1 4 10 4 4 0 0 0 1-5 9 9 0 0 1 2 6 8 8 0 1 1-16 0 6 6 0 0 1 1-3 6 6 0 0 0 3.5 4.5Z"></path>
          </svg>
          <span>Banger</span>
        </span>
        <span class="action-count">{bangers}/{BANGER_MAX}</span>
      </button>
    </div>
    <div
      id={voteStateId}
      class="vote-state"
      aria-live={cooldownSeconds > 0 ? 'off' : 'polite'}
      aria-atomic="true"
    >{readyTxt}</div>
  </div>
</div>

<style>
  .vm {
    container-type: inline-size;
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
  .meter-label {
    fill: var(--lcd-text);
    text-shadow: var(--lcd-text-shadow);
    filter: drop-shadow(0 0 2px rgba(90, 160, 255, 0.42));
    font-family: var(--font-headline);
    -webkit-text-stroke: 0.25px currentColor;
  }
  .meter-actions {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
    margin-top: 10px;
    padding: 0 0.35rem;
  }
  .meter-action {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    min-width: 0;
    min-height: 46px;
    padding: 0 12px;
    border: 0;
    border-radius: 0;
    color: var(--lcd-text);
    background: rgba(9, 11, 12, 0.8);
    box-shadow: inset 0 -2px rgba(183, 188, 196, 0.5);
    font-family: var(--font);
    white-space: nowrap;
    cursor: pointer;
  }
  .meter-action:hover:not(:disabled) {
    background: rgba(241, 243, 244, 0.08);
  }
  .meter-action.active {
    box-shadow: inset 0 -2px var(--accent);
    background: color-mix(in srgb, var(--accent) 8%, #17191b);
  }
  .meter-action.active .action-label {
    color: var(--accent);
  }
  .meter-action:focus-visible {
    outline: 1px dashed var(--lcd-text);
    outline-offset: 2px;
  }
  .meter-action:disabled {
    cursor: default;
  }
  .meter-action:disabled:not(.active) {
    opacity: 0.5;
  }
  .action-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-family: var(--font-headline);
    font-size: 18px;
    font-weight: 400;
    line-height: 1.15;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    -webkit-text-stroke: 0.25px currentColor;
  }
  .action-icon {
    flex: 0 0 17px;
    width: 17px;
    height: 17px;
  }
  .action-count {
    color: var(--lcd-text-soft);
    font-size: 11px;
    font-weight: 400;
    font-variant-numeric: tabular-nums;
  }
  .vote-state {
    min-height: 20px;
    margin-top: 12px;
    text-align: center;
    color: var(--lcd-text-dim);
    font-family: var(--font);
    font-size: 14px;
    font-weight: 400;
    line-height: 1.4;
    font-variant-numeric: tabular-nums;
    text-shadow: var(--lcd-copy-shadow);
  }
  @container (max-width: 270px) {
    .meter-action {
      padding-inline: 8px;
      gap: 4px;
    }
    .action-label {
      gap: 5px;
      letter-spacing: 0.04em;
    }
  }
  @media (max-width: 760px) {
    .gauge-wrap {
      max-width: 360px;
      margin: 0 auto;
    }
    .gauge-svg {
      height: 148px;
    }
  }
</style>

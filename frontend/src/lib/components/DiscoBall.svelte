<script lang="ts">
  // Animated disco ball: a sphere of mirror facets that shimmer, plus twinkling
  // sparkles and a gentle bob. Used as the club placeholder image.
  let { size = 44 }: { size?: number } = $props()

  // Build a grid of facet tiles clipped to the ball circle.
  const CX = 18
  const CY = 19
  const R = 12
  const T = 3 // tile size
  const palette = ['#8e30eb', '#a855f7', '#c084fc', '#6b21a8', '#d8b4fe', '#7c3aed']
  type Tile = { x: number; y: number; fill: string; delay: number }
  const tiles: Tile[] = []
  for (let gy = CY - R; gy < CY + R; gy += T) {
    for (let gx = CX - R; gx < CX + R; gx += T) {
      const mx = gx + T / 2
      const my = gy + T / 2
      if ((mx - CX) ** 2 + (my - CY) ** 2 <= (R - 0.4) ** 2) {
        tiles.push({
          x: gx,
          y: gy,
          fill: palette[(Math.abs(gx + gy) / 1) % palette.length | 0],
          // Stagger the shimmer by position for a rolling glint.
          delay: (((gx - gy) % 9) + 9) % 9 * 0.18,
        })
      }
    }
  }
</script>

<svg class="disco" viewBox="0 0 36 36" width={size} height={size} role="img" aria-label="disco ball">
  <defs>
    <radialGradient id="sphere" cx="38%" cy="32%" r="75%">
      <stop offset="0%" stop-color="#efe1ff" stop-opacity="0.9" />
      <stop offset="45%" stop-color="#8e30eb" stop-opacity="0.25" />
      <stop offset="100%" stop-color="#1b0b33" stop-opacity="0.9" />
    </radialGradient>
    <clipPath id="ballclip">
      <circle cx={CX} cy={CY} r={R} />
    </clipPath>
  </defs>

  <!-- hanger -->
  <line x1={CX} y1="2" x2={CX} y2={CY - R} stroke="#6b21a8" stroke-width="1" />
  <circle cx={CX} cy="2.5" r="1.2" fill="#c084fc" />

  <g class="ball">
    <circle cx={CX} cy={CY} r={R} fill="#13072a" />
    <g clip-path="url(#ballclip)">
      {#each tiles as tile (tile.x + ',' + tile.y)}
        <rect
          class="facet"
          x={tile.x + 0.25}
          y={tile.y + 0.25}
          width={T - 0.6}
          height={T - 0.6}
          rx="0.4"
          fill={tile.fill}
          style="animation-delay: {tile.delay}s"
        />
      {/each}
    </g>
    <!-- glossy highlight -->
    <circle cx={CX} cy={CY} r={R} fill="url(#sphere)" />
  </g>

  <!-- sparkles -->
  <g class="spark s1" fill="#ffffff">
    <path d="M30 9 l0.6 1.6 1.6 0.6 -1.6 0.6 -0.6 1.6 -0.6 -1.6 -1.6 -0.6 1.6 -0.6 Z" />
  </g>
  <g class="spark s2" fill="#e9d5ff">
    <path d="M7 24 l0.5 1.3 1.3 0.5 -1.3 0.5 -0.5 1.3 -0.5 -1.3 -1.3 -0.5 1.3 -0.5 Z" />
  </g>
</svg>

<style>
  .disco {
    display: block;
    flex: 0 0 auto;
    filter: drop-shadow(0 0 6px rgba(142, 48, 235, 0.5));
  }
  .ball {
    transform-origin: 18px 19px;
    animation: bob 3.4s ease-in-out infinite;
  }
  @keyframes bob {
    0%,
    100% {
      transform: translateY(0) scale(1);
    }
    50% {
      transform: translateY(-0.6px) scale(1.015);
    }
  }
  .facet {
    animation: shimmer 1.8s ease-in-out infinite;
  }
  @keyframes shimmer {
    0%,
    100% {
      opacity: 0.45;
    }
    50% {
      opacity: 1;
    }
  }
  .spark {
    transform-origin: center;
    animation: twinkle 2.2s ease-in-out infinite;
  }
  .spark.s2 {
    animation-delay: 1.1s;
  }
  @keyframes twinkle {
    0%,
    100% {
      opacity: 0;
      transform: scale(0.5);
    }
    50% {
      opacity: 1;
      transform: scale(1);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .ball,
    .facet,
    .spark {
      animation: none;
    }
    .spark {
      opacity: 0.7;
    }
  }
</style>

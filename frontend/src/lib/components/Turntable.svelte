<script lang="ts">
  // Spinning vinyl turntable in Nostr purple. Reused as the header logo and as the
  // club placeholder image. The record spins; the tonearm stays put.
  let { size = 32, glow = true }: { size?: number; glow?: boolean } = $props()
</script>

<svg
  class="turntable"
  class:glow
  viewBox="0 0 36 36"
  width={size}
  height={size}
  role="img"
  aria-label="turntable"
>
  <g class="vinyl">
    <circle cx="16" cy="20" r="13" fill="#1b0b33" stroke="#8e30eb" stroke-width="1.6" />
    <circle cx="16" cy="20" r="9.5" fill="none" stroke="#a855f7" stroke-width="0.5" opacity="0.4" />
    <circle cx="16" cy="20" r="6.5" fill="none" stroke="#a855f7" stroke-width="0.5" opacity="0.3" />
    <circle cx="16" cy="20" r="3.6" fill="#22c55e" />
    <circle cx="16" cy="11.5" r="1.1" fill="#d8b4fe" />
    <circle cx="16" cy="20" r="1" fill="#1b0b33" />
  </g>
  <line x1="29" y1="7" x2="20.5" y2="15.5" stroke="#c084fc" stroke-width="1.7" stroke-linecap="round" />
  <circle cx="29" cy="7" r="1.9" fill="#c084fc" />
</svg>

<style>
  .turntable {
    display: block;
    flex: 0 0 auto;
  }
  .turntable.glow {
    filter: drop-shadow(0 0 6px rgba(142, 48, 235, 0.55));
  }
  .vinyl {
    transform-origin: 16px 20px;
    animation: vinyl-spin 2.4s linear infinite;
  }
  @keyframes vinyl-spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .vinyl {
      animation: none;
    }
  }
</style>

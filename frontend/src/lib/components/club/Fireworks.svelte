<script lang="ts">
  let { show = false }: { show: boolean } = $props()

  // 24 particles at evenly-spaced angles, varied distance and color.
  const COLORS = ['#f59e0b', '#ef4444', '#a855f7', '#22c55e', '#3b82f6', '#ec4899', '#fbbf24', '#f97316']
  const particles = Array.from({ length: 24 }, (_, i) => ({
    angle: i * 15,
    dist: 48 + (i * 17) % 40,
    color: COLORS[i % COLORS.length],
    delay: (i * 35) % 180,
    size: 5 + (i * 3) % 5,
  }))
</script>

{#if show}
  <div class="fw-overlay" aria-hidden="true">
    <div class="fw-center">
      {#each particles as p}
        <span
          class="spark"
          style="
            --tx: {Math.cos((p.angle * Math.PI) / 180) * p.dist}px;
            --ty: {Math.sin((p.angle * Math.PI) / 180) * p.dist}px;
            --color: {p.color};
            --delay: {p.delay}ms;
            --size: {p.size}px;
          "
        ></span>
      {/each}
      <span class="burst">🔥</span>
    </div>
  </div>
{/if}

<style>
  .fw-overlay {
    position: fixed;
    inset: 0;
    z-index: 300;
    pointer-events: none;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .fw-center {
    position: relative;
    width: 0;
    height: 0;
  }
  .spark {
    position: absolute;
    width: var(--size);
    height: var(--size);
    border-radius: 50%;
    background: var(--color);
    top: 0;
    left: 0;
    animation: spark 900ms ease-out forwards;
    animation-delay: var(--delay);
    opacity: 0;
    box-shadow: 0 0 4px var(--color);
  }
  @keyframes spark {
    0%   { transform: translate(0, 0) scale(1.2); opacity: 1; }
    60%  { opacity: 1; }
    100% { transform: translate(var(--tx), var(--ty)) scale(0); opacity: 0; }
  }
  .burst {
    position: absolute;
    font-size: 2.5rem;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    animation: pop 600ms ease-out forwards;
    line-height: 1;
  }
  @keyframes pop {
    0%   { transform: translate(-50%, -50%) scale(0.5); opacity: 0; }
    30%  { transform: translate(-50%, -50%) scale(1.4); opacity: 1; }
    70%  { transform: translate(-50%, -50%) scale(1.1); opacity: 1; }
    100% { transform: translate(-50%, -50%) scale(1.6); opacity: 0; }
  }
</style>

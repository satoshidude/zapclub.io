<script lang="ts">
  import { onMount } from 'svelte'

  type LedTheme = 'red' | 'green' | 'blue' | 'black'

  const STORAGE_KEY = 'zapclub-led-theme'
  const themes: { id: LedTheme; label: string; colors: [string, string, string]; dot: string }[] = [
    { id: 'red', label: 'Red', colors: ['#4c101a', '#22070d', '#080204'], dot: '#e72e48' },
    { id: 'green', label: 'Green', colors: ['#0c3a1d', '#061b0e', '#020804'], dot: '#2fc85c' },
    { id: 'blue', label: 'Blue', colors: ['#0d1f42', '#091c3c', '#030a18'], dot: '#397fe4' },
    { id: 'black', label: 'Black', colors: ['#1a1c1e', '#0c0f11', '#020303'], dot: '#34383c' },
  ]

  let active = $state<LedTheme>('black')

  function isTheme(value: string | null): value is LedTheme {
    return themes.some((theme) => theme.id === value)
  }

  function applyTheme(theme: LedTheme, persist = true) {
    active = theme
    document.documentElement.dataset.zapLedTheme = theme
    if (persist) localStorage.setItem(STORAGE_KEY, theme)
  }

  onMount(() => {
    const saved = localStorage.getItem(STORAGE_KEY)
    applyTheme(isTheme(saved) ? saved : 'black', false)
  })
</script>

<div class="led-themes" role="group" aria-label="Card LED color">
  {#each themes as theme (theme.id)}
    <button
      type="button"
      class:active={active === theme.id}
      class:is-black={theme.id === 'black'}
      style={`--led-a: ${theme.colors[0]}; --led-b: ${theme.colors[1]}; --led-c: ${theme.colors[2]}; --led-dot: ${theme.dot}`}
      aria-label={`${theme.label} card LED`}
      aria-pressed={active === theme.id}
      title={`${theme.label} card LED`}
      onclick={() => applyTheme(theme.id)}
    ><span aria-hidden="true"></span></button>
  {/each}
</div>

<style>
  .led-themes {
    display: flex;
    align-items: center;
    gap: 4px;
    flex: 0 0 auto;
  }
  button {
    display: grid;
    width: 28px;
    height: 32px;
    padding: 0;
    place-items: center;
    border: 0;
    color: var(--lcd-text, var(--text));
    background: transparent;
    cursor: pointer;
  }
  span {
    display: block;
    width: 16px;
    height: 16px;
    border: 1px solid color-mix(in srgb, var(--led-dot) 58%, #020303 42%);
    border-radius: 50%;
    background:
      repeating-linear-gradient(180deg, rgba(255, 255, 255, 0.06) 0 1px, transparent 1px 3px),
      radial-gradient(circle at 38% 32%, var(--led-dot) 0 20%, var(--led-a) 58%, var(--led-c) 100%);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.1),
      inset 0 -2px 3px rgba(0, 0, 0, 0.7);
    opacity: 0.72;
    transform: scale(1);
    transition: opacity 120ms ease, transform 120ms ease, filter 120ms ease;
  }
  button.is-black span {
    border-color: #24282b;
  }
  button.active span {
    opacity: 1;
    transform: scale(1.18);
    filter: saturate(1.3) brightness(1.16);
  }
  button:hover span {
    opacity: 0.9;
    transform: scale(1.08);
  }
  button.active:hover span {
    opacity: 1;
    transform: scale(1.18);
  }
  button:focus-visible {
    outline: 1px dashed var(--accent);
    outline-offset: -2px;
  }
  @media (max-width: 560px) {
    .led-themes { gap: 2px; }
    button { width: 24px; }
    span { width: 15px; height: 15px; }
  }
</style>

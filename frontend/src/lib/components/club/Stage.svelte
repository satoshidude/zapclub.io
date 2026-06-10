<script lang="ts">
  import { stage, MAX_DJS } from '../../nostr/stage.svelte'

  interface Props {
    /** The now-playing card — rendered inside the stage card (the track plays on the stage). */
    children?: import('svelte').Snippet
    /** Footer content (the DJ's Live Set) — rendered inside the stage card, below. */
    footer?: import('svelte').Snippet
  }
  let { children, footer }: Props = $props()

  // The people (slots, join/leave, kick) live on the Dancefloor's stage row — this card is
  // the decks: what plays + the live set.
  const djs = $derived(stage.djs)
</script>

<div class="stage card">
  <div class="head">
    <h3>Stage <span class="count">{djs.length}/{MAX_DJS}</span></h3>
  </div>

  {#if children}<div class="now-wrap">{@render children()}</div>{/if}

  {#if footer}<div class="set-wrap">{@render footer()}</div>{/if}
</div>

<style>
  .stage {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.9rem 1rem;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.8rem;
  }
  h3 {
    margin: 0;
    font-size: 1rem;
  }
  .count {
    color: var(--text-dim);
    font-weight: 600;
    font-size: 0.85rem;
  }
  .now-wrap {
    margin-bottom: 0.9rem;
  }
  .set-wrap {
    margin-top: 0.9rem;
  }
</style>

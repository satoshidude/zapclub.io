<script lang="ts">
  import { upcomingTracks } from '../../nostr/sync.svelte'
  import { stage } from '../../nostr/stage.svelte'
  import { queues } from '../../nostr/queue.svelte'
  import { useProfile, displayName } from '../../nostr/profiles.svelte'

  // Recompute when stage or queues change (touch them so the deriveds track).
  const next = $derived.by(() => {
    void stage.djs
    void queues
    return upcomingTracks(5)
  })
</script>

{#if next.length > 0}
  <div class="cn card">
    <h3>Coming up</h3>
    <ol>
      {#each next as item, i (item.videoId + i)}
        {@const profile = useProfile(item.dj)}
        <li>
          <span class="idx">{i + 1}</span>
          <span class="title">{item.title}</span>
          <span class="dj">{displayName(item.dj, profile)}</span>
        </li>
      {/each}
    </ol>
  </div>
{/if}

<style>
  .cn {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.9rem 1rem;
  }
  h3 {
    margin: 0 0 0.6rem;
    font-size: 0.95rem;
  }
  ol {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  li {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    font-size: 0.85rem;
  }
  .idx {
    flex: 0 0 1.3rem;
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
  }
  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dj {
    flex: 0 0 auto;
    color: var(--text-dim);
    font-size: 0.76rem;
    max-width: 10ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>

<script lang="ts">
  import { upcomingTracks, sync } from '../../nostr/sync.svelte'
  import { stage } from '../../nostr/stage.svelte'
  import { queues } from '../../nostr/queue.svelte'
  import { autodj } from '../../nostr/autodj.svelte'
  import { useProfile, displayName } from '../../nostr/profiles.svelte'

  let { clubId = '' }: { clubId?: string } = $props()

  // Recompute when the running track, the stage, any DJ's queue, or the auto-DJ config changes.
  const next = $derived.by(() => {
    void sync.nowPlaying
    void autodj.getConfig(clubId)
    const djs = stage.djs
    for (const d of djs) void queues.get(d.pubkey)?.updatedAt
    return upcomingTracks(clubId, 6)
  })
  const firstProfile = $derived(next[0] ? useProfile(next[0].dj) : null)
  const rest = $derived(next.slice(1))
</script>

{#if next.length > 0}
  <details class="cn">
    <summary>
      <span class="cn-label">Up next</span>
      <span class="cn-title">{next[0].title}</span>
      <span class="cn-dj">{displayName(next[0].dj, firstProfile)}</span>
      {#if rest.length > 0}<span class="chevron" aria-hidden="true">▾</span>{/if}
    </summary>
    {#if rest.length > 0}
      <ol>
        {#each rest as item, i (item.videoId + i)}
          {@const profile = useProfile(item.dj)}
          <li>
            <span class="idx">{i + 2}</span>
            <span class="title">{item.title}</span>
            <span class="dj">{displayName(item.dj, profile)}</span>
          </li>
        {/each}
      </ol>
    {/if}
  </details>
{/if}

<style>
  /* Borderless accordion — sits inside the hero card; the summary previews the next track. */
  .cn {
    margin-top: 0.9rem;
    border-top: 1px solid var(--border);
    padding-top: 0.8rem;
  }
  summary {
    display: flex;
    align-items: center;
    gap: 0.55rem;
    cursor: pointer;
    list-style: none;
    font-size: 0.85rem;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  .cn-label {
    flex: 0 0 auto;
    color: var(--accent);
    font-weight: 700;
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  .cn-title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
  }
  .cn-dj {
    flex: 0 0 auto;
    color: var(--text-dim);
    font-size: 0.76rem;
    max-width: 10ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chevron {
    flex: 0 0 auto;
    color: var(--text-dim);
    transition: transform 0.18s ease;
  }
  .cn[open] .chevron {
    transform: rotate(180deg);
  }
  ol {
    list-style: none;
    margin: 0.7rem 0 0;
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

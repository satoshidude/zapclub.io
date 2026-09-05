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
    return upcomingTracks(clubId, 3)
  })
</script>

{#if stage.djs.length > 0}
  <section class="cn" aria-label="Upcoming DJ queue">
    <div class="cn-head">
      <span class="cn-label lcd-card-title">Up next</span>
    </div>
    {#if next.length > 0}
      <ol>
        {#each next as item, i (item.videoId + i)}
          {@const profile = useProfile(item.dj)}
          <li class:first={i === 0}>
            <span class="idx">{i + 1}</span>
            <span class="title">{item.title}</span>
            <span class="dj"><span class="by">by</span> {displayName(item.dj, profile)}</span>
          </li>
        {/each}
      </ol>
    {:else}
      <p class="empty">No tracks queued yet.</p>
    {/if}
  </section>
{/if}

<style>
  /* Compact relay-derived round-robin preview, placed directly below the stage avatars. */
  .cn {
    margin-top: 0.1rem;
    padding-top: 0.7rem;
    border-top: 1px solid rgba(241, 243, 244, 0.2);
  }
  .cn-head {
    display: flex;
    align-items: center;
    min-height: 24px;
    margin-bottom: 0.25rem;
  }
  .cn-label {
    color: var(--lcd-text-bright);
    font-size: 0.92rem;
  }
  ol {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }
  li {
    display: grid;
    grid-template-columns: 1.35rem minmax(0, 1fr) minmax(6rem, auto);
    align-items: center;
    gap: 0.5rem;
    min-height: 30px;
    border-bottom: 1px solid rgba(241, 243, 244, 0.1);
    color: var(--lcd-text-soft);
    font-size: 0.76rem;
  }
  li.first {
    color: var(--lcd-text-bright);
  }
  .idx {
    color: var(--accent);
    font-variant-numeric: tabular-nums;
    text-align: center;
  }
  .title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dj {
    max-width: 15ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--lcd-text);
    text-align: right;
  }
  .by {
    color: var(--lcd-text-dim);
  }
  .empty {
    margin: 0;
    color: var(--lcd-text-dim);
    font-size: 0.76rem;
    line-height: 30px;
  }
  @media (max-width: 560px) {
    li {
      grid-template-columns: 1.15rem minmax(0, 1fr) minmax(4.5rem, 30%);
      gap: 0.35rem;
      font-size: 0.69rem;
    }
    .dj {
      max-width: none;
    }
    .by {
      display: none;
    }
  }
</style>

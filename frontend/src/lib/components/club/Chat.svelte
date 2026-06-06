<script lang="ts">
  import { chat, sendChat } from '../../nostr/chat.svelte'
  import { auth } from '../../nostr/auth.svelte'
  import { useProfile, displayName, avatarUrl } from '../../nostr/profiles.svelte'
  import { presence } from '../../nostr/presence.svelte'

  interface Props {
    groupId: string
    /** Only members may send (the relay rejects non-members). */
    canChat: boolean
    /** Host/moderator: may delete messages. */
    canModerate?: boolean
    /** Click an author → open their profile. */
    onauthor?: (pubkey: string) => void
    /** Moderation: delete a message. */
    ondelete?: (eventId: string) => void
  }
  let { groupId, canChat, canModerate = false, onauthor, ondelete }: Props = $props()

  let draft = $state('')
  let sending = $state(false)
  let error = $state('')
  let listEl = $state<HTMLDivElement | null>(null)

  // Scroll to the bottom on new messages.
  let count = $derived(chat.messages.length)
  $effect(() => {
    void count
    if (listEl) listEl.scrollTop = listEl.scrollHeight
  })

  async function send(e: SubmitEvent) {
    e.preventDefault()
    if (!draft.trim()) return
    sending = true
    error = ''
    try {
      await sendChat(groupId, draft)
      draft = ''
    } catch (err) {
      error = err instanceof Error ? err.message : 'Could not send message.'
    } finally {
      sending = false
    }
  }
</script>

<section class="chat card">
  <h3>Chat</h3>
  <div class="messages" bind:this={listEl}>
    {#if chat.messages.length === 0}
      <p class="empty">No messages yet. Say hi 👋</p>
    {:else}
      {#each chat.messages as m (m.id)}
        {@const profile = useProfile(m.pubkey)}
        <div class="msg">
          <img class="av" class:online={presence.isOnline(m.pubkey)} src={avatarUrl(m.pubkey, profile)} alt="" width="24" height="24" />
          <div class="body">
            <button class="author" onclick={() => onauthor?.(m.pubkey)}>
              {displayName(m.pubkey, profile)}
            </button>
            <span class="text">{m.content}</span>
          </div>
          {#if canModerate}
            <button class="del" title="Delete message" onclick={() => ondelete?.(m.id)}>✕</button>
          {/if}
        </div>
      {/each}
    {/if}
  </div>

  {#if canChat}
    <form onsubmit={send}>
      <input bind:value={draft} placeholder="Message…" maxlength="500" />
      <button type="submit" class="btn btn-primary btn-sm" disabled={sending}>Send</button>
    </form>
    {#if error}<p class="send-error">{error}</p>{/if}
  {:else if auth.isLoggedIn}
    <p class="hint">Join the club to chat.</p>
  {/if}
</section>

<style>
  .chat {
    background: var(--bg-elev);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1rem;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  h3 {
    margin: 0 0 0.7rem;
    font-size: 0.95rem;
  }
  .messages {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
    min-height: 160px;
    max-height: 420px;
  }
  .empty {
    color: var(--text-dim);
    font-size: 0.88rem;
  }
  .msg {
    display: flex;
    gap: 0.5rem;
    align-items: flex-start;
  }
  .msg .del {
    margin-left: auto;
    flex: none;
    width: 20px;
    height: 20px;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: var(--bg-elev-2);
    color: var(--text-dim);
    font-size: 0.62rem;
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.12s ease;
  }
  .msg:hover .del {
    opacity: 1;
  }
  .msg .del:hover {
    color: var(--danger);
    border-color: var(--danger);
  }
  .av {
    border-radius: 50%;
    flex: none;
    margin-top: 2px;
    background: var(--bg-elev-2);
  }
  .av.online {
    box-shadow: 0 0 0 2px var(--accent-2), 0 0 6px rgba(177, 77, 255, 0.5);
  }
  .body {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .author {
    font-size: 0.75rem;
    color: var(--accent);
    font-weight: 600;
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    text-align: left;
  }
  .author:hover {
    text-decoration: underline;
  }
  .text {
    font-size: 0.92rem;
    word-break: break-word;
  }
  .send-error {
    color: var(--danger);
    font-size: 0.8rem;
    margin: 0.4rem 0 0;
  }
  .hint {
    color: var(--text-dim);
    font-size: 0.85rem;
    margin: 0.7rem 0 0;
    text-align: center;
  }
  form {
    display: flex;
    gap: 0.5rem;
    margin-top: 0.7rem;
  }
  form input {
    flex: 1;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.7rem;
    color: var(--text);
  }
  form input:focus {
    outline: none;
    border-color: var(--accent-2);
  }
</style>

<script lang="ts">
  import type { Snippet } from 'svelte'
  import { tick } from 'svelte'
  import type { Event } from 'nostr-tools/pure'
  import { npubEncode } from 'nostr-tools/nip19'
  import { auth } from '../../nostr/auth.svelte'
  import {
    CHAT_MAX_LENGTH,
    mergeChatMessages,
    publishChat,
    subscribeChat,
  } from '../../nostr/chat'
  import { avatarUrl, displayName, useProfile } from '../../nostr/profiles.svelte'
  import { goUser } from '../../router.svelte'
  import { visibleMemberRows } from './memberRoster'

  let {
    groupId,
    memberCount = 0,
    membersExpanded = false,
    children,
  }: {
    groupId: string
    memberCount?: number
    membersExpanded?: boolean
    children?: Snippet
  } = $props()

  let messages = $state<Event[]>([])
  let draft = $state('')
  let loading = $state(true)
  let sending = $state(false)
  let error = $state('')
  let stream: HTMLDivElement
  let composerInput: HTMLTextAreaElement
  const memberRows = $derived(visibleMemberRows(memberCount, membersExpanded))

  const QUICK_REACTIONS = [
    { icon: '🔥', label: 'Fire' },
    { icon: '🙌', label: 'Hands up' },
    { icon: '🪩', label: 'Disco ball' },
    { icon: '🕺', label: 'Dance' },
    { icon: '🎉', label: 'Party' },
  ]

  function isNearBottom(): boolean {
    return !stream || stream.scrollHeight - stream.scrollTop - stream.clientHeight < 72
  }

  async function scrollToBottom(behavior: ScrollBehavior = 'auto') {
    await tick()
    stream?.scrollTo({ top: stream.scrollHeight, behavior })
  }

  $effect(() => {
    const id = groupId
    const me = auth.pubkey
    if (!id || !me || !auth.canSign) return
    messages = []
    loading = true
    error = ''
    const stop = subscribeChat(id, {
      onMessage(event) {
        const follow = isNearBottom() || event.pubkey === me
        messages = mergeChatMessages(messages, event)
        if (follow) void scrollToBottom(event.pubkey === me ? 'smooth' : 'auto')
      },
      onEose() {
        loading = false
        void scrollToBottom()
      },
      onClose(reason) {
        loading = false
        if (/restricted|auth/i.test(reason)) error = 'Chat access ended. Rejoin the club to continue.'
      },
    })
    return stop
  })

  function fitComposer() {
    if (!composerInput) return
    composerInput.style.height = 'auto'
    const nextHeight = Math.min(composerInput.scrollHeight, 96)
    composerInput.style.height = `${Math.max(38, nextHeight)}px`
    composerInput.style.overflowY = composerInput.scrollHeight > 96 ? 'auto' : 'hidden'
  }

  async function sendContent(content: string, clearDraft = false) {
    if (sending || !content.trim()) return
    sending = true
    error = ''
    try {
      const sent = await publishChat(groupId, content)
      messages = mergeChatMessages(messages, sent)
      if (clearDraft) {
        draft = ''
        await tick()
        fitComposer()
      }
      await scrollToBottom('smooth')
    } catch (cause) {
      error = String((cause as Error)?.message ?? cause)
    } finally {
      sending = false
    }
  }

  function send() {
    void sendContent(draft, true)
  }

  function sendReaction(icon: string) {
    void sendContent(icon)
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void send()
    }
  }

  function openProfile(pubkey: string) {
    goUser(npubEncode(pubkey))
  }

  function timeLabel(timestamp: number): string {
    return new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(
      timestamp * 1000,
    )
  }
</script>

<section
  class="chat led-zone"
  class:has-members={!!children}
  class:members-expanded={membersExpanded}
  style={`--visible-member-rows: ${memberRows}`}
  aria-label="Club chat"
>
  <header class="chat-head lcd-card-heading">
    <h3 class="lcd-card-title">Club chat</h3>
    <span class="privacy" title="Only signed-in club members can read this chat">
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <rect x="5" y="10" width="14" height="10" rx="2"></rect>
        <path d="M8 10V7a4 4 0 0 1 8 0v3"></path>
      </svg>
      Members only
    </span>
  </header>

  <div class="stream" bind:this={stream} role="log" aria-live="polite" aria-relevant="additions">
    {#if loading && messages.length === 0}
      <p class="state">Tuning into the club…</p>
    {:else if messages.length === 0}
      <p class="state">No messages yet. Open the channel.</p>
    {:else}
      {#each messages as message (message.id)}
        {@const profile = useProfile(message.pubkey)}
        <article class="message" class:mine={message.pubkey === auth.pubkey}>
          <button class="identity" onclick={() => openProfile(message.pubkey)} title="Open profile">
            <img src={avatarUrl(message.pubkey, profile)} alt="" width="30" height="30" loading="lazy" />
          </button>
          <div class="message-main">
            <div class="meta">
              <button class="name" onclick={() => openProfile(message.pubkey)}>
                {message.pubkey === auth.pubkey ? 'You' : displayName(message.pubkey, profile)}
              </button>
              <time datetime={new Date(message.created_at * 1000).toISOString()}>{timeLabel(message.created_at)}</time>
            </div>
            <p>{message.content}</p>
          </div>
        </article>
      {/each}
    {/if}
  </div>

  {#if children}
    <div class="chat-members">{@render children()}</div>
  {/if}

  <form class="composer" onsubmit={(event) => { event.preventDefault(); void send() }}>
    <div class="quick-reactions" aria-label="Quick party reactions">
      {#each QUICK_REACTIONS as reaction (reaction.icon)}
        <button
          class="reaction-button"
          type="button"
          disabled={sending}
          aria-label={`Send ${reaction.label}`}
          title={`Send ${reaction.label}`}
          onclick={() => sendReaction(reaction.icon)}
        ><span class="reaction-icon" aria-hidden="true">{reaction.icon}</span></button>
      {/each}
    </div>
    <textarea
      bind:value={draft}
      bind:this={composerInput}
      oninput={fitComposer}
      onkeydown={handleKeydown}
      maxlength={CHAT_MAX_LENGTH}
      rows="1"
      placeholder="Message the club"
      aria-label="Message the club"
      disabled={sending}
    ></textarea>
    <button class="send-button" type="submit" disabled={sending || !draft.trim()} aria-label="Send message">
      {#if sending}<span class="sending">···</span>{:else}<span aria-hidden="true">↗</span>{/if}
      <span class="send-label">Send</span>
    </button>
  </form>
  {#if error}<p class="error">{error}</p>{/if}
</section>

<style>
  .chat {
    position: relative;
    display: grid;
    grid-template-areas:
      'head'
      'stream'
      'composer'
      'error';
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: auto minmax(220px, 380px) auto;
    min-width: 0;
    padding: 0.9rem 1rem 1rem;
    color: var(--lcd-text);
    background: transparent;
    font-family: 'DotGothic16', ui-monospace, monospace;
  }
  .chat.has-members {
    grid-template-areas:
      'head head'
      'stream members'
      'composer members'
      'error error';
    grid-template-columns: minmax(0, 1fr) minmax(220px, 270px);
    grid-template-rows:
      auto
      minmax(220px, calc(var(--visible-member-rows, 6) * 51px + 16px))
      auto
      auto;
    transition: grid-template-rows 180ms ease-out;
  }
  .chat-members {
    grid-area: members;
    min-width: 0;
    min-height: 0;
    margin-top: 0.75rem;
    overflow: hidden;
    border-left: 1px solid rgba(201, 206, 209, 0.18);
  }
  .chat-head {
    grid-area: head;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  h3 { margin: 0; }
  .privacy {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    color: var(--lcd-text-dim);
    font-size: 0.73rem;
  }
  .privacy svg {
    width: 15px;
    height: 15px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.7;
  }
  .stream {
    grid-area: stream;
    min-height: 220px;
    margin-top: 0.75rem;
    padding: 0.25rem 0.25rem 0.55rem;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-color: rgba(241, 243, 244, 0.3) transparent;
  }
  .state {
    display: grid;
    min-height: 190px;
    place-items: center;
    margin: 0;
    color: var(--lcd-text-dim);
    font-size: 0.85rem;
  }
  .message {
    display: grid;
    grid-template-columns: 38px minmax(0, 1fr);
    gap: 0.55rem;
    padding: 0.45rem 0.35rem;
    border-bottom: 1px solid rgba(201, 206, 209, 0.11);
  }
  .message:last-child { border-bottom-color: transparent; }
  .identity,
  .name {
    padding: 0;
    border: 0;
    color: inherit;
    background: none;
    font: inherit;
    cursor: pointer;
  }
  .identity {
    width: 30px;
    height: 30px;
    margin-top: 0.12rem;
    border-radius: 50%;
  }
  .identity img {
    display: block;
    width: 30px;
    height: 30px;
    border-radius: 50%;
    object-fit: cover;
    filter: saturate(0.7) contrast(1.06);
  }
  .message-main { min-width: 0; }
  .meta {
    display: flex;
    align-items: baseline;
    gap: 0.55rem;
  }
  .name {
    max-width: min(36ch, 70%);
    overflow: hidden;
    color: var(--lcd-text);
    font-size: 0.78rem;
    text-overflow: ellipsis;
    text-shadow: var(--lcd-text-shadow);
    white-space: nowrap;
  }
  .mine .name { color: var(--lcd-text-bright); }
  time {
    color: var(--lcd-text-dim);
    font-size: 0.66rem;
    font-variant-numeric: tabular-nums;
  }
  .message p {
    margin: 0.14rem 0 0;
    overflow-wrap: anywhere;
    color: var(--lcd-text-soft);
    font-family: system-ui, sans-serif;
    font-size: 0.9rem;
    line-height: 1.42;
    white-space: pre-wrap;
  }
  .composer {
    grid-area: composer;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: end;
    gap: 0.45rem;
    padding-top: 0.7rem;
    border-top: 1px solid rgba(201, 206, 209, 0.22);
  }
  .quick-reactions {
    display: flex;
    align-items: center;
    gap: 0.2rem;
  }
  .reaction-button {
    display: grid;
    width: 29px;
    height: 38px;
    place-items: center;
    padding: 0;
    border: 0;
    border-radius: 0;
    color: var(--lcd-text-bright);
    background: transparent;
    box-shadow: none;
    font-size: 0.96rem;
    line-height: 1;
    cursor: pointer;
  }
  .reaction-icon {
    filter:
      grayscale(1)
      brightness(1.75)
      contrast(1.12)
      drop-shadow(0 0 3px rgba(207, 233, 255, 0.72));
    transform: scale(1);
  }
  .reaction-button:hover:not(:disabled),
  .reaction-button:focus-visible {
    color: #ffffff;
    background: transparent;
    box-shadow: none;
  }
  .reaction-button:hover:not(:disabled) .reaction-icon,
  .reaction-button:focus-visible .reaction-icon {
    filter:
      grayscale(1)
      brightness(2.15)
      contrast(1.18)
      drop-shadow(0 0 5px rgba(235, 247, 255, 0.95));
    transform: scale(1.12);
  }
  .reaction-button:focus-visible { outline: none; }
  .reaction-button:active:not(:disabled) { transform: translateY(1px); }
  .reaction-button:disabled {
    opacity: 0.42;
    cursor: default;
  }
  textarea {
    box-sizing: border-box;
    width: 100%;
    height: 38px;
    min-height: 38px;
    max-height: 96px;
    resize: none;
    overflow-y: hidden;
    padding: 0.45rem 0.65rem;
    border: 0;
    border-radius: 0;
    outline: none;
    color: var(--lcd-text-bright);
    caret-color: var(--lcd-text-bright);
    background: transparent;
    box-shadow: none;
    font: 0.86rem/1.4 'DotGothic16', ui-monospace, monospace;
    text-shadow: var(--lcd-text-shadow);
  }
  textarea::placeholder {
    color: color-mix(in srgb, var(--lcd-text-soft) 82%, transparent);
    text-shadow: none;
  }
  textarea:focus {
    border: 0;
    background: transparent;
    box-shadow: none;
    text-shadow: 0 0 4px rgba(235, 241, 244, 0.85), 0 0 11px rgba(145, 195, 235, 0.3);
  }
  .send-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.42rem;
    min-width: 72px;
    height: 38px;
    padding: 0.35rem 0.58rem;
    border: 0;
    border-radius: 0;
    color: var(--lcd-text-bright);
    background: transparent;
    box-shadow: none;
    font: 0.76rem 'DotGothic16', ui-monospace, monospace;
    text-shadow: 0 0 3px rgba(207, 233, 255, 0.76), 0 0 8px rgba(145, 195, 235, 0.22);
    cursor: pointer;
  }
  .send-button:hover:not(:disabled),
  .send-button:focus-visible {
    color: #ffffff;
    background: transparent;
    box-shadow: none;
    text-shadow: 0 0 5px #ffffff, 0 0 12px rgba(177, 220, 255, 0.52);
  }
  .send-button:focus-visible { outline: none; }
  .send-button:active:not(:disabled) { transform: translateY(1px); }
  .send-button:disabled {
    opacity: 0.38;
    filter: none;
    cursor: default;
  }
  .sending { letter-spacing: 0.12em; }
  .error {
    grid-area: error;
    margin: 0.45rem 0 0;
    color: #ffd0d0;
    font: 0.76rem/1.35 system-ui, sans-serif;
  }
  @media (max-width: 700px) {
    .chat {
      grid-template-rows: auto minmax(190px, 46vh) auto;
      padding: 0.8rem 0.75rem;
    }
    .chat.has-members {
      grid-template-areas:
        'head'
        'members'
        'stream'
        'composer'
        'error';
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: auto auto minmax(190px, 46vh) auto auto;
    }
    .chat-members {
      margin-top: 0.6rem;
      overflow: visible;
      border-left: 0;
      border-bottom: 1px solid rgba(201, 206, 209, 0.15);
    }
    .privacy { font-size: 0.68rem; }
    .stream { min-height: 190px; }
    .message {
      grid-template-columns: 34px minmax(0, 1fr);
      gap: 0.4rem;
      padding-inline: 0.15rem;
    }
    .composer { grid-template-columns: minmax(0, 1fr) auto; }
    .quick-reactions { grid-column: 1 / -1; }
    .send-label { display: none; }
    .send-button { min-width: 48px; }
  }

  @media (prefers-reduced-motion: reduce) {
    .chat.has-members { transition: none; }
  }
</style>

import { describe, expect, it } from 'vitest'
import source from './ClubList.svelte?raw'

describe('club search suggestions', () => {
  it('selects on pointerdown before Safari removes the blurred suggestion list', () => {
    expect(source).toMatch(
      /class="search-suggestion"[\s\S]*?onpointerdown=\{\(event\) => \{[\s\S]*?event\.preventDefault\(\)[\s\S]*?selectClubSuggestion\(club\.id\)/,
    )
    expect(source).toContain('onclick={() => selectClubSuggestion(club.id)}')
  })

  it('renders live clubs with the player information hierarchy and no player controls', () => {
    expect(source).toContain('class="club-player-status"')
    expect(source).toContain('class="club-player-title"')
    expect(source).toContain('class="club-player-artist"')
    expect(source).toContain('class="club-player-name"')
    expect(source).toContain('class="club-player-actions"')
    expect(source).toContain('class="club-player-tags"')
    expect(source).toContain('class="club-player-separator" aria-hidden="true">|</span>')
    expect(source).toContain('class="tag host-tag"')
    expect(source).toContain('class="tag members-tag"')
    expect(source).toContain('fetchClubMemberCounts(clubIds)')
    expect(source).toContain('subscribeClubMemberCounts(ids')
    expect(source).toContain('{#if memberCounts[club.id]}')
    expect(source).toContain('class="tag listener-tag"')
    expect(source).toContain('subscribeListenerCounts(ids')
    expect(source).toContain('{#if (listenerCounts[club.id] ?? 0) > 0}')
    expect(source).toContain('{listenerCounts[club.id]}')
    expect(source).toContain('{#if (listenerCounts[TELEGRAM_BOT_CLUB_ID] ?? 0) > 0}')
    expect(source).toContain('{listenerCounts[TELEGRAM_BOT_CLUB_ID]}')
    expect(source).not.toContain('subscribeClubPresence')
    expect(source).toContain('class="club-player-byline"')
    expect(source).toContain('class="club-player-title" use:marquee')
    expect(source).toContain('<span class="enter-club">Enter club</span>')
    expect(source).not.toContain('class="club-directory-row"')
    expect(source).toMatch(
      /class="club-player-row"[\s\S]*?class="pic club-player-pic"[\s\S]*?class="club-player-content"/,
    )
    expect(source).toMatch(
      /class="club-player-status">\s*<span class="club-player-club">[\s\S]*?class="club-player-name"[\s\S]*?<span class="club-player-dj">/,
    )
    expect(source).toMatch(/\.club-player-title\s*\{[\s\S]*?font-size: 1rem;/)
    expect(source).toMatch(/\.club-player-tags\s*\{[\s\S]*?font-size: 14px;/)
    expect(source).toContain('.club-player-tags { font-size: 12px; }')
    expect(source).toContain('height: 104px;')
    expect(source).toMatch(
      /:global\(body\.site-led-page\) \.hero-title \{[\s\S]*?font-family: var\(--font\);[\s\S]*?font-weight: 800;/,
    )
    expect(source).toContain('.club-player-row:hover .club-player-title:global([data-mq]) .mq-inner')
    expect(source).toMatch(
      /\.club-player-artist\s*\{[\s\S]*?color: var\(--text-dim\);[\s\S]*?font-size: 0\.82rem;/,
    )
    expect(source).toMatch(
      /class="club-player-row telegram-club-row"[\s\S]*?class="club-player-title club-player-placeholder"[\s\S]*?class="enter-club">Enter club/,
    )
    expect(source).toContain("botStageDj ? 'DJ is loading tracks' : 'No DJ on stage'")
    expect(source).toContain("botStageDj ? 'Playback starts automatically.' : 'Take the first slot and start a set.'")
    expect(source).not.toMatch(
      /\{#if auth\.canSign && !myIds\.has\(TELEGRAM_BOT_CLUB_ID\)\}[\s\S]*?Enter club/,
    )
  })
})

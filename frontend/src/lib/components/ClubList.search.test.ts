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
    expect(source).toContain('class="tag club-player-dj-tag"')
    expect(source).toContain('<span>DJ:</span>')
    expect(source).toContain('class="club-player-dj-accent"')
    expect(source).toContain('class="club-player-sats-icon" aria-hidden="true">⚡︎</span>')
    expect(source).toContain('class="club-player-dj-name">{displayName(live.dj, liveDjProfile)}</span>')
    expect(source).not.toContain('class="club-player-dj"')
    expect(source).not.toContain('<span class="live-label">ON AIR</span>')
    expect(source).not.toContain('subscribeClubPresence')
    expect(source).toContain('class="club-player-byline"')
    expect(source).toContain('class="club-player-title" use:marquee')
    expect(source).toContain('<span class="enter-club">Enter club</span>')
    expect(source).toMatch(/\.club-player-actions\s*\{[\s\S]*?align-self: stretch;[\s\S]*?align-items: flex-end;[\s\S]*?transform: translateY\(11px\);/)
    expect(source).toMatch(/@media \(max-width: 560px\)[\s\S]*?\.club-player-actions \{ transform: translateY\(10px\); \}/)
    expect(source).toMatch(/\.enter-club\s*\{[\s\S]*?color: #c084fc;[\s\S]*?text-shadow: 0 0 3px rgba\(192, 132, 252, 0\.82\)/)
    expect(source).not.toContain('class="club-directory-row"')
    expect(source).toMatch(
      /class="club-player-row"[\s\S]*?class="pic club-player-pic"[\s\S]*?class="club-player-content"/,
    )
    expect(source).toMatch(
      /class="club-player-status">\s*<span class="club-player-club">[\s\S]*?class="club-player-name"[\s\S]*?class="club-player-tags"[\s\S]*?class="tag listener-tag"[\s\S]*?class="tag club-player-dj-tag"[\s\S]*?class="club-player-dj-name">\{displayName\(live\.dj, liveDjProfile\)\}<\/span>[\s\S]*?<\/span>\s*<\/span>\s*<\/div>\s*<div class="club-player-title"/,
    )
    expect(source).toMatch(/\.club-player-title\s*\{[\s\S]*?font-size: 1rem;/)
    expect(source).toMatch(/\.club-player-status\s*\{[\s\S]*?flex-direction: column;[\s\S]*?align-items: flex-start;[\s\S]*?margin: 0 0 7px;/)
    expect(source).toMatch(/\.club-player-club\s*\{[\s\S]*?flex-direction: column;[\s\S]*?align-items: flex-start;/)
    expect(source).toMatch(/\.club-player-name\s*\{[\s\S]*?width: 100%;/)
    expect(source).toMatch(/\.club-player-tags \.club-player-dj-tag\s*\{[\s\S]*?flex: 0 1 auto;[\s\S]*?overflow: hidden;/)
    expect(source).toMatch(/\.club-player-dj-accent\s*\{[\s\S]*?display: inline-flex;/)
    expect(source).toMatch(/\.club-player-dj-accent\s*\{[\s\S]*?color: #f4e04d;[\s\S]*?font: inherit;[\s\S]*?letter-spacing: inherit;[\s\S]*?text-shadow: var\(--lcd-text-shadow\);/)
    expect(source).toContain('.club-player-tags > .club-player-separator:first-child { display: none; }')
    expect(source).toMatch(/\.club-player-tags\s*\{[\s\S]*?flex-wrap: nowrap;[\s\S]*?font-size: 14px;[\s\S]*?white-space: nowrap;/)
    expect(source).toContain('.club-player-tags { font-size: 12px; }')
    expect(source).toContain('min-height: 112px;')
    expect(source).toMatch(/\.club-player-row\s*\{[\s\S]*?grid-template-columns: 128px minmax\(0, 1fr\);[\s\S]*?align-items: stretch;[\s\S]*?padding: 0;/)
    expect(source).toMatch(/\.club-player-pic\s*\{[\s\S]*?align-self: stretch;/)
    expect(source).toMatch(/\.pic\s*\{[\s\S]*?width: 128px;[\s\S]*?height: auto;[\s\S]*?border-radius: 0;/)
    expect(source).toMatch(/\.pic-img\s*\{[\s\S]*?height: 100%;[\s\S]*?object-fit: cover;/)
    expect(source).toContain("import { clubAvatar } from '../avatar'")
    expect(source).not.toContain('DEFAULT_CLUB_PICTURE')
    expect(source).toContain('<img src={club.picture || clubAvatar(club.owner || club.id)} alt="" width="38" height="38" />')
    expect(source).toContain('<img class="pic-img" src={club.picture || clubAvatar(club.owner || club.id)} alt="" />')
    expect(source).toContain('<img class="pic-img" src={telegramBotClub.picture || clubAvatar(telegramBotClub.owner || telegramBotClub.id)} alt="" />')
    expect(source).toMatch(/@media \(max-width: 560px\)[\s\S]*?grid-template-columns: 104px minmax\(0, 1fr\);[\s\S]*?min-height: 104px;/)
    expect(source).toContain('<h1 class="hero-title site-h1">')
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

  it('uses separate decorative hero artwork for desktop and mobile', () => {
    expect(source).toContain('<div class="hero-art" aria-hidden="true"></div>')
    expect(source).toContain("url('/images/home-hero-turntable-desktop.webp')")
    expect(source).toContain("url('/images/home-hero-turntable-mobile.webp')")
    expect(source).toMatch(/\.hero-art\s*\{[\s\S]*?pointer-events: none;/)
    expect(source).toMatch(/:global\(body\.site-led-page\) \.hero\s*\{[\s\S]*?justify-content: flex-start;[\s\S]*?padding: 1rem;/)
  })

  it('renders the hero feature rail with semantic LED icons', () => {
    expect(source).toContain('<p class="eyebrow lcd-card-title">Social · Decentralized · Entertaining</p>')
    expect(source).toContain('class="hero-features" aria-label="Zapclub features"')
    expect(source).toContain('<span>deck conductor</span>')
    expect(source).toContain('<span>Zap the DJ</span>')
    expect(source).toContain('class="hero-feature hero-feature-nostr"')
    expect(source).toContain('<span>nostr driven experience</span>')
    expect(source).toContain('class="hero-feature-icon hero-feature-nostrich" src="/nostrich.png"')
    expect(source).toContain('class="hero-feature-icon hero-feature-sync"')
    expect(source).toContain('class="hero-feature-icon hero-feature-zap" aria-hidden="true">⚡︎</span>')
    expect(source).not.toContain('<span>no email</span>')
    expect(source).not.toContain('🎛️ Pass the deck')
    expect(source).toMatch(/\.hero-features\s*\{[\s\S]*?display: grid;[\s\S]*?grid-template-columns: max-content max-content;[\s\S]*?justify-content: flex-start;/)
    expect(source).toMatch(/\.hero-feature-nostr\s*\{[\s\S]*?grid-column: 1 \/ -1;/)
  })

  it('keeps the leaderboard and its relay request off the homepage', () => {
    expect(source).not.toContain("from '../nostr/leaderboard'")
    expect(source).not.toContain('fetchLeaderboard')
    expect(source).not.toContain('lbEntries')
    expect(source).not.toContain('class="lb-preview')
    expect(source).not.toContain('FULL LEADERBOARD')
  })
})

// Minimal history router for zapclub. Routes: "/", "/club/<id>", "/user/<npub>", "/admin".
// Caddy serves index.html for all paths (SPA fallback).

export type Route =
  | { name: 'home' }
  | { name: 'club'; id: string }
  | { name: 'user'; npub: string }
  | { name: 'admin' }
  | { name: 'howto' }
  | { name: 'about' }
  | { name: 'disclaimer' }
  | { name: 'leaderboard' }
  | { name: 'privacy' }
  | { name: 'terms' }
  | { name: 'legal' }

export function parseRoute(path: string): Route {
  const club = path.match(/^\/club\/([\w-]+)\/?$/)
  if (club) return { name: 'club', id: club[1] }
  const user = path.match(/^\/user\/(npub1[\w]+)\/?$/)
  if (user) return { name: 'user', npub: user[1] }
  if (/^\/admin\/?$/.test(path)) return { name: 'admin' }
  if (/^\/howto\/?$/.test(path)) return { name: 'howto' }
  if (/^\/about\/?$/.test(path)) return { name: 'about' }
  if (/^\/credits\/?$/.test(path)) return { name: 'about' }
  if (/^\/disclaimer\/?$/.test(path)) return { name: 'disclaimer' }
  if (/^\/leaderboard\/?$/.test(path)) return { name: 'leaderboard' }
  if (/^\/privacy\/?$/.test(path)) return { name: 'privacy' }
  if (/^\/terms\/?$/.test(path)) return { name: 'terms' }
  if (/^\/legal\/?$/.test(path)) return { name: 'legal' }
  return { name: 'home' }
}

const state = $state<{ route: Route }>({ route: parseRoute(location.pathname) })

if (typeof window !== 'undefined') {
  window.addEventListener('popstate', () => {
    state.route = parseRoute(location.pathname)
  })
}

export const router = {
  get route() {
    return state.route
  },
}

export function navigate(path: string): void {
  if (path !== location.pathname) history.pushState({}, '', path)
  state.route = parseRoute(path)
  if (typeof window !== 'undefined') {
    requestAnimationFrame(() => window.scrollTo({ top: 0, left: 0, behavior: 'auto' }))
  }
}

export function goHome(): void {
  navigate('/')
}

export function goClub(id: string): void {
  navigate(`/club/${id}`)
}

export function goUser(npub: string): void {
  navigate(`/user/${npub}`)
}

export function goAdmin(): void {
  navigate('/admin')
}

export function goHowto(): void {
  navigate('/howto')
}

export function goAbout(): void {
  navigate('/about')
}

export function goLeaderboard(): void {
  navigate('/leaderboard')
}

export function goPrivacy(): void {
  navigate('/privacy')
}

export function goTerms(): void {
  navigate('/terms')
}

export function goLegal(): void {
  navigate('/legal')
}

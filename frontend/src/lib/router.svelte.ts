// Minimal history router for zapclub. Routes: "/", "/club/<id>", "/user/<npub>", "/admin".
// Caddy serves index.html for all paths (SPA fallback).

export type Route =
  | { name: 'home' }
  | { name: 'club'; id: string }
  | { name: 'user'; npub: string }
  | { name: 'admin' }
  | { name: 'howto' }

function parse(path: string): Route {
  const club = path.match(/^\/club\/([\w-]+)\/?$/)
  if (club) return { name: 'club', id: club[1] }
  const user = path.match(/^\/user\/(npub1[\w]+)\/?$/)
  if (user) return { name: 'user', npub: user[1] }
  if (/^\/admin\/?$/.test(path)) return { name: 'admin' }
  if (/^\/howto\/?$/.test(path)) return { name: 'howto' }
  return { name: 'home' }
}

const state = $state<{ route: Route }>({ route: parse(location.pathname) })

if (typeof window !== 'undefined') {
  window.addEventListener('popstate', () => {
    state.route = parse(location.pathname)
  })
}

export const router = {
  get route() {
    return state.route
  },
}

export function navigate(path: string): void {
  if (path !== location.pathname) history.pushState({}, '', path)
  state.route = parse(path)
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

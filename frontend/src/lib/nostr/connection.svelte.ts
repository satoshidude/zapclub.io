import { pool } from './pool'

// Watches the connection to the club relay. SimplePool reconnects automatically;
// this watcher only makes the state VISIBLE (reconnect indicator) so brief drops
// don't look like a total outage.
const state = $state<{ clubConnected: boolean; known: boolean }>({
  clubConnected: true,
  known: false,
})

export const connection = {
  /** Is the club-relay connection up? (true while unknown = optimistic) */
  get clubConnected() {
    return state.clubConnected
  },
  /** Has a club-relay connection ever been attempted? */
  get known() {
    return state.known
  },
}

let timer: ReturnType<typeof setInterval> | null = null

export function startConnectionWatch(): void {
  if (timer) return
  const check = () => {
    let found = false
    let connected = false
    for (const [url, ok] of pool.listConnectionStatus()) {
      if (url.includes('relay.zapclub.io')) {
        found = true
        connected = ok
        break
      }
    }
    state.known = found
    state.clubConnected = !found || connected
  }
  check()
  timer = setInterval(check, 3000)
}

// Drives our own lean login modal. launchLogin()/launchSignup() in nostrLogin.ts
// open it; LoginDialog.svelte renders it.

const state = $state<{ open: boolean }>({ open: false })

export const loginDialog = {
  get open() {
    return state.open
  },
}

export function openLoginDialog(): void {
  state.open = true
}

export function closeLoginDialog(): void {
  state.open = false
}

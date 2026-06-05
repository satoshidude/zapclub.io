import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { initAuth } from './lib/nostr/nostrLogin'

initAuth()

const app = mount(App, {
  target: document.getElementById('app')!,
})

export default app

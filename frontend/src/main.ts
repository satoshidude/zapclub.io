import { mount } from 'svelte'
import '@fontsource/dotgothic16/latin.css'
import '@fontsource/ibm-plex-mono/latin-400.css'
import '@fontsource/ibm-plex-mono/latin-500.css'
import '@fontsource/jersey-25/latin.css'
import './app.css'
import App from './App.svelte'
import { initAuth } from './lib/nostr/nostrLogin'

initAuth()

const app = mount(App, {
  target: document.getElementById('app')!,
})

export default app

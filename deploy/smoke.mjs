#!/usr/bin/env node

const timeoutMs = Number(process.env.ZAPCLUB_SMOKE_TIMEOUT_MS || 10_000)

async function request(url, validate) {
  const response = await fetch(url, { signal: AbortSignal.timeout(timeoutMs) })
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`)
  const body = await response.text()
  if (validate && !validate(body)) throw new Error(`${url}: unexpected response`)
}

await request('https://zapclub.io/', (body) => /<html/i.test(body))
await request('https://zapclub.io/.well-known/nostr.json?name=satoshidude', (body) => {
  try {
    const document = JSON.parse(body)
    return typeof document.names?.satoshidude === 'string'
  } catch {
    return false
  }
})
await request('https://relay.zapclub.io/', (body) => body.length > 0)

await new Promise((resolve, reject) => {
  const socket = new WebSocket('wss://relay.zapclub.io/')
  const timer = setTimeout(() => {
    socket.close()
    reject(new Error('wss://relay.zapclub.io/: timed out waiting for EOSE'))
  }, timeoutMs)

  socket.addEventListener('open', () => {
    socket.send(JSON.stringify(['REQ', 'zapclub-smoke', { kinds: [39000], limit: 1 }]))
  })
  socket.addEventListener('message', (event) => {
    try {
      const message = JSON.parse(String(event.data))
      if (message[0] !== 'EOSE' || message[1] !== 'zapclub-smoke') return
      clearTimeout(timer)
      socket.send(JSON.stringify(['CLOSE', 'zapclub-smoke']))
      socket.close()
      resolve()
    } catch (error) {
      clearTimeout(timer)
      socket.close()
      reject(error)
    }
  })
  socket.addEventListener('error', () => {
    clearTimeout(timer)
    reject(new Error('wss://relay.zapclub.io/: WebSocket connection failed'))
  })
})

console.log('Zapclub public smoke checks passed.')

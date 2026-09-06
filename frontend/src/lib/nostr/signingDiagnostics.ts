/**
 * Local-only counters for Nostr signer activity.
 *
 * Nothing is sent to the relay or to an analytics endpoint. The snapshot contains only
 * aggregate counts, event kinds, fixed trigger labels and opaque connection ordinals. It never
 * retains event content, signatures or public keys. Enable it from a local browser console with
 * `window.__zapclubSigningDiagnostics.enable()`. The opt-in survives a reload so cold-login
 * activity can be captured; counter values themselves remain tab-local and are never persisted.
 */

export type SigningTrigger =
  | 'nip42-auth'
  | 'nostrLogin'
  | 'queue-user-action'
  | 'nip98'
  | 'unknown'

export type Nip07GetPublicKeySource = 'nostrLogin' | 'accountWatch' | 'unknown'

export interface SignableTemplate {
  kind: number
  tags?: ReadonlyArray<ReadonlyArray<string>>
}

export interface SigningCount {
  kind: number
  trigger: SigningTrigger
  /** Local ordinal derived from a NIP-42 challenge; absent for non-AUTH events. */
  connection?: number
  count: number
}

export interface Nip07GetPublicKeyCount {
  source: Nip07GetPublicKeySource
  count: number
}

export interface Nip42ConnectionCount {
  /** Opaque, tab-local ordinal. The challenge and relay URL are never exposed. */
  connection: number
  logicalRequests: number
  physicalSignEventCalls: number
}

export interface SigningDiagnosticsSnapshot {
  enabled: boolean
  logicalSignRequests: SigningCount[]
  physicalSignEventCalls: SigningCount[]
  nip07GetPublicKeyCalls: Nip07GetPublicKeyCount[]
  nip42Connections: Nip42ConnectionCount[]
}

interface SigningContext {
  trigger: SigningTrigger
  connection?: number
}

const AUTH_KIND = 22242
const NIP98_KIND = 27235
const MAX_RETAINED_CHALLENGES = 256
const ENABLED_STORAGE_KEY = 'zapclub:signing-diagnostics'
const keySeparator = '\u001f'

function readLocalOptIn(): boolean {
  if (import.meta.env.MODE === 'test') return false
  try {
    return typeof window !== 'undefined' && window.localStorage?.getItem(ENABLED_STORAGE_KEY) === '1'
  } catch {
    return false
  }
}

let enabled = readLocalOptIn()
let triggerByTemplate = new WeakMap<object, SigningTrigger>()
let connectionByChallenge = new Map<string, number>()
let nextConnection = 1
let logicalCounts = new Map<string, number>()
let physicalCounts = new Map<string, number>()
let nip07Counts = new Map<Nip07GetPublicKeySource, number>()

function challengeOf(template: SignableTemplate): string | undefined {
  return template.tags?.find((tag) => tag[0] === 'challenge')?.[1]
}

function connectionFor(template: SignableTemplate): number | undefined {
  if (template.kind !== AUTH_KIND) return undefined
  const challenge = challengeOf(template)
  if (!challenge) return undefined

  const existing = connectionByChallenge.get(challenge)
  if (existing !== undefined) return existing

  // Challenges are useful only while diagnosing the current tab. Bound the raw in-memory keys;
  // already aggregated counts keep their opaque ordinals when the lookup cache is rotated.
  if (connectionByChallenge.size >= MAX_RETAINED_CHALLENGES) connectionByChallenge.clear()
  const connection = nextConnection++
  connectionByChallenge.set(challenge, connection)
  return connection
}

function contextFor(template: SignableTemplate, fallback: SigningTrigger): SigningContext {
  const annotated = triggerByTemplate.get(template)
  const protocolTrigger = template.kind === AUTH_KIND
    ? 'nip42-auth'
    : template.kind === NIP98_KIND
      ? 'nip98'
      : undefined
  return {
    trigger: protocolTrigger ?? annotated ?? fallback,
    connection: connectionFor(template),
  }
}

function countKey(kind: number, context: SigningContext): string {
  return [String(kind), context.trigger, context.connection ?? ''].join(keySeparator)
}

function increment(map: Map<string, number>, key: string): void {
  map.set(key, (map.get(key) ?? 0) + 1)
}

function signingRows(counts: Map<string, number>): SigningCount[] {
  return [...counts.entries()]
    .map(([key, count]) => {
      const [kind, trigger, connection] = key.split(keySeparator)
      return {
        kind: Number(kind),
        trigger: trigger as SigningTrigger,
        ...(connection ? { connection: Number(connection) } : {}),
        count,
      }
    })
    .sort((a, b) => a.kind - b.kind || a.trigger.localeCompare(b.trigger) || (a.connection ?? -1) - (b.connection ?? -1))
}

/** Attach a fixed, non-sensitive cause label to a template before it reaches signEvent(). */
export function withSigningTrigger<T extends SignableTemplate>(template: T, trigger: SigningTrigger): T {
  triggerByTemplate.set(template, trigger)
  return template
}

/** Record one logical call of the application's signEvent wrapper. */
export function recordLogicalSignRequest(
  template: SignableTemplate,
  fallbackTrigger: SigningTrigger = 'unknown',
): void {
  if (!enabled) return
  const context = contextFor(template, fallbackTrigger)
  increment(logicalCounts, countKey(template.kind, context))
}

/** Record one physical invocation of the configured signer, including retry attempts. */
export function recordPhysicalSignEventCall(
  template: SignableTemplate,
  fallbackTrigger: SigningTrigger = 'unknown',
): void {
  if (!enabled) return
  const context = contextFor(template, fallbackTrigger)
  increment(physicalCounts, countKey(template.kind, context))
}

/** Record a NIP-07 provider call separately from event signing. */
export function recordNip07GetPublicKeyCall(source: Nip07GetPublicKeySource = 'unknown'): void {
  if (!enabled) return
  nip07Counts.set(source, (nip07Counts.get(source) ?? 0) + 1)
}

export function signingDiagnosticsSnapshot(): SigningDiagnosticsSnapshot {
  const logicalSignRequests = signingRows(logicalCounts)
  const physicalSignEventCalls = signingRows(physicalCounts)
  const nip42Connections = [...new Set(
    [...logicalSignRequests, ...physicalSignEventCalls]
      .filter((row) => row.kind === AUTH_KIND && row.connection !== undefined)
      .map((row) => row.connection as number),
  )]
    .sort((a, b) => a - b)
    .map((connection) => ({
      connection,
      logicalRequests: logicalSignRequests
        .filter((row) => row.kind === AUTH_KIND && row.connection === connection)
        .reduce((total, row) => total + row.count, 0),
      physicalSignEventCalls: physicalSignEventCalls
        .filter((row) => row.kind === AUTH_KIND && row.connection === connection)
        .reduce((total, row) => total + row.count, 0),
    }))
  return {
    enabled,
    logicalSignRequests,
    physicalSignEventCalls,
    nip07GetPublicKeyCalls: [...nip07Counts.entries()]
      .map(([source, count]) => ({ source, count }))
      .sort((a, b) => a.source.localeCompare(b.source)),
    nip42Connections,
  }
}

export function resetSigningDiagnostics(): void {
  triggerByTemplate = new WeakMap<object, SigningTrigger>()
  connectionByChallenge = new Map<string, number>()
  nextConnection = 1
  logicalCounts = new Map<string, number>()
  physicalCounts = new Map<string, number>()
  nip07Counts = new Map<Nip07GetPublicKeySource, number>()
}

export function setSigningDiagnosticsEnabled(next: boolean): void {
  enabled = next
  if (import.meta.env.MODE === 'test') return
  try {
    if (typeof window !== 'undefined' && window.localStorage) {
      if (next) window.localStorage.setItem(ENABLED_STORAGE_KEY, '1')
      else window.localStorage.removeItem(ENABLED_STORAGE_KEY)
    }
  } catch {
    // Storage can be disabled; the current in-memory session still works.
  }
}

export interface SigningDiagnosticsRuntimeApi {
  enable(): void
  disable(): void
  reset(): void
  snapshot(): SigningDiagnosticsSnapshot
}

declare global {
  interface Window {
    __zapclubSigningDiagnostics?: SigningDiagnosticsRuntimeApi
  }
}

if (typeof window !== 'undefined') {
  window.__zapclubSigningDiagnostics = {
    enable: () => {
      resetSigningDiagnostics()
      setSigningDiagnosticsEnabled(true)
    },
    disable: () => setSigningDiagnosticsEnabled(false),
    reset: resetSigningDiagnostics,
    snapshot: signingDiagnosticsSnapshot,
  }
}

// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  recordLogicalSignRequest,
  recordNip07GetPublicKeyCall,
  recordPhysicalSignEventCall,
  resetSigningDiagnostics,
  setSigningDiagnosticsEnabled,
  signingDiagnosticsSnapshot,
  withSigningTrigger,
} from './signingDiagnostics'

beforeEach(() => {
  setSigningDiagnosticsEnabled(true)
  resetSigningDiagnostics()
})

afterEach(() => {
  setSigningDiagnosticsEnabled(false)
  resetSigningDiagnostics()
})

describe('signing diagnostics', () => {
  it('separates logical requests from physical retry attempts by kind and trigger', () => {
    const queue = withSigningTrigger({ kind: 30103, tags: [['h', 'private-club-id']] }, 'queue-user-action')

    recordLogicalSignRequest(queue, 'nostrLogin')
    recordPhysicalSignEventCall(queue, 'nostrLogin')
    recordPhysicalSignEventCall(queue, 'nostrLogin')

    const snapshot = signingDiagnosticsSnapshot()
    expect(snapshot.logicalSignRequests).toEqual([
      { kind: 30103, trigger: 'queue-user-action', count: 1 },
    ])
    expect(snapshot.physicalSignEventCalls).toEqual([
      { kind: 30103, trigger: 'queue-user-action', count: 2 },
    ])
  })

  it('groups NIP-42 calls by opaque connection challenge without exposing the challenge', () => {
    const first = {
      kind: 22242,
      tags: [['relay', 'wss://relay.example'], ['challenge', 'sensitive-challenge-a']],
      content: 'must-not-leak',
      pubkey: 'f'.repeat(64),
      sig: 'signature-must-not-leak',
    }
    const sameConnection = {
      kind: 22242,
      tags: [['challenge', 'sensitive-challenge-a']],
    }
    const reconnected = {
      kind: 22242,
      tags: [['challenge', 'sensitive-challenge-b']],
    }

    recordLogicalSignRequest(first, 'nostrLogin')
    recordPhysicalSignEventCall(first, 'nostrLogin')
    recordPhysicalSignEventCall(sameConnection, 'nostrLogin')
    recordLogicalSignRequest(reconnected, 'nostrLogin')
    recordPhysicalSignEventCall(reconnected, 'nostrLogin')

    const snapshot = signingDiagnosticsSnapshot()
    expect(snapshot.nip42Connections).toEqual([
      { connection: 1, logicalRequests: 1, physicalSignEventCalls: 2 },
      { connection: 2, logicalRequests: 1, physicalSignEventCalls: 1 },
    ])
    expect(snapshot.logicalSignRequests).toEqual([
      { kind: 22242, trigger: 'nip42-auth', connection: 1, count: 1 },
      { kind: 22242, trigger: 'nip42-auth', connection: 2, count: 1 },
    ])

    const serialized = JSON.stringify(snapshot)
    expect(serialized).not.toContain('sensitive-challenge')
    expect(serialized).not.toContain('relay.example')
    expect(serialized).not.toContain('f'.repeat(64))
    expect(serialized).not.toContain('signature-must-not-leak')
    expect(serialized).not.toContain('must-not-leak')
  })

  it('counts NIP-07 getPublicKey calls separately by fixed source', () => {
    recordNip07GetPublicKeyCall('nostrLogin')
    recordNip07GetPublicKeyCall('accountWatch')
    recordNip07GetPublicKeyCall('accountWatch')

    expect(signingDiagnosticsSnapshot().nip07GetPublicKeyCalls).toEqual([
      { source: 'accountWatch', count: 2 },
      { source: 'nostrLogin', count: 1 },
    ])
  })

  it('classifies NIP-98 centrally without requiring HTTP call-site instrumentation', () => {
    recordLogicalSignRequest({ kind: 27235, tags: [] }, 'nostrLogin')
    recordPhysicalSignEventCall({ kind: 27235, tags: [] }, 'nostrLogin')

    expect(signingDiagnosticsSnapshot().logicalSignRequests).toEqual([
      { kind: 27235, trigger: 'nip98', count: 1 },
    ])
    expect(signingDiagnosticsSnapshot().physicalSignEventCalls).toEqual([
      { kind: 27235, trigger: 'nip98', count: 1 },
    ])
  })

  it('stays inert until explicitly enabled and exposes only a local runtime API', () => {
    setSigningDiagnosticsEnabled(false)
    resetSigningDiagnostics()
    recordLogicalSignRequest({ kind: 1 }, 'nostrLogin')
    expect(signingDiagnosticsSnapshot().logicalSignRequests).toEqual([])

    window.__zapclubSigningDiagnostics?.enable()
    recordLogicalSignRequest({ kind: 1 }, 'nostrLogin')
    expect(window.__zapclubSigningDiagnostics?.snapshot().logicalSignRequests).toEqual([
      { kind: 1, trigger: 'nostrLogin', count: 1 },
    ])
  })
})

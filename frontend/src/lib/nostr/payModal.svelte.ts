import { pollPaid, creditZap } from './zaps.svelte'

// Global "pay this Lightning invoice" modal — shared by the DJ zap button and the
// footer donation. Shows a QR + "open in wallet" + copy, and auto-closes (paid state)
// when an external payment is detected via the LUD-21 verify URL.
interface PayState {
  invoice: string
  sats: number
  label: string
  paid: boolean
  /** Zap recipient (DJ pubkey) — credited locally on payment. Empty for donations. */
  dj: string
}

const state = $state<PayState>({ invoice: '', sats: 0, label: '', paid: false, dj: '' })

export const payModal = {
  get open() {
    return !!state.invoice
  },
  get invoice() {
    return state.invoice
  },
  get sats() {
    return state.sats
  },
  get label() {
    return state.label
  },
  get paid() {
    return state.paid
  },
}

export function showPay(
  invoice: string,
  sats: number,
  label: string,
  opts: { verify?: string; dj?: string } = {},
): void {
  state.invoice = invoice
  state.sats = sats
  state.label = label
  state.paid = false
  state.dj = opts.dj ?? ''
  if (opts.verify) {
    void pollPaid(opts.verify, () => state.invoice === invoice).then((ok) => {
      if (ok && state.invoice === invoice) markPaid()
    })
  }
}

/** Marks the current invoice paid and optimistically credits the DJ's zap score. */
export function markPaid(): void {
  if (state.paid) return
  state.paid = true
  if (state.dj && state.invoice) creditZap(state.dj, state.sats, state.invoice)
}

export function hidePay(): void {
  state.invoice = ''
  state.sats = 0
  state.label = ''
  state.paid = false
  state.dj = ''
}

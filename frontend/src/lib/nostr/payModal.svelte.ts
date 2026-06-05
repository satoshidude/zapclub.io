import { pollPaid } from './zaps.svelte'

// Global "pay this Lightning invoice" modal — shared by the DJ zap button and the
// footer donation. Shows a QR + "open in wallet" + copy, and auto-closes (paid state)
// when an external payment is detected via the LUD-21 verify URL.
interface PayState {
  invoice: string
  sats: number
  label: string
  paid: boolean
}

const state = $state<PayState>({ invoice: '', sats: 0, label: '', paid: false })

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

export function showPay(invoice: string, sats: number, label: string, verify?: string): void {
  state.invoice = invoice
  state.sats = sats
  state.label = label
  state.paid = false
  if (verify) {
    void pollPaid(verify, () => state.invoice === invoice).then((ok) => {
      if (ok && state.invoice === invoice) state.paid = true
    })
  }
}

export function markPaid(): void {
  state.paid = true
}

export function hidePay(): void {
  state.invoice = ''
  state.sats = 0
  state.label = ''
  state.paid = false
}

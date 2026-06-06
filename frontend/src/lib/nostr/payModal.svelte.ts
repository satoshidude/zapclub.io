import { pollPaid, creditZap, watchInvoicePaid } from './zaps.svelte'

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
let closeWatch: (() => void) | null = null
let onPaidCb: (() => void) | null = null

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
  opts: { verify?: string; dj?: string; onPaid?: () => void } = {},
): void {
  state.invoice = invoice
  state.sats = sats
  state.label = label
  state.paid = false
  state.dj = opts.dj ?? ''
  onPaidCb = opts.onPaid ?? null
  closeWatch?.()
  closeWatch = null
  // Detect an external payment two ways:
  // 1. LUD-21 verify URL (if the LNURL server provides one — many, incl. nsnip.io, don't).
  if (opts.verify) {
    void pollPaid(opts.verify, () => state.invoice === invoice).then((ok) => {
      if (ok && state.invoice === invoice) markPaid()
    })
  }
  // 2. The 9735 zap receipt for this invoice (works without LUD-21). For a zap (dj set) this
  //    is usually the ONLY automatic signal → it's what lets the modal auto-close.
  if (state.dj) {
    closeWatch = watchInvoicePaid(invoice, state.dj, () => {
      if (state.invoice === invoice) markPaid()
    })
  }
}

/** Marks the current invoice paid and optimistically credits the DJ's zap score. */
export function markPaid(): void {
  if (state.paid) return
  state.paid = true
  if (state.dj && state.invoice) creditZap(state.dj, state.sats, state.invoice)
  if (onPaidCb) {
    const cb = onPaidCb
    onPaidCb = null
    cb() // e.g. paid-club entry → join after payment
  }
}

export function hidePay(): void {
  closeWatch?.()
  closeWatch = null
  onPaidCb = null
  state.invoice = ''
  state.sats = 0
  state.label = ''
  state.paid = false
  state.dj = ''
}

import type { Account } from './types'

export function amortPayment(balance: number, rate: number, years: number): number {
  const n = years * 12
  if (balance <= 0 || n <= 0) return 0
  const i = rate / 12
  return i === 0 ? balance / n : (balance * i) / (1 - Math.pow(1 + i, -n))
}

/** Effective monthly payment for a liability (mirrors the Go engine). */
export function effectivePayment(a: Account): number {
  const c = a.config
  if (a.category === 'credit_card') return c.monthly_payment || Math.max(25, c.balance * 0.01 + (c.balance * (c.apr ?? 0)) / 12)
  if (c.monthly_payment) return c.monthly_payment
  return amortPayment(c.balance, c.interest_rate ?? 0, c.remaining_years || 10)
}

/** Months until paid off, or null if it never amortises (payment <= interest). */
export function payoffMonths(a: Account): number | null {
  const c = a.config
  if (c.balance <= 0) return 0
  if (a.category === 'credit_card' && c.pay_in_full) return 0
  const rate = (a.category === 'credit_card' ? c.apr : c.interest_rate) ?? 0
  const pay = effectivePayment(a)
  let bal = c.balance
  for (let m = 1; m <= 1200; m++) {
    bal = bal * (1 + rate / 12) - pay
    if (bal <= 0.005) return m
  }
  return null
}

export function durationLabel(months: number | null): string {
  if (months == null) return 'never at this payment'
  if (months === 0) return 'paid off'
  const y = Math.floor(months / 12), m = months % 12
  return [y ? `${y}y` : '', m ? `${m}m` : ''].filter(Boolean).join(' ')
}

import { computed, reactive } from 'vue'
import { api, ApiError, setUnauthorizedHandler } from './api'
import type { Account, PlanEvent, PlanResult, Profile, User } from './types'

export const state = reactive({
  user: null as User | null,
  ready: false, // session check finished
  loaded: false, // accounts/profile/events fetched
  accounts: [] as Account[],
  events: [] as PlanEvent[],
  profile: null as Profile | null,
  configured: false,
  plan: null as PlanResult | null,
  planLoading: false,
  planError: '',
  profileRev: 0, // bumps whenever the profile is saved, so open pages can refresh
  toasts: [] as { id: number; kind: 'ok' | 'err'; text: string }[],
})

let toastId = 0
export function toast(text: string, kind: 'ok' | 'err' = 'ok') {
  const id = ++toastId
  state.toasts.push({ id, kind, text })
  setTimeout(() => { state.toasts = state.toasts.filter((t) => t.id !== id) }, kind === 'err' ? 5000 : 2800)
}

export function errMsg(e: unknown): string {
  return e instanceof ApiError ? e.message : e instanceof Error ? e.message : 'Something went wrong'
}

export function resetData() {
  state.accounts = []
  state.events = []
  state.profile = null
  state.configured = false
  state.plan = null
  state.loaded = false
}

setUnauthorizedHandler(() => {
  state.user = null
  resetData()
})

export async function bootstrap() {
  try {
    state.user = await api.me()
  } catch {
    state.user = null
  } finally {
    state.ready = true
  }
}

export async function loadData(force = false) {
  if (state.loaded && !force) return
  const [accounts, events, p] = await Promise.all([api.accounts(), api.events(), api.getProfile()])
  state.accounts = accounts
  state.events = events
  state.profile = p.profile
  state.configured = p.configured
  state.loaded = true
}

export function invalidatePlan() {
  state.plan = null
}

let planToken = 0
export async function ensurePlan(force = false) {
  if (state.plan && !force) return state.plan
  const token = ++planToken
  state.planLoading = true
  state.planError = ''
  try {
    const res = await api.simulate()
    if (token === planToken) state.plan = res
    return res
  } catch (e) {
    if (token === planToken) state.planError = errMsg(e)
    throw e
  } finally {
    if (token === planToken) state.planLoading = false
  }
}

export async function signOut() {
  try { await api.logout() } catch { /* ignore */ }
  state.user = null
  resetData()
}

// ---- derived financial position ----

export const position = computed(() => {
  let cash = 0, taxable = 0, taxDeferred = 0, taxFree = 0, homeValue = 0
  let mortgage = 0, cards = 0, debts = 0, monthlyContrib = 0, monthlyDebtPay = 0
  for (const a of state.accounts) {
    const c = a.config
    switch (a.category) {
      case 'cash': cash += c.balance; monthlyContrib += c.monthly_contribution ?? 0; break
      case 'investment':
        if (c.tax_treatment === 'tax_deferred') taxDeferred += c.balance
        else if (c.tax_treatment === 'tax_free') taxFree += c.balance
        else taxable += c.balance
        monthlyContrib += (c.monthly_contribution ?? 0) + (c.employer_match ?? 0)
        break
      case 'mortgage': mortgage += c.balance; homeValue += c.home_value ?? 0; monthlyDebtPay += c.monthly_payment ?? 0; break
      case 'credit_card': cards += c.balance; monthlyDebtPay += c.monthly_payment ?? 0; break
      case 'debt': debts += c.balance; monthlyDebtPay += c.monthly_payment ?? 0; break
    }
  }
  const investments = taxable + taxDeferred + taxFree
  const assets = cash + investments + homeValue
  const liabilities = mortgage + cards + debts
  return {
    cash, taxable, taxDeferred, taxFree, investments, homeValue, mortgage, cards, debts,
    assets, liabilities, netWorth: assets - liabilities, monthlyContrib, monthlyDebtPay,
  }
})

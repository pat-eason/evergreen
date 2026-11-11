import type {
  Account, Budget, BudgetMonth, Calculation, FreedomInputs, FreedomResult, PlanEvent, PlanResult, Profile, User,
} from './types'

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

let onUnauthorized: () => void = () => {}
export function setUnauthorizedHandler(fn: () => void) { onUnauthorized = fn }

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api' + path, {
    method,
    credentials: 'same-origin',
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (res.status === 204) return undefined as T
  let data: any = null
  try { data = await res.json() } catch { /* empty body */ }
  if (!res.ok) {
    if (res.status === 401 && !path.startsWith('/auth/')) onUnauthorized()
    throw new ApiError(res.status, data?.error ?? `Request failed (${res.status})`)
  }
  return data as T
}

export const api = {
  register: (email: string, password: string) => req<User>('POST', '/auth/register', { email, password }),
  login: (email: string, password: string) => req<User>('POST', '/auth/login', { email, password }),
  logout: () => req<{ ok: boolean }>('POST', '/auth/logout', {}),
  me: () => req<User>('GET', '/me'),
  changeEmail: (email: string, password: string) => req<User>('PUT', '/me/email', { email, password }),
  changePassword: (current_password: string, new_password: string) => req<{ ok: boolean }>('PUT', '/me/password', { current_password, new_password }),

  getProfile: () => req<{ profile: Profile; configured: boolean }>('GET', '/profile'),
  saveProfile: (p: Profile) => req<{ profile: Profile; configured: boolean }>('PUT', '/profile', p),

  accounts: () => req<Account[]>('GET', '/accounts'),
  createAccount: (a: Omit<Account, 'id'>) => req<Account>('POST', '/accounts', a),
  updateAccount: (a: Account) => req<Account>('PUT', `/accounts/${a.id}`, a),
  deleteAccount: (id: number) => req<void>('DELETE', `/accounts/${id}`),

  events: () => req<PlanEvent[]>('GET', '/events'),
  createEvent: (e: Omit<PlanEvent, 'id'>) => req<PlanEvent>('POST', '/events', e),
  updateEvent: (e: PlanEvent) => req<PlanEvent>('PUT', `/events/${e.id}`, e),
  deleteEvent: (id: number) => req<void>('DELETE', `/events/${id}`),

  budgetMonths: () => req<BudgetMonth[]>('GET', '/budgets'),
  budget: (month: string) => req<Budget>('GET', `/budgets/${month}`),
  saveBudget: (month: string, b: Pick<Budget, 'notes' | 'items'>) => req<Budget>('PUT', `/budgets/${month}`, b),
  deleteBudget: (month: string) => req<void>('DELETE', `/budgets/${month}`),
  copyBudget: (month: string, from: string) => req<Budget>('POST', `/budgets/${month}/copy`, { from }),
  applyBudget: (month: string) => req<{ accounts_updated: number; profile: Profile }>('POST', `/budgets/${month}/apply`, {}),

  simulate: (profile?: Partial<Profile>, trials?: number, fast = false) => req<PlanResult>('POST', '/plan/simulate', { profile, trials, fast }),
  freedom: (overrides: Partial<FreedomInputs> = {}) => req<FreedomResult>('POST', '/freedom/analyze', overrides),

  calculations: (kind?: string) => req<Calculation[]>('GET', '/calculations' + (kind ? `?kind=${kind}` : '')),
  saveCalculation: (c: { kind: string; name: string; inputs: unknown; result: unknown }) => req<Calculation>('POST', '/calculations', c),
  deleteCalculation: (id: number) => req<void>('DELETE', `/calculations/${id}`),
}

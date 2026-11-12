export type Category = 'cash' | 'investment' | 'mortgage' | 'credit_card' | 'debt'
export type TaxTreatment = 'taxable' | 'tax_deferred' | 'tax_free'
export type Strategy = 'smart' | 'taxable_first' | 'tax_deferred_first'

export interface User { id: string; email: string }

export interface Profile {
  first_name: string
  last_name: string
  birth_year: number
  current_age: number
  retirement_age: number
  end_age: number
  filing_status: 'single' | 'married'
  inflation: number
  return_pre: number
  return_post: number
  return_stdev: number
  contribution_growth: number
  retirement_spending: number
  annual_income: number
  annual_expenses: number
  safe_withdrawal_rate: number
  state_tax_rate: number
  dividend_yield: number
  withdrawal_strategy: Strategy
  roth_conversion_ceiling: number
}

export interface AccountConfig {
  balance: number
  apy?: number
  monthly_contribution?: number
  tax_treatment?: TaxTreatment
  expected_return?: number
  employer_match?: number
  cost_basis?: number
  expense_ratio?: number
  interest_rate?: number
  monthly_payment?: number
  remaining_years?: number
  home_value?: number
  home_appreciation?: number
  property_tax?: number
  insurance?: number
  apr?: number
  credit_limit?: number
  pay_in_full?: boolean
  debt_kind?: string
}

export interface Member {
  id: string
  first_name: string
  last_name: string
  relationship: 'spouse' | 'partner' | 'child' | 'parent' | 'other'
  age: number
}

/** "self", "joint", or a household member id (as a string). */
export type Owner = string

export interface Account {
  id: string
  owner: Owner
  name: string
  category: Category
  config: AccountConfig
}

export interface PlanEvent {
  id: string
  owner: Owner // start_age is this person's age
  kind: 'income' | 'spending'
  type: string
  name: string
  start_age: number
  years: number
  annual_amount: number
  inflation_adjusted: boolean
  taxable: boolean
}

export interface Buckets { cash: number; taxable: number; tax_deferred: number; tax_free: number; total: number }
export interface Item { name: string; type?: string; amount: number }

export interface YearRow {
  age: number
  year: number
  retired: boolean
  start: Buckets
  end: Buckets
  saving: number
  retirement_spending: number
  other_spending: number
  other_income: number
  cash_flow: number
  total_spending: number
  reinvested: number
  shortfall: number
  income_items: Item[] | null
  spending_items: Item[] | null
  withdrawals: { cash: number; taxable: number; tax_deferred: number; tax_free: number; rmd: number; roth_converted: number }
  taxes: { federal: number; state: number; gains: number; total: number; marginal_rate: number }
  insights: string[] | null
  portfolio_p10: number
  portfolio_p50: number
  portfolio_p90: number
}

export interface PlanSummary {
  success_rate: number
  verdict: string
  planned_monthly: number
  projected_monthly: number
  portfolio_today: number
  portfolio_at_retirement: number
  portfolio_at_retirement_p10: number
  portfolio_at_end: number
  depleted_age: number
  annual_contribution: number
  trials: number
  lifetime_taxes: number
}

export interface PlanResult { summary: PlanSummary; years: YearRow[]; profile: Profile }

export interface BudgetItem {
  id?: string
  section: 'income' | 'savings' | 'expense'
  name: string
  expr: string
  amount: number
  growth_rate: number
  account_id: string | null
}

export interface BudgetSummary {
  income: number; savings: number; expenses: number; remainder: number
  savings_pct: number; expenses_pct: number; remainder_pct: number
  annual_income: number; annual_savings: number; annual_expenses: number; annual_remainder: number
  annual_growth: number; annual_income_with_growth: number
}

export interface Budget { month: string; notes: string; updated_at?: string; items: BudgetItem[]; summary?: BudgetSummary }
export interface BudgetMonth { month: string; income: number; savings: number; expenses: number }

export interface FreedomInputs {
  current_age: number
  retirement_age: number
  investable: number
  annual_contribution: number
  contribution_growth: number
  return_rate: number
  inflation: number
  annual_spending: number
  current_expenses: number
  safe_withdrawal_rate: number
}

export interface FreedomPoint {
  age: number; portfolio: number; contributions: number; growth: number; passive_income: number
  coast_number: number; coast_projected: number; fi_number: number
}

export interface FreedomResult {
  inputs: FreedomInputs
  real_return: number
  series: FreedomPoint[]
  crossover: {
    reached: boolean; age: number; years_away: number; portfolio_at_cross: number
    growth_now: number; contrib_now: number; ratio_now: number; portfolio_needed_now: number
  }
  coast: {
    coast_number_today: number; coasting: boolean; progress: number; coast_age: number; years_to_coast: number
    projected_if_stop_now: number; fi_number_at_retirement: number; shortfall: number
    monthly_expenses_to_cover: number; years_to_retirement: number
  }
  freedom: {
    fi_number: number; lean_fi: number; fat_fi: number; progress: number; fi_age: number; years_to_fi: number
    passive_monthly_now: number; freedom_ratio: number; savings_rate: number; monthly_gap: number; fi_by_retirement: boolean
  }
}

export interface Calculation {
  id: string
  kind: 'freedom' | 'plan'
  name: string
  inputs: unknown
  result: any
  created_at: string
}

export type LedgerKind = 'created' | 'updated' | 'edited' | 'deleted'

export interface LedgerEntry {
  id: string
  account_id: string
  account_name: string
  category: Category
  owner: Owner
  kind: LedgerKind
  reason: string
  balance: number // after the event
  asset_value: number
  note: string
  occurred_at: string
  previous: number
  delta: number
  first: boolean // the account's opening balance
}

export interface HistoryPoint {
  date: string // YYYY-MM-DD
  net_worth: number
  assets: number
  liabilities: number
  cash: number
  investments: number
  home: number
  mortgage: number
  credit_cards: number
  debts: number
  net_contributions: number
  growth: number
}

export interface HistorySummary {
  from: string
  to: string
  start_net_worth: number
  end_net_worth: number
  change: number
  change_pct: number
  growth: number
  contributions: number
  liability_change: number
}

export interface History {
  interval: 'day' | 'week' | 'month'
  points: HistoryPoint[]
  summary: HistorySummary
  accounts?: { id: string; name: string; category: Category; owner: Owner; alive: boolean }[]
  series?: Record<string, number[]>
}

export type ValueReason = 'value' | 'deposit' | 'withdrawal' | 'payment' | 'borrowing' | 'correction'

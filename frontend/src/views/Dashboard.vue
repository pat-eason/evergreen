<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import CountUp from '@/components/CountUp.vue'
import DonutChart from '@/components/DonutChart.vue'
import Icon from '@/components/Icon.vue'
import LineChart, { type Series } from '@/components/LineChart.vue'
import ProgressRing from '@/components/ProgressRing.vue'
import Sparkline from '@/components/Sparkline.vue'
import { api } from '@/lib/api'
import { compact, currentMonth, greeting, money, monthLabel, pct } from '@/lib/format'
import { ensurePlan, errMsg, firstName, loadData, position, state, toast } from '@/lib/store'
import type { Budget, BudgetMonth, FreedomResult } from '@/lib/types'

const loading = ref(true)
const freedom = ref<FreedomResult | null>(null)
const budget = ref<Budget | null>(null)
const months = ref<BudgetMonth[]>([])

onMounted(async () => {
  try {
    await loadData()
    const [, f, ms] = await Promise.all([ensurePlan(), api.freedom({}), api.budgetMonths()])
    freedom.value = f
    months.value = ms
    try { budget.value = await api.budget(currentMonth()) } catch { budget.value = null }
  } catch (e) { toast(errMsg(e), 'err') } finally { loading.value = false }
})

watch(() => state.profileRev, async () => {
  try { const [, f] = await Promise.all([ensurePlan(true), api.freedom({})]); freedom.value = f } catch (e) { toast(errMsg(e), 'err') }
})

const plan = computed(() => state.plan)
const hasData = computed(() => state.accounts.length > 0)
const rate = computed(() => plan.value?.summary.success_rate ?? 0)
const shown = computed(() => Math.min(rate.value, 0.99)) // simulation can't prove certainty
const ringColor = computed(() => (rate.value >= 0.75 ? 'var(--accent)' : rate.value >= 0.5 ? 'var(--warn)' : 'var(--danger)'))
const retireAge = computed(() => state.profile?.retirement_age ?? 65)

const series = computed<Series[]>(() => {
  const y = plan.value?.years ?? []
  return [
    { key: 'med', label: 'Median', color: 'var(--c-moss)', area: true, fill: 0.75, points: y.map((r) => ({ x: r.age, y: r.start.total })) },
    { key: 'p10', label: '10th percentile', color: 'var(--c-accent)', area: true, fill: 0.5, points: y.map((r) => ({ x: r.age, y: r.portfolio_p10 })) },
  ]
})

const mix = computed(() => [
  { label: 'Tax-deferred', value: position.value.taxDeferred, color: 'var(--c-rose)' },
  { label: 'Taxable', value: position.value.taxable, color: 'var(--c-accent)' },
  { label: 'Tax-free', value: position.value.taxFree, color: 'var(--c-cream)' },
  { label: 'Cash', value: position.value.cash, color: 'var(--c-mint)' },
])

const bs = computed(() => budget.value?.summary)
const remainders = computed(() => [...months.value].reverse().slice(-8).map((m) => m.income - m.savings - m.expenses))
const topAccounts = computed(() => [...state.accounts].filter((a) => a.category === 'cash' || a.category === 'investment').sort((a, b) => b.config.balance - a.config.balance).slice(0, 4))

const steps = computed(() => [
  { done: hasData.value, label: 'Add your accounts', to: '/accounts', icon: 'wallet' },
  { done: state.configured, label: 'Set your retirement profile', to: '/plan', icon: 'target' },
  { done: months.value.length > 0, label: 'Build this month’s budget', to: '/budget', icon: 'calendar' },
  { done: state.events.length > 0, label: 'Add Social Security or other income', to: '/plan', icon: 'shield' },
])
const showSteps = computed(() => steps.value.some((s) => !s.done))
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div>
        <h1>{{ greeting() }}, {{ firstName }}.</h1>
        <p>Your finances at a glance — what you have, where it's headed, and when you're free.</p>
      </div>
      <RouterLink to="/plan" class="btn accent"><Icon name="chart" :size="17" /> Open planner</RouterLink>
    </div>

    <div class="grid cols-4">
      <div class="glass pad hover rise"><div class="stat-label">Net worth</div><div class="stat-value lg"><CountUp :value="position.netWorth" /></div><div class="small faint">{{ money(position.assets) }} assets · {{ money(position.liabilities) }} owed</div></div>
      <div class="glass pad hover rise" style="--i: 1"><div class="stat-label">Cash &amp; investments</div><div class="stat-value lg pos"><CountUp :value="position.cash + position.investments" /></div><div class="small faint">{{ money(position.monthlyContrib) }}/mo going in</div></div>
      <div class="glass pad hover rise" style="--i: 2">
        <div class="stat-label">This month's remainder</div>
        <div class="stat-value lg" :class="(bs?.remainder ?? 0) >= 0 ? '' : 'neg'"><CountUp :value="bs?.remainder ?? 0" /></div>
        <div class="row"><span class="small faint">{{ bs ? pct(bs.remainder_pct, 0) + ' of income' : 'No budget yet' }}</span><span class="spacer" /><Sparkline v-if="remainders.length > 1" :values="remainders" :width="84" :height="26" /></div>
      </div>
      <div class="glass pad hover rise" style="--i: 3"><div class="stat-label">Debt</div><div class="stat-value lg"><CountUp :value="position.liabilities" /></div><div class="small faint">{{ money(position.monthlyDebtPay) }}/mo in payments</div></div>
    </div>

    <div v-if="showSteps && !loading" class="glass pad steps rise" style="margin-top: 18px; --i: 3">
      <div class="card-head"><h2>Get set up</h2><span class="sub">{{ steps.filter((s) => s.done).length }} of {{ steps.length }} done</span></div>
      <div class="stepgrid">
        <RouterLink v-for="s in steps" :key="s.label" :to="s.to" class="step" :class="{ done: s.done }"><span class="ck"><Icon :name="s.done ? 'check' : s.icon" :size="16" /></span>{{ s.label }}</RouterLink>
      </div>
    </div>

    <div class="grid split" style="margin-top: 18px">
      <section class="glass pad rise" style="--i: 4">
        <div class="card-head"><div><h2>Projected portfolio</h2><span class="sub">median vs. a down market, through age {{ state.profile?.end_age }}</span></div><RouterLink to="/plan" class="small">Details →</RouterLink></div>
        <div v-if="loading || !plan" class="skeleton" style="height: 300px" />
        <LineChart v-else :series="series" :height="300" :vlines="[{ x: retireAge, label: `Retire ${retireAge}` }]" />
      </section>

      <section class="glass pad rise" style="--i: 5">
        <div class="card-head"><h2>Retirement outlook</h2></div>
        <div v-if="plan" class="outlook">
          <ProgressRing :value="rate" :display="shown" :size="168" :stroke="14" :color="ringColor" />
          <p class="lead">You're in <b>{{ plan.summary.verdict }}</b>. We forecast a {{ pct(shown) }} chance your portfolio supports your goals.</p>
          <dl>
            <div><dt>Planned spending</dt><dd class="num">{{ money(plan.summary.planned_monthly) }}/mo</dd></div>
            <div><dt>Sustainable spending</dt><dd class="num pos">{{ money(plan.summary.projected_monthly) }}/mo</dd></div>
            <div><dt>Portfolio at {{ retireAge }}</dt><dd class="num">{{ compact(plan.summary.portfolio_at_retirement) }}</dd></div>
          </dl>
        </div>
        <div v-else class="skeleton" style="height: 300px" />
      </section>
    </div>

    <div class="grid cols-3" style="margin-top: 18px">
      <RouterLink to="/freedom" class="glass hover pad mile rise" style="--i: 6">
        <div class="hl"><Icon name="trend" :size="17" /> Contribution Crossover</div>
        <div v-if="freedom" class="mv">{{ freedom.crossover.reached ? 'Age ' + freedom.crossover.age : '—' }}</div>
        <p class="small muted">{{ freedom?.crossover.reached ? (freedom.crossover.years_away ? `${freedom.crossover.years_away} years away` : 'Already crossed') : 'Growth hasn’t overtaken contributions yet' }}</p>
      </RouterLink>
      <RouterLink to="/freedom" class="glass hover pad mile rise" style="--i: 7">
        <div class="hl"><Icon name="umbrella" :size="17" /> Coast FIRE</div>
        <div v-if="freedom" class="mv">{{ freedom.coast.coasting ? 'Coasting' : freedom.coast.coast_age ? 'Age ' + freedom.coast.coast_age : '—' }}</div>
        <p class="small muted">{{ freedom ? `Need ${compact(freedom.coast.coast_number_today)} today · ${pct(freedom.coast.progress)} there` : '' }}</p>
      </RouterLink>
      <RouterLink to="/freedom" class="glass hover pad mile rise" style="--i: 8">
        <div class="hl"><Icon name="spark" :size="17" /> Financial Freedom</div>
        <div v-if="freedom" class="mv">{{ freedom.freedom.fi_age ? 'Age ' + freedom.freedom.fi_age : '—' }}</div>
        <p class="small muted">{{ freedom ? `FI number ${compact(freedom.freedom.fi_number)} · ${pct(freedom.freedom.progress)} there` : '' }}</p>
      </RouterLink>
    </div>

    <div class="grid cols-2" style="margin-top: 18px">
      <section class="glass pad rise" style="--i: 9">
        <div class="card-head"><h2>Where it's invested</h2><RouterLink to="/accounts" class="small">Manage →</RouterLink></div>
        <div v-if="hasData && position.cash + position.investments > 0" class="mixrow">
          <DonutChart :segments="mix" :size="160" :thickness="20" center-label="Liquid assets" :center-value="compact(position.cash + position.investments)" />
          <div class="tops">
            <div v-for="a in topAccounts" :key="a.id" class="top"><span>{{ a.name }}</span><b class="num">{{ money(a.config.balance) }}</b></div>
          </div>
        </div>
        <div v-else class="empty"><p>Add accounts to see your mix.</p><RouterLink to="/accounts" class="btn sm" style="margin-top: 12px">Add accounts</RouterLink></div>
      </section>
      <section class="glass pad rise" style="--i: 10">
        <div class="card-head"><h2>{{ monthLabel(currentMonth()) }} budget</h2><RouterLink to="/budget" class="small">Open →</RouterLink></div>
        <div v-if="bs" class="bud">
          <div v-for="r in [{ l: 'Income', v: bs.income, p: 1, c: 'var(--c-green)' }, { l: 'Savings', v: bs.savings, p: bs.savings_pct, c: 'var(--c-accent)' }, { l: 'Expenses', v: bs.expenses, p: bs.expenses_pct, c: 'var(--c-rose)' }, { l: 'Remainder', v: bs.remainder, p: Math.max(0, bs.remainder_pct), c: 'var(--c-mint)' }]" :key="r.l" class="b">
            <div class="row"><span>{{ r.l }}</span><span class="spacer" /><b class="num">{{ money(r.v) }}</b></div>
            <i><b :style="{ width: Math.min(100, r.p * 100) + '%', background: r.c }" /></i>
          </div>
        </div>
        <div v-else class="empty"><p>No budget for this month yet.</p><RouterLink to="/budget" class="btn sm" style="margin-top: 12px">Create budget</RouterLink></div>
      </section>
    </div>
  </main>
</template>

<style scoped>
.stepgrid { display: grid; grid-template-columns: repeat(auto-fit, minmax(210px, 1fr)); gap: 10px; }
.step { display: flex; align-items: center; gap: 12px; padding: 12px 14px; border-radius: 16px; background: rgba(0, 0, 0, 0.18); border: 1px solid var(--line); color: var(--text); font-weight: 600; font-size: 14px; transition: border-color 0.2s, transform 0.3s var(--ease); }
.step:hover { text-decoration: none; border-color: var(--accent); transform: translateY(-2px); }
.ck { width: 30px; height: 30px; border-radius: 50%; display: grid; place-items: center; background: rgba(255, 255, 255, 0.08); flex: none; }
.step.done { color: var(--muted); }
.step.done .ck { background: var(--accent); color: var(--accent-ink); }
.outlook { display: grid; justify-items: center; gap: 14px; text-align: center; }
.lead { font-size: 15.5px; }
.outlook dl { width: 100%; margin: 0; display: grid; gap: 2px; }
.outlook dl div { display: flex; justify-content: space-between; padding: 8px 0; border-top: 1px solid rgba(255, 255, 255, 0.07); font-size: 13.5px; }
.outlook dt { color: var(--muted); }
.outlook dd { margin: 0; font-weight: 700; }
.mile { display: grid; gap: 4px; color: var(--text); }
.mile:hover { text-decoration: none; }
.hl { display: flex; align-items: center; gap: 8px; color: var(--accent); font-weight: 700; font-size: 13.5px; }
.mv { font-size: 38px; font-weight: 800; letter-spacing: -0.04em; line-height: 1.15; }
.mixrow { display: flex; align-items: center; gap: 24px; flex-wrap: wrap; }
.tops { flex: 1; min-width: 190px; display: grid; gap: 4px; }
.top { display: flex; justify-content: space-between; padding: 9px 0; border-bottom: 1px solid rgba(255, 255, 255, 0.07); font-size: 14px; }
.top span { color: var(--muted); }
.bud { display: grid; gap: 14px; }
.b .row { font-size: 14px; margin-bottom: 6px; color: var(--muted); }
.b .row b { color: var(--text); }
.b i { display: block; height: 8px; border-radius: 99px; background: rgba(255, 255, 255, 0.08); overflow: hidden; }
.b i b { display: block; height: 100%; border-radius: 99px; transition: width 1s var(--ease); }
</style>

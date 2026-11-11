<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import CountUp from '@/components/CountUp.vue'
import EventForm from '@/components/EventForm.vue'
import Icon from '@/components/Icon.vue'
import LineChart, { type Dot, type Series } from '@/components/LineChart.vue'
import ProfileForm from '@/components/ProfileForm.vue'
import ProgressRing from '@/components/ProgressRing.vue'
import { api } from '@/lib/api'
import { iconFor } from '@/lib/eventTypes'
import { compact, money, pct } from '@/lib/format'
import { effectivePayment, payoffMonths } from '@/lib/loan'
import { ensurePlan, errMsg, invalidatePlan, loadData, position, state, toast } from '@/lib/store'
import type { Calculation, PlanEvent } from '@/lib/types'

const loading = ref(true)
const view = ref<'market' | 'location'>('market')
const tableKind = ref<'cash' | 'balances' | 'taxes'>('cash')
const profileOpen = ref(false)
const assumeOpen = ref(false)
const eventOpen = ref(false)
const eventKind = ref<'income' | 'spending'>('income')
const editingEvent = ref<PlanEvent | null>(null)
const saved = ref<Calculation[]>([])

async function boot() {
  try {
    await loadData()
    await ensurePlan()
    saved.value = await api.calculations('plan')
  } catch (e) { toast(errMsg(e), 'err') } finally { loading.value = false }
}
watch(() => state.profileRev, () => { if (!state.plan) refresh() })
onMounted(boot)

const plan = computed(() => state.plan)
const years = computed(() => plan.value?.years ?? [])
const retireAge = computed(() => state.profile?.retirement_age ?? 65)

const series = computed<Series[]>(() => {
  const y = years.value
  if (view.value === 'market') {
    return [
      { key: 'med', label: 'Median', color: 'var(--c-moss)', points: y.map((r) => ({ x: r.age, y: r.start.total })), area: true, fill: 0.7 },
      { key: 'p10', label: '10th percentile', color: 'var(--c-accent)', points: y.map((r) => ({ x: r.age, y: r.portfolio_p10 })), area: true, fill: 0.5 },
    ]
  }
  const cum = (f: (r: (typeof y)[number]) => number) => y.map((r) => ({ x: r.age, y: f(r) }))
  return [
    { key: 'tf', label: 'Tax-free', color: 'var(--c-cream)', points: cum((r) => r.start.total), area: true, fill: 0.75 },
    { key: 'td', label: 'Tax-deferred', color: 'var(--c-rose)', points: cum((r) => r.start.total - r.start.tax_free), area: true, fill: 0.8 },
    { key: 'tx', label: 'Taxable', color: 'var(--c-accent)', points: cum((r) => r.start.cash + r.start.taxable), area: true, fill: 0.85 },
    { key: 'cs', label: 'Cash', color: 'var(--c-mint)', points: cum((r) => r.start.cash), area: true, fill: 0.9 },
  ]
})
const dots = computed<Dot[]>(() => {
  const byAge = new Map(years.value.map((r) => [r.age, r.start.total]))
  return state.events.filter((e) => byAge.has(e.start_age)).map((e) => ({
    x: e.start_age, y: byAge.get(e.start_age)!, label: e.name, color: e.kind === 'income' ? 'var(--c-green)' : 'var(--c-rose)',
  }))
})

const incomeEvents = computed(() => state.events.filter((e) => e.kind === 'income'))
const spendEvents = computed(() => state.events.filter((e) => e.kind === 'spending'))
const liabs = computed(() => state.accounts.filter((a) => ['mortgage', 'credit_card', 'debt'].includes(a.category) && a.config.balance > 0 && !(a.category === 'credit_card' && a.config.pay_in_full)))

function describe(e: PlanEvent): string {
  const dur = e.years === 0 ? 'for life' : `for ${e.years} year${e.years > 1 ? 's' : ''}`
  return `Age: ${e.start_age} | ${money(e.annual_amount)}/yr ${dur}`
}
function describeLiab(a: (typeof liabs.value)[number]): string {
  const m = payoffMonths(a)
  return `${money(effectivePayment(a) * 12)}/yr${m ? ` for ${m >= 24 ? Math.round(m / 12) + ' years' : m + ' months'}` : ''}`
}

function addEvent(kind: 'income' | 'spending') { editingEvent.value = null; eventKind.value = kind; eventOpen.value = true }
function editEvent(e: PlanEvent) { editingEvent.value = e; eventKind.value = e.kind; eventOpen.value = true }
async function removeEvent(e: PlanEvent) {
  if (!confirm(`Delete “${e.name}”?`)) return
  try {
    await api.deleteEvent(e.id)
    state.events = state.events.filter((x) => x.id !== e.id)
    invalidatePlan()
    await ensurePlan()
  } catch (err) { toast(errMsg(err), 'err') }
}
async function refresh() {
  invalidatePlan()
  loading.value = true
  try { await ensurePlan() } catch (e) { toast(errMsg(e), 'err') } finally { loading.value = false }
}
// Re-run the plan when a drawer closes after editing.
async function afterEdit() { if (!state.plan) await refresh() }

async function snapshot() {
  const name = prompt('Name this snapshot', `Plan ${new Date().toLocaleDateString()}`)
  if (!name || !plan.value) return
  try {
    const { years: _y, ...lite } = plan.value
    const c = await api.saveCalculation({ kind: 'plan', name, inputs: plan.value.profile, result: lite })
    saved.value.unshift(c)
    toast('Snapshot saved')
  } catch (e) { toast(errMsg(e), 'err') }
}
async function delSnapshot(c: Calculation) {
  try { await api.deleteCalculation(c.id); saved.value = saved.value.filter((x) => x.id !== c.id) } catch (e) { toast(errMsg(e), 'err') }
}

const rate = computed(() => plan.value?.summary.success_rate ?? 0)
// Simulation can't prove certainty, so never show 100%.
const shown = computed(() => Math.min(rate.value, 0.99))
const ringColor = computed(() => (rate.value >= 0.75 ? 'var(--accent)' : rate.value >= 0.5 ? 'var(--warn)' : 'var(--danger)'))
const ability = computed(() => {
  const s = plan.value?.summary
  if (!s) return { planned: 0, projected: 0, pw: 0, jw: 0 }
  const max = Math.max(s.planned_monthly, s.projected_monthly, 1)
  return { planned: s.planned_monthly, projected: s.projected_monthly, pw: (s.planned_monthly / max) * 100, jw: (s.projected_monthly / max) * 100 }
})
const flow = (v: number) => (v === 0 ? '–' : (v < 0 ? '-' : '') + money(Math.abs(v)))
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div>
        <h1>Retirement planner</h1>
        <p>A Monte Carlo projection of your accounts, income events and spending goals through age {{ state.profile?.end_age ?? 90 }}.</p>
      </div>
      <div class="row wrap">
        <button class="btn" @click="assumeOpen = true"><Icon name="sliders" :size="17" /> Edit assumptions</button>
        <button class="btn" @click="profileOpen = true"><Icon name="edit" :size="17" /> Edit profile</button>
        <button class="btn accent" :disabled="!plan" @click="snapshot"><Icon name="save" :size="17" /> Save snapshot</button>
      </div>
    </div>

    <div v-if="!state.configured && !loading" class="glass pad notice rise">
      <Icon name="info" /> <span>Using default assumptions — <a href="#" @click.prevent="profileOpen = true">set your age, retirement age and spending</a> for an accurate plan.</span>
    </div>

    <section class="glass pad rise">
      <div class="card-head">
        <div class="row"><h2>Projected portfolio value</h2><span class="sub">{{ plan ? `${plan.summary.trials.toLocaleString()} simulations` : '' }}</span></div>
        <div class="tabs"><button class="tab" :class="{ on: view === 'market' }" @click="view = 'market'">Market conditions</button><button class="tab" :class="{ on: view === 'location' }" @click="view = 'location'">Asset location</button></div>
      </div>
      <div v-if="loading || !plan" class="skeleton" style="height: 340px" />
      <LineChart v-else :key="view" :series="series" :height="340" :dots="dots" :vlines="[{ x: retireAge, label: `Retirement age ${retireAge}` }]" :x-format="(v: number) => (v === state.profile?.current_age ? `Age ${v}` : `${v}`)" />
      <div class="legend" style="margin-top: 10px">
        <span v-for="s in series" :key="s.key"><i class="dot" :style="{ background: s.color }" />{{ s.label }}</span>
        <span><i class="dot" style="background: var(--c-green)" />Income event</span><span><i class="dot" style="background: var(--c-rose)" />Spending goal</span>
      </div>
    </section>

    <div class="grid cols-2" style="margin-top: 18px">
      <section class="glass pad rise" style="--i: 1">
        <div v-if="plan" class="shape">
          <ProgressRing :value="rate" :display="shown" :size="150" :color="ringColor" />
          <div>
            <p class="lead">You're in <b class="shape-word">{{ plan.summary.verdict }}</b> for retirement. We forecast a {{ pct(shown) }} chance your portfolio will support your goals.</p>
            <p class="small muted" style="margin-top: 8px">Median portfolio at retirement: <b class="text">{{ compact(plan.summary.portfolio_at_retirement) }}</b> · in a down market (10th pct.): <b class="text">{{ compact(plan.summary.portfolio_at_retirement_p10) }}</b></p>
            <p v-if="plan.summary.depleted_age" class="small neg" style="margin-top: 6px">On a median-market path, money runs out at age {{ plan.summary.depleted_age }}.</p>
          </div>
        </div>
        <div v-else class="skeleton" style="height: 150px" />
      </section>
      <section class="glass pad rise" style="--i: 2">
        <div class="card-head"><h2>Retirement spending ability</h2><span class="sub">per month, today's dollars</span></div>
        <div v-if="plan" class="ability">
          <div class="ab"><div class="lab"><span class="pos">PROJECTED</span><b class="num pos"><CountUp :value="ability.projected" />/mo</b></div><i><b class="p" :style="{ width: ability.jw + '%' }" /></i></div>
          <div class="ab"><div class="lab"><span>PLANNED</span><b class="num"><CountUp :value="ability.planned" />/mo</b></div><i><b class="q" :style="{ width: ability.pw + '%' }" /></i></div>
          <p class="small muted">Projected is the most you could spend on retirement living costs while keeping a 90% chance of success, after your other goals and debts.</p>
        </div>
        <div v-else class="skeleton" style="height: 120px" />
      </section>
    </div>

    <div class="grid cols-2" style="margin-top: 18px">
      <section class="glass pad rise" style="--i: 3">
        <div class="card-head"><h2>Income events</h2><button class="btn icon sm" aria-label="Add income event" @click="addEvent('income')"><Icon name="plus" :size="17" /></button></div>
        <div class="evlist">
          <div class="ev auto"><div class="eico inc"><Icon name="piggy" :size="20" /></div><div><h3>Savings</h3><p>{{ money(position.cash + position.investments) }} today | {{ money(position.monthlyContrib * 12) }}/yr</p></div><span class="chip">from Accounts</span></div>
          <TransitionGroup name="list">
            <div v-for="e in incomeEvents" :key="e.id" class="ev">
              <div class="eico inc"><Icon :name="iconFor(e.kind, e.type)" :size="20" /></div>
              <div><h3>{{ e.name }}</h3><p>{{ describe(e) }}</p></div>
              <div class="eact"><button class="btn icon sm" aria-label="Edit" @click="editEvent(e)"><Icon name="edit" :size="15" /></button><button class="btn icon sm danger" aria-label="Delete" @click="removeEvent(e)"><Icon name="trash" :size="15" /></button></div>
            </div>
          </TransitionGroup>
          <p v-if="!incomeEvents.length" class="small faint">Add Social Security, a pension or part-time work.</p>
        </div>
      </section>
      <section class="glass pad rise" style="--i: 4">
        <div class="card-head"><h2>Spending goals</h2><button class="btn icon sm" aria-label="Add spending goal" @click="addEvent('spending')"><Icon name="plus" :size="17" /></button></div>
        <div class="evlist">
          <div class="ev auto"><div class="eico spd"><Icon name="palm" :size="20" /></div><div><h3>Retirement spending</h3><p>Age: {{ retireAge }} | {{ money(state.profile?.retirement_spending ?? 0) }}/yr</p></div><span class="chip">from Profile</span></div>
          <div v-for="a in liabs" :key="a.id" class="ev auto"><div class="eico spd"><Icon :name="a.category === 'mortgage' ? 'house' : a.category === 'credit_card' ? 'card' : 'debt'" :size="20" /></div><div><h3>{{ a.name }}</h3><p>{{ describeLiab(a) }}</p></div><span class="chip">from Accounts</span></div>
          <TransitionGroup name="list">
            <div v-for="e in spendEvents" :key="e.id" class="ev">
              <div class="eico spd"><Icon :name="iconFor(e.kind, e.type)" :size="20" /></div>
              <div><h3>{{ e.name }}</h3><p>{{ describe(e) }}</p></div>
              <div class="eact"><button class="btn icon sm" aria-label="Edit" @click="editEvent(e)"><Icon name="edit" :size="15" /></button><button class="btn icon sm danger" aria-label="Delete" @click="removeEvent(e)"><Icon name="trash" :size="15" /></button></div>
            </div>
          </TransitionGroup>
        </div>
      </section>
    </div>

    <section class="glass pad rise" style="margin-top: 18px; --i: 5">
      <div class="card-head">
        <select v-model="tableKind" class="input sel" aria-label="Table view"><option value="cash">Detailed cash flow table</option><option value="balances">Portfolio by account type</option><option value="taxes">Estimated taxes</option></select>
        <span class="sub">Before retirement, expenses are paid from your paycheck; only savings flow into the portfolio.</span>
      </div>
      <div v-if="!plan" class="skeleton" style="height: 300px" />
      <div v-else class="table-wrap tall">
        <table v-if="tableKind === 'cash'" class="t">
          <thead><tr><th>Age</th><th>Starting portfolio (median)</th><th>Starting portfolio (10th pct.)</th><th>Basic saving or retirement spending</th><th>Other spending goals</th><th>Other income events</th><th>Taxes</th><th>Cash flow</th></tr></thead>
          <tbody>
            <tr v-for="r in years" :key="r.age" :class="{ retire: r.age === retireAge }">
              <td>{{ r.age }}{{ r.age === retireAge ? ' (Retire)' : '' }}</td><td>{{ money(r.start.total) }}</td><td>{{ money(r.portfolio_p10) }}</td>
              <td :class="r.retired ? '' : 'pos'">{{ r.retired ? flow(-r.retirement_spending) : flow(r.saving) }}</td><td>{{ flow(-r.other_spending) }}</td><td>{{ flow(r.other_income) }}</td><td>{{ flow(-r.taxes.total) }}</td>
              <td :class="r.cash_flow < 0 ? 'neg' : ''">{{ flow(r.cash_flow) }}</td>
            </tr>
          </tbody>
        </table>
        <table v-else-if="tableKind === 'balances'" class="t">
          <thead><tr><th>Age</th><th>Cash</th><th>Taxable</th><th>Tax-deferred</th><th>Tax-free</th><th>Total</th></tr></thead>
          <tbody><tr v-for="r in years" :key="r.age" :class="{ retire: r.age === retireAge }"><td>{{ r.age }}{{ r.age === retireAge ? ' (Retire)' : '' }}</td><td>{{ money(r.start.cash) }}</td><td>{{ money(r.start.taxable) }}</td><td>{{ money(r.start.tax_deferred) }}</td><td>{{ money(r.start.tax_free) }}</td><td><b>{{ money(r.start.total) }}</b></td></tr></tbody>
        </table>
        <table v-else class="t">
          <thead><tr><th>Age</th><th>Federal income</th><th>Gains &amp; dividends</th><th>State</th><th>Total</th><th>Marginal rate</th></tr></thead>
          <tbody><tr v-for="r in years" :key="r.age" :class="{ retire: r.age === retireAge }"><td>{{ r.age }}{{ r.age === retireAge ? ' (Retire)' : '' }}</td><td>{{ money(r.taxes.federal) }}</td><td>{{ money(r.taxes.gains) }}</td><td>{{ money(r.taxes.state) }}</td><td><b>{{ money(r.taxes.total) }}</b></td><td>{{ pct(r.taxes.marginal_rate) }}</td></tr></tbody>
        </table>
      </div>
    </section>

    <section v-if="saved.length" class="glass pad rise" style="margin-top: 18px">
      <div class="card-head"><h2><Icon name="history" :size="18" /> Saved snapshots</h2></div>
      <div class="snaps">
        <div v-for="c in saved" :key="c.id" class="snap">
          <div><h3>{{ c.name }}</h3><small class="muted">{{ new Date(c.created_at).toLocaleString() }}</small></div>
          <span class="chip good">{{ pct(c.result?.summary?.success_rate ?? 0) }} success</span>
          <span class="chip">{{ compact(c.result?.summary?.portfolio_at_retirement ?? 0) }} at retirement</span>
          <button class="btn icon sm danger" aria-label="Delete snapshot" @click="delSnapshot(c)"><Icon name="trash" :size="15" /></button>
        </div>
      </div>
    </section>

    <ProfileForm :open="profileOpen" mode="profile" @close="profileOpen = false; afterEdit()" />
    <ProfileForm :open="assumeOpen" mode="assumptions" @close="assumeOpen = false; afterEdit()" />
    <EventForm :open="eventOpen" :kind="eventKind" :event="editingEvent" @close="eventOpen = false; afterEdit()" />
  </main>
</template>

<style scoped>
.notice { display: flex; align-items: center; gap: 12px; margin-bottom: 18px; border-color: rgba(205, 247, 61, 0.35); }
.notice svg { color: var(--accent); flex: none; }
.shape { display: flex; align-items: center; gap: 24px; }
.lead { font-size: 17px; line-height: 1.45; }
.shape-word { border-bottom: 2px dotted var(--accent); }
.text { color: var(--text); }
.ability { display: grid; gap: 16px; }
.ab .lab { display: flex; justify-content: space-between; font-size: 13px; color: var(--muted); font-weight: 600; margin-bottom: 7px; }
.ab .lab b { font-size: 16px; }
.ab i { display: block; height: 7px; border-radius: 99px; background: rgba(255, 255, 255, 0.08); overflow: hidden; }
.ab b.p, .ab b.q { display: block; height: 100%; border-radius: 99px; transition: width 1.2s var(--ease); }
.ab b.p { background: var(--accent); box-shadow: 0 0 14px var(--accent-glow); }
.ab b.q { background: var(--c-cream); opacity: 0.8; }
.evlist { display: grid; gap: 12px; }
.ev { display: flex; align-items: center; gap: 16px; padding: 14px 16px; border-radius: 18px; border: 1px solid var(--line); background: rgba(0, 0, 0, 0.16); transition: border-color 0.25s, transform 0.3s var(--ease); }
.ev:hover { border-color: rgba(255, 255, 255, 0.25); transform: translateX(2px); }
.ev h3 { font-size: 16px; }
.ev p { color: var(--muted); font-size: 13px; margin-top: 1px; }
.ev > .chip, .eact { margin-left: auto; flex: none; }
.eact { display: flex; gap: 4px; opacity: 0.55; transition: opacity 0.2s; }
.ev:hover .eact { opacity: 1; }
.eico { width: 46px; height: 46px; border-radius: 50%; display: grid; place-items: center; flex: none; }
.eico.inc { background: var(--c-moss); color: #eaffd0; }
.eico.spd { background: var(--c-wine); color: #ffe3e5; }
.sel { width: auto; min-width: 250px; height: 42px; font-weight: 600; }
.tall { max-height: 540px; }
.list-enter-active, .list-leave-active { transition: all 0.4s var(--ease); }
.list-enter-from, .list-leave-to { opacity: 0; transform: translateX(-12px); }
.snaps { display: grid; gap: 10px; }
.snap { display: flex; align-items: center; gap: 12px; padding: 12px 16px; border-radius: 16px; background: rgba(0, 0, 0, 0.16); border: 1px solid var(--line); }
.snap > div { flex: 1; min-width: 0; }
@media (max-width: 680px) { .shape { flex-direction: column; text-align: center; } .sel { min-width: 0; width: 100%; } }
</style>

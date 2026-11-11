<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AccountForm from '@/components/AccountForm.vue'
import CountUp from '@/components/CountUp.vue'
import DonutChart from '@/components/DonutChart.vue'
import Icon from '@/components/Icon.vue'
import { api } from '@/lib/api'
import { money, pct } from '@/lib/format'
import { durationLabel, effectivePayment, payoffMonths } from '@/lib/loan'
import { errMsg, invalidatePlan, loadData, position, state, toast } from '@/lib/store'
import type { Account, Category } from '@/lib/types'

const loading = ref(true)
const filter = ref<'all' | Category>('all')
const formOpen = ref(false)
const editing = ref<Account | null>(null)
const newCat = ref<Category>('cash')

onMounted(async () => {
  try { await loadData() } catch (e) { toast(errMsg(e), 'err') } finally { loading.value = false }
})

const meta: Record<Category, { label: string; plural: string; icon: string; color: string }> = {
  cash: { label: 'Cash', plural: 'Cash', icon: 'bank', color: 'var(--c-mint)' },
  investment: { label: 'Investment', plural: 'Investments', icon: 'trend', color: 'var(--c-accent)' },
  mortgage: { label: 'Mortgage', plural: 'Mortgage', icon: 'house', color: 'var(--c-rose)' },
  credit_card: { label: 'Credit card', plural: 'Credit cards', icon: 'card', color: 'var(--c-wine)' },
  debt: { label: 'Debt', plural: 'Debts', icon: 'debt', color: 'var(--c-cream)' },
}
const tabs = computed(() => [
  { id: 'all' as const, label: 'All', n: state.accounts.length },
  ...(Object.keys(meta) as Category[]).map((id) => ({ id, label: meta[id].plural, n: state.accounts.filter((a) => a.category === id).length })),
])
const shown = computed(() => state.accounts.filter((a) => filter.value === 'all' || a.category === filter.value))

const treatment = { taxable: 'Taxable', tax_deferred: 'Tax-deferred', tax_free: 'Tax-free' } as const

function details(a: Account): string[] {
  const c = a.config
  switch (a.category) {
    case 'cash': return [`${pct(c.apy ?? 0, 2)} APY`, c.monthly_contribution ? `+${money(c.monthly_contribution)}/mo` : '']
    case 'investment': return [treatment[c.tax_treatment ?? 'taxable'], c.monthly_contribution || c.employer_match ? `+${money((c.monthly_contribution ?? 0) + (c.employer_match ?? 0))}/mo` : '', c.expected_return ? `${pct(c.expected_return, 1)} return` : '']
    case 'mortgage': return [`${pct(c.interest_rate ?? 0, 2)}`, `${money(effectivePayment(a))}/mo`, `payoff ${durationLabel(payoffMonths(a))}`]
    case 'credit_card': return [`${pct(c.apr ?? 0, 1)} APR`, c.pay_in_full ? 'paid in full monthly' : `${money(effectivePayment(a))}/mo`, c.credit_limit ? `${pct(c.balance / c.credit_limit)} used` : '']
    case 'debt': return [c.debt_kind ?? '', `${pct(c.interest_rate ?? 0, 2)}`, `${money(effectivePayment(a))}/mo`, `payoff ${durationLabel(payoffMonths(a))}`]
  }
}

function add(cat: Category = 'cash') { editing.value = null; newCat.value = cat; formOpen.value = true }
function edit(a: Account) { editing.value = a; formOpen.value = true }
async function remove(a: Account) {
  if (!confirm(`Delete “${a.name}”? This can't be undone.`)) return
  try {
    await api.deleteAccount(a.id)
    state.accounts = state.accounts.filter((x) => x.id !== a.id)
    invalidatePlan()
    toast('Account deleted')
  } catch (e) { toast(errMsg(e), 'err') }
}

const mix = computed(() => [
  { label: 'Tax-deferred', value: position.value.taxDeferred, color: 'var(--c-rose)' },
  { label: 'Taxable', value: position.value.taxable, color: 'var(--c-accent)' },
  { label: 'Tax-free', value: position.value.taxFree, color: 'var(--c-cream)' },
  { label: 'Cash', value: position.value.cash, color: 'var(--c-mint)' },
])
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div>
        <h1>Accounts</h1>
        <p>Record what you own and owe. Each type has its own settings that feed the retirement and freedom projections.</p>
      </div>
      <button class="btn accent" @click="add()"><Icon name="plus" :size="18" /> Add account</button>
    </div>

    <div class="grid cols-4">
      <div class="glass pad rise"><div class="stat-label">Net worth</div><div class="stat-value"><CountUp :value="position.netWorth" /></div><div class="small faint">incl. home equity</div></div>
      <div class="glass pad rise" style="--i: 1"><div class="stat-label">Liquid &amp; invested</div><div class="stat-value pos"><CountUp :value="position.cash + position.investments" /></div><div class="small faint">cash + investments</div></div>
      <div class="glass pad rise" style="--i: 2"><div class="stat-label">Liabilities</div><div class="stat-value neg"><CountUp :value="position.liabilities" /></div><div class="small faint">{{ money(position.monthlyDebtPay) }}/mo in payments</div></div>
      <div class="glass pad rise" style="--i: 3"><div class="stat-label">Saving each month</div><div class="stat-value"><CountUp :value="position.monthlyContrib" /></div><div class="small faint">{{ money(position.monthlyContrib * 12) }}/yr into accounts</div></div>
    </div>

    <div class="tabs filter" role="tablist" style="margin: 26px 0 18px">
      <button v-for="t in tabs" :key="t.id" class="tab" :class="{ on: filter === t.id }" role="tab" @click="filter = t.id">{{ t.label }} <span class="faint">{{ t.n }}</span></button>
    </div>

    <div v-if="loading" class="grid cols-3"><div v-for="i in 3" :key="i" class="skeleton" style="height: 150px" /></div>

    <div v-else-if="!state.accounts.length" class="glass empty rise">
      <h3>Add your first account</h3>
      <p>Start with the accounts that matter most for your plan.</p>
      <div class="quick">
        <button v-for="(m, id) in meta" :key="id" class="btn" @click="add(id)"><Icon :name="m.icon" :size="17" /> {{ m.label }}</button>
      </div>
    </div>

    <div v-else class="split2">
      <TransitionGroup name="list" tag="div" class="grid cols-2 cards">
        <article v-for="(a, i) in shown" :key="a.id" class="glass hover acct" :style="{ '--i': i, '--c': meta[a.category].color }">
          <div class="ico"><Icon :name="meta[a.category].icon" :size="20" /></div>
          <div class="main">
            <div class="cat-label">{{ meta[a.category].label }}</div>
            <h3>{{ a.name }}</h3>
            <div class="bal num" :class="{ neg: !['cash', 'investment'].includes(a.category) }">{{ !['cash', 'investment'].includes(a.category) ? '−' : '' }}{{ money(a.config.balance) }}</div>
            <div class="chips"><span v-for="d in details(a).filter(Boolean)" :key="d" class="chip">{{ d }}</span></div>
          </div>
          <div class="actions">
            <button class="btn icon sm" aria-label="Edit" @click="edit(a)"><Icon name="edit" :size="16" /></button>
            <button class="btn icon sm danger" aria-label="Delete" @click="remove(a)"><Icon name="trash" :size="16" /></button>
          </div>
        </article>
      </TransitionGroup>

      <aside class="glass pad mixcard rise" style="--i: 2">
        <div class="card-head"><h2>Portfolio mix</h2><span class="sub">by tax treatment</span></div>
        <div v-if="position.cash + position.investments > 0" class="mixrow">
          <DonutChart :segments="mix" :size="170" :thickness="20" center-label="Liquid assets" :center-value="money(position.cash + position.investments)" />
          <div class="legend col">
            <span v-for="s in mix" :key="s.label"><i class="dot" :style="{ background: s.color }" />{{ s.label }} <b class="num">{{ money(s.value) }}</b></span>
          </div>
        </div>
        <p v-else class="muted small">Add cash or investment accounts to see your mix.</p>
        <p class="small faint" style="margin-top: 14px">Withdrawal order and taxes in retirement depend on this mix — see Smart Withdrawal.</p>
      </aside>
    </div>

    <AccountForm :open="formOpen" :account="editing" :initial-category="newCat" @close="formOpen = false" />
  </main>
</template>

<style scoped>
.filter { max-width: 100%; overflow-x: auto; }
.split2 { display: grid; grid-template-columns: minmax(0, 1fr) 320px; gap: 18px; align-items: start; }
.cards { gap: 16px; }
.acct { display: flex; gap: 16px; padding: 20px; animation: rise 0.6s var(--ease) both; animation-delay: calc(var(--i) * 60ms); }
.ico { width: 46px; height: 46px; border-radius: 15px; display: grid; place-items: center; flex: none; color: var(--c); background: color-mix(in srgb, var(--c) 16%, transparent); border: 1px solid color-mix(in srgb, var(--c) 35%, transparent); }
.main { flex: 1; min-width: 0; }
.cat-label { font-size: 11.5px; font-weight: 700; letter-spacing: 0.06em; text-transform: uppercase; color: var(--c); }
.main h3 { font-size: 16px; margin: 2px 0 4px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bal { font-size: 24px; font-weight: 700; letter-spacing: -0.03em; margin-bottom: 10px; }
.chips { display: flex; flex-wrap: wrap; gap: 6px; }
.actions { display: flex; flex-direction: column; gap: 6px; opacity: 0.6; transition: opacity 0.2s; }
.acct:hover .actions, .acct:focus-within .actions { opacity: 1; }
.quick { display: flex; flex-wrap: wrap; gap: 10px; justify-content: center; margin-top: 18px; }
.mixrow { display: grid; gap: 18px; justify-items: center; }
.legend.col { display: grid; gap: 8px; width: 100%; }
.legend.col span b { margin-left: auto; color: var(--text); }
.legend.col span { width: 100%; }
.list-enter-active, .list-leave-active { transition: all 0.45s var(--ease); }
.list-enter-from, .list-leave-to { opacity: 0; transform: scale(0.94); }
.list-leave-active { position: absolute; }
@media (max-width: 1060px) { .split2 { grid-template-columns: 1fr; } }
</style>

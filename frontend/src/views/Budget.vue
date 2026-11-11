<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import CountUp from '@/components/CountUp.vue'
import DonutChart from '@/components/DonutChart.vue'
import Icon from '@/components/Icon.vue'
import { api, ApiError } from '@/lib/api'
import { evalExpr } from '@/lib/expr'
import { currentMonth, money, money2, monthLabel, pct, shiftMonth } from '@/lib/format'
import { errMsg, invalidatePlan, loadData, state, toast } from '@/lib/store'
import type { BudgetItem, BudgetMonth } from '@/lib/types'

type Section = 'income' | 'savings' | 'expense'
interface Row { key: number; section: Section; name: string; text: string; growth: number; account_id: number | null }

const month = ref(currentMonth())
const rows = ref<Row[]>([])
const exists = ref(false)
const loading = ref(true)
const saveState = ref<'idle' | 'saving' | 'saved' | 'error'>('idle')
const months = ref<BudgetMonth[]>([])
let keySeq = 1
let timer: ReturnType<typeof setTimeout> | undefined

const sections: { id: Section; title: string; hint: string; color: string; add: string }[] = [
  { id: 'income', title: 'Income', hint: 'Take-home pay & other income', color: 'var(--c-green)', add: 'Add income' },
  { id: 'savings', title: 'Savings', hint: 'Contributions & expected growth', color: 'var(--c-accent)', add: 'Add savings' },
  { id: 'expense', title: 'Expenses', hint: 'Bills, housing & living costs', color: 'var(--c-rose)', add: 'Add expense' },
]

// Starter template mirrors the structure of the household budget spreadsheet.
const TEMPLATE: { section: Section; name: string; text: string; growth?: number }[] = [
  { section: 'income', name: 'Paycheck 1', text: '=(1368.23*2)+(3192.56*2)+(994*2)' },
  { section: 'income', name: 'Paycheck 2', text: '=(2573.42*2)+(593.75*2)' },
  { section: 'savings', name: '401(k)', text: '=994*2', growth: 0.07 },
  { section: 'savings', name: 'IRA', text: '=593.75*2', growth: 0.07 },
  { section: 'savings', name: 'Roth IRA', text: '=500*2', growth: 0.07 },
  { section: 'savings', name: 'Brokerage', text: '2000', growth: 0.07 },
  { section: 'savings', name: 'High-yield savings', text: '=(1250*2)+500', growth: 0.0356 },
  ...[['Cable', 130], ['Streaming', 19.25], ['Water', 40], ['Trash', 20], ['Electric', 200], ['Mortgage', 2319.14], ['Groceries', 800], ['Gas', 40], ['Music', 20], ['Games', 14], ['Pest control', 50], ['HVAC service', 20.28], ['Home security', 33]].map(
    ([name, amt]) => ({ section: 'expense' as Section, name: name as string, text: String(amt) }),
  ),
]

const isPlain = (t: string) => /^\$?[\d,]*\.?\d*$/.test(t.trim())
const rowAmount = (r: Row) => (r.text.trim() === '' ? 0 : evalExpr(r.text))
const invalid = (r: Row) => r.text.trim() !== '' && rowAmount(r) === null

const bySection = (s: Section) => rows.value.filter((r) => r.section === s)
const total = (s: Section) => bySection(s).reduce((a, r) => a + (rowAmount(r) ?? 0), 0)

const sum = computed(() => {
  const income = total('income'), savings = total('savings'), expenses = total('expense')
  const remainder = income - savings - expenses
  const growth = bySection('savings').reduce((a, r) => a + (rowAmount(r) ?? 0) * 12 * r.growth, 0)
  return {
    income, savings, expenses, remainder, growth,
    p: (v: number) => (income > 0 ? v / income : 0),
    annualIncome: income * 12, annualSavings: savings * 12, annualExpenses: expenses * 12, annualRemainder: remainder * 12,
  }
})

const donut = computed(() => [
  { label: 'Savings', value: Math.max(0, sum.value.savings), color: 'var(--c-accent)' },
  { label: 'Expenses', value: Math.max(0, sum.value.expenses), color: 'var(--c-rose)' },
  { label: 'Remainder', value: Math.max(0, sum.value.remainder), color: 'var(--c-mint)' },
])
const topExpenses = computed(() => {
  const list = bySection('expense').map((r) => ({ name: r.name || 'Untitled', v: rowAmount(r) ?? 0 })).sort((a, b) => b.v - a.v).slice(0, 6)
  const max = Math.max(1, ...list.map((x) => x.v))
  return list.map((x) => ({ ...x, w: (x.v / max) * 100 }))
})
const linkable = computed(() => state.accounts.filter((a) => a.category === 'cash' || a.category === 'investment'))

function toRows(items: BudgetItem[]): Row[] {
  return items.map((i) => ({ key: keySeq++, section: i.section, name: i.name, text: i.expr || String(i.amount), growth: i.growth_rate, account_id: i.account_id }))
}

async function load() {
  loading.value = true
  clearTimeout(timer)
  saveState.value = 'idle'
  try {
    const b = await api.budget(month.value)
    rows.value = toRows(b.items)
    exists.value = true
  } catch (e) {
    rows.value = []
    exists.value = false
    if (!(e instanceof ApiError && e.status === 404)) toast(errMsg(e), 'err')
  } finally {
    loading.value = false
  }
}
async function loadMonths() {
  try { months.value = await api.budgetMonths() } catch { /* non-fatal */ }
}

onMounted(async () => {
  try { await loadData() } catch { /* shown elsewhere */ }
  await Promise.all([load(), loadMonths()])
})
watch(month, load)
onBeforeUnmount(() => { if (timer) { clearTimeout(timer); void flush() } })

function touch() {
  saveState.value = 'idle'
  clearTimeout(timer)
  timer = setTimeout(flush, 700)
}
async function flush() {
  clearTimeout(timer)
  timer = undefined
  if (!exists.value || rows.value.some((r) => invalid(r))) return
  saveState.value = 'saving'
  const items = rows.value.filter((r) => r.name.trim()).map((r) => ({
    section: r.section, name: r.name.trim(), expr: isPlain(r.text) ? '' : r.text, amount: rowAmount(r) ?? 0,
    growth_rate: r.section === 'savings' ? r.growth : 0, account_id: r.section === 'savings' ? r.account_id : null,
  }))
  try {
    await api.saveBudget(month.value, { notes: '', items })
    saveState.value = 'saved'
    loadMonths()
  } catch (e) {
    saveState.value = 'error'
    toast(errMsg(e), 'err')
  }
}

function addRow(section: Section) {
  rows.value.push({ key: keySeq++, section, name: '', text: '', growth: section === 'savings' ? 0.07 : 0, account_id: null })
  touch()
}
function removeRow(r: Row) { rows.value = rows.value.filter((x) => x !== r); touch() }

async function create(kind: 'blank' | 'template' | 'copy') {
  try {
    if (kind === 'copy') {
      const prev = months.value.find((m) => m.month < month.value)
      if (!prev) return
      rows.value = toRows((await api.copyBudget(month.value, prev.month)).items)
    } else {
      const items = kind === 'template' ? TEMPLATE.map((t) => ({ section: t.section, name: t.name, expr: isPlain(t.text) ? '' : t.text, amount: evalExpr(t.text) ?? 0, growth_rate: t.growth ?? 0, account_id: null })) : []
      rows.value = toRows((await api.saveBudget(month.value, { notes: '', items })).items)
    }
    exists.value = true
    loadMonths()
  } catch (e) { toast(errMsg(e), 'err') }
}

async function removeMonth() {
  if (!confirm(`Delete the ${monthLabel(month.value)} budget?`)) return
  try {
    await api.deleteBudget(month.value)
    exists.value = false
    rows.value = []
    loadMonths()
    toast('Budget deleted')
  } catch (e) { toast(errMsg(e), 'err') }
}

const applying = ref(false)
async function apply() {
  await flush()
  applying.value = true
  try {
    const r = await api.applyBudget(month.value)
    state.profile = r.profile
    await loadData(true)
    invalidatePlan()
    toast(`Plan updated: income & expenses set${r.accounts_updated ? `, ${r.accounts_updated} account contribution${r.accounts_updated > 1 ? 's' : ''} synced` : ''}`)
  } catch (e) { toast(errMsg(e), 'err') } finally { applying.value = false }
}
const hasPrev = computed(() => months.value.some((m) => m.month < month.value))
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div>
        <h1>Household budget</h1>
        <p>One budget per month. Amounts can be formulas like <code>=(1368.23*2)+500</code>, just like your spreadsheet.</p>
      </div>
      <div class="row wrap">
        <div class="monthnav glass">
          <button class="btn icon sm" aria-label="Previous month" @click="month = shiftMonth(month, -1)"><Icon name="left" :size="16" /></button>
          <input v-model="month" class="mpick" type="month" aria-label="Budget month" />
          <button class="btn icon sm" aria-label="Next month" @click="month = shiftMonth(month, 1)"><Icon name="right" :size="16" /></button>
        </div>
      </div>
    </div>

    <div v-if="loading" class="grid cols-3"><div v-for="i in 3" :key="i" class="skeleton" style="height: 320px" /></div>

    <div v-else-if="!exists" class="glass empty rise">
      <h3>No budget for {{ monthLabel(month) }}</h3>
      <p>Start from last month, the starter template based on your spreadsheet, or a blank page.</p>
      <div class="row wrap" style="justify-content: center; margin-top: 18px">
        <button v-if="hasPrev" class="btn accent" @click="create('copy')"><Icon name="copy" :size="17" /> Copy previous month</button>
        <button class="btn" :class="{ accent: !hasPrev }" @click="create('template')"><Icon name="calendar" :size="17" /> Use starter template</button>
        <button class="btn" @click="create('blank')">Blank budget</button>
      </div>
    </div>

    <template v-else>
      <div class="grid cols-4 kpis">
        <div class="glass pad rise"><div class="stat-label">Monthly income</div><div class="stat-value"><CountUp :value="sum.income" /></div><div class="small faint">{{ money(sum.annualIncome) }}/yr</div></div>
        <div class="glass pad rise" style="--i: 1"><div class="stat-label">Savings</div><div class="stat-value pos"><CountUp :value="sum.savings" /></div><div class="small faint"><b>{{ pct(sum.p(sum.savings), 1) }}</b> of income</div></div>
        <div class="glass pad rise" style="--i: 2"><div class="stat-label">Expenses</div><div class="stat-value"><CountUp :value="sum.expenses" /></div><div class="small faint"><b>{{ pct(sum.p(sum.expenses), 1) }}</b> of income</div></div>
        <div class="glass pad rise" style="--i: 3"><div class="stat-label">Monthly remainder</div><div class="stat-value" :class="sum.remainder >= 0 ? 'pos' : 'neg'"><CountUp :value="sum.remainder" /></div><div class="small faint"><b>{{ pct(sum.p(sum.remainder), 1) }}</b> of income</div></div>
      </div>

      <div class="grid sections">
        <section v-for="(s, si) in sections" :key="s.id" class="glass pad rise" :style="{ '--i': si + 2, '--c': s.color }">
          <div class="card-head">
            <div><h2><i class="dot" :style="{ background: s.color }" /> {{ s.title }}</h2><span class="sub">{{ s.hint }}</span></div>
            <b class="num tot">{{ money(total(s.id)) }}</b>
          </div>
          <TransitionGroup name="row" tag="div" class="rowlist">
            <div v-for="r in bySection(s.id)" :key="r.key" class="brow" :class="{ sav: s.id === 'savings' }">
              <input v-model="r.name" class="input name" placeholder="Name" maxlength="80" aria-label="Item name" @input="touch" />
              <div class="amt">
                <input v-model="r.text" class="input num" :class="{ bad: invalid(r) }" placeholder="0.00" inputmode="decimal" aria-label="Amount or formula" :title="invalid(r) ? 'Invalid formula' : ''" @input="touch" />
                <small v-if="!isPlain(r.text) && rowAmount(r) !== null" class="calc num">= {{ money2(rowAmount(r)!) }}</small>
              </div>
              <template v-if="s.id === 'savings'">
                <div class="input-wrap has-r g"><input class="input num" :value="r.growth ? +(r.growth * 100).toFixed(2) : ''" inputmode="decimal" placeholder="0" aria-label="Expected annual growth %" title="Expected annual growth" @input="r.growth = (parseFloat(($event.target as HTMLInputElement).value) || 0) / 100; touch()" /><i class="adorn r">%</i></div>
                <select v-model="r.account_id" class="input acc" aria-label="Linked account" title="Link to an account to sync its monthly contribution" @change="touch">
                  <option :value="null">No account</option>
                  <option v-for="a in linkable" :key="a.id" :value="a.id">{{ a.name }}</option>
                </select>
              </template>
              <button class="btn icon sm x" aria-label="Remove" @click="removeRow(r)"><Icon name="close" :size="15" /></button>
            </div>
          </TransitionGroup>
          <button class="btn sm addbtn" @click="addRow(s.id)"><Icon name="plus" :size="15" /> {{ s.add }}</button>
        </section>
      </div>

      <div class="grid split" style="margin-top: 18px">
        <section class="glass pad rise" style="--i: 5">
          <div class="card-head"><h2>Annual outlook</h2><span class="sub">monthly × 12, growth at your estimated rates</span></div>
          <dl class="ann">
            <div><dt>Annual income</dt><dd class="num">{{ money(sum.annualIncome) }}</dd></div>
            <div><dt>Annual savings</dt><dd class="num pos">{{ money(sum.annualSavings) }}</dd></div>
            <div><dt>Estimated growth</dt><dd class="num">{{ money(sum.growth) }}</dd></div>
            <div><dt>Income + growth</dt><dd class="num">{{ money(sum.annualIncome + sum.growth) }}</dd></div>
            <div><dt>Annual expenses</dt><dd class="num">{{ money(sum.annualExpenses) }}</dd></div>
            <div><dt>Annual remainder</dt><dd class="num" :class="sum.annualRemainder >= 0 ? 'pos' : 'neg'">{{ money(sum.annualRemainder) }}</dd></div>
          </dl>
          <hr class="divider" />
          <div class="row wrap">
            <button class="btn accent sm" :disabled="applying" @click="apply"><Icon name="target" :size="16" /> Use in retirement plan</button>
            <span class="small faint">Sets annual income &amp; expenses and syncs linked savings to your accounts.</span>
          </div>
        </section>

        <section class="glass pad rise" style="--i: 6">
          <div class="card-head"><h2>Where it goes</h2><span class="sub">{{ monthLabel(month) }}</span></div>
          <div class="donutrow">
            <DonutChart :segments="donut" :size="150" :thickness="18" center-label="Remainder" :center-value="money(sum.remainder)" />
            <div class="bars">
              <div v-for="e in topExpenses" :key="e.name" class="bar"><span>{{ e.name }}</span><i><b :style="{ width: e.w + '%' }" /></i><em class="num">{{ money(e.v) }}</em></div>
              <p v-if="!topExpenses.length" class="muted small">Add expenses to see a breakdown.</p>
            </div>
          </div>
        </section>
      </div>

      <div class="row foot">
        <span class="status small" :class="saveState">
          <template v-if="saveState === 'saving'"><span class="spin" /> Saving…</template>
          <template v-else-if="saveState === 'saved'"><Icon name="check" :size="15" /> All changes saved</template>
          <template v-else-if="saveState === 'error'">Couldn't save</template>
          <template v-else-if="rows.some(invalid)"><span class="neg">Fix invalid formulas to save</span></template>
          <template v-else>Changes save automatically</template>
        </span>
        <span class="spacer" />
        <button class="btn sm danger" @click="removeMonth"><Icon name="trash" :size="15" /> Delete month</button>
      </div>
    </template>

    <section v-if="months.length" class="glass pad history rise" style="--i: 7">
      <div class="card-head"><h2>Budget history</h2><span class="sub">{{ months.length }} month{{ months.length > 1 ? 's' : '' }}</span></div>
      <div class="table-wrap">
        <table class="t">
          <thead><tr><th>Month</th><th>Income</th><th>Savings</th><th>Expenses</th><th>Remainder</th></tr></thead>
          <tbody>
            <tr v-for="m in months" :key="m.month" :class="{ retire: m.month === month }" style="cursor: pointer" @click="month = m.month">
              <td>{{ monthLabel(m.month) }}</td><td>{{ money(m.income) }}</td><td>{{ money(m.savings) }}</td><td>{{ money(m.expenses) }}</td>
              <td :class="m.income - m.savings - m.expenses >= 0 ? 'pos' : 'neg'">{{ money(m.income - m.savings - m.expenses) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </main>
</template>

<style scoped>
code { background: rgba(0, 0, 0, 0.3); padding: 2px 7px; border-radius: 7px; font-size: 0.9em; color: var(--accent); }
.monthnav { display: flex; align-items: center; gap: 4px; padding: 5px; border-radius: 999px; }
.mpick { background: transparent; border: 0; color: var(--text); font: inherit; font-weight: 700; height: 34px; padding: 0 8px; text-align: center; cursor: pointer; color-scheme: dark; }
.mpick:focus-visible { outline: 2px solid var(--accent); border-radius: 8px; }
.kpis { margin-bottom: 18px; }
.sections { align-items: start; grid-template-columns: minmax(0, 1fr) minmax(0, 1.25fr) minmax(0, 1fr); }
@media (max-width: 1200px) { .sections { grid-template-columns: 1fr; } }
.tot { font-size: 20px; letter-spacing: -0.02em; }
.card-head h2 { display: flex; align-items: center; gap: 9px; }
.rowlist { display: grid; gap: 8px; margin-bottom: 12px; }
.brow { display: grid; grid-template-columns: minmax(0, 1fr) 148px 30px; gap: 8px; align-items: start; }
.brow.sav { grid-template-columns: minmax(0, 1fr) 148px 66px 30px; }
.brow.sav .acc { grid-column: 1 / -1; grid-row: 2; height: 34px; font-size: 12.5px; }
.brow.sav .x { grid-column: 4; grid-row: 1; }
.brow.sav { padding-bottom: 6px; border-bottom: 1px solid rgba(255, 255, 255, 0.05); }
.brow .input { height: 40px; padding: 0 11px; font-size: 14px; }
.brow .x { margin-top: 3px; opacity: 0.4; }
.brow:hover .x, .brow:focus-within .x { opacity: 1; }
.amt { display: grid; }
.input.bad { border-color: var(--danger); box-shadow: 0 0 0 3px rgba(255, 138, 128, 0.15); }
.calc { color: var(--accent); font-size: 11.5px; text-align: right; padding: 3px 4px 0; }
.input-wrap.g .input { padding-right: 20px; padding-left: 8px; text-align: right; }
.input-wrap.g .adorn.r { right: 8px; font-size: 12px; }
.addbtn { width: 100%; border-style: dashed; background: transparent; color: var(--muted); }
.row-enter-active, .row-leave-active { transition: all 0.35s var(--ease); }
.row-enter-from, .row-leave-to { opacity: 0; transform: translateX(-12px); }
.ann { margin: 0; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 4px 24px; }
.ann div { display: flex; justify-content: space-between; padding: 9px 0; border-bottom: 1px solid rgba(255, 255, 255, 0.06); }
.ann dt { color: var(--muted); }
.ann dd { margin: 0; font-weight: 700; }
.donutrow { display: flex; align-items: center; gap: 22px; flex-wrap: wrap; }
.bars { flex: 1; min-width: 200px; display: grid; gap: 9px; }
.bar { display: grid; grid-template-columns: 96px 1fr 70px; gap: 10px; align-items: center; font-size: 12.5px; }
.bar span { color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bar i { height: 8px; border-radius: 99px; background: rgba(255, 255, 255, 0.08); overflow: hidden; }
.bar b { display: block; height: 100%; border-radius: 99px; background: linear-gradient(90deg, var(--c-wine), var(--c-rose)); transition: width 0.8s var(--ease); }
.bar em { font-style: normal; text-align: right; font-weight: 600; }
.foot { margin: 16px 4px 0; }
.status { display: inline-flex; align-items: center; gap: 8px; color: var(--muted); }
.status.saved { color: var(--accent); }
.history { margin-top: 18px; }
@media (max-width: 680px) { .ann { grid-template-columns: 1fr; } .brow, .brow.sav { grid-template-columns: minmax(0, 1fr) 118px 30px; } .brow.sav { grid-template-columns: minmax(0, 1fr) 100px 60px 30px; } }
</style>

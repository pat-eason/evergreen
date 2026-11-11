<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import DonutChart from '@/components/DonutChart.vue'
import Icon from '@/components/Icon.vue'
import StackedBars, { type BarRow } from '@/components/StackedBars.vue'
import { api } from '@/lib/api'
import { compact, money, pct } from '@/lib/format'
import { ensurePlan, errMsg, invalidatePlan, loadData, state, toast } from '@/lib/store'
import type { Strategy, YearRow } from '@/lib/types'

const loading = ref(true)
const open = ref<number | null>(null)
const busy = ref(false)
const compare = ref<{ id: Strategy; label: string; taxes: number; end: number; success: number }[]>([])
const comparing = ref(false)

const strategies: { id: Strategy; label: string; blurb: string }[] = [
  { id: 'smart', label: 'Smart', blurb: 'Fill the low tax brackets with tax-deferred money first, then taxable, then the rest. Roth last.' },
  { id: 'taxable_first', label: 'Taxable first', blurb: 'Conventional order: spend taxable accounts, then tax-deferred, then Roth.' },
  { id: 'tax_deferred_first', label: 'Tax-deferred first', blurb: 'Draw down 401(k)/IRA balances first to shrink future required distributions.' },
]
const ceilings = [{ v: 0, l: 'Off' }, { v: 12, l: 'Fill 12% bracket' }, { v: 22, l: 'Fill 22% bracket' }, { v: 24, l: 'Fill 24% bracket' }]

async function boot() {
  try {
    await loadData()
    await ensurePlan()
    const first = retired.value[0]
    if (first) open.value = first.age
  } catch (e) { toast(errMsg(e), 'err') } finally { loading.value = false }
}
watch(() => state.profileRev, async () => { try { await ensurePlan() } catch (e) { toast(errMsg(e), 'err') } })
onMounted(boot)

const profile = computed(() => state.profile)
const retired = computed<YearRow[]>(() => (state.plan?.years ?? []).filter((r) => r.retired))

const COLORS = ['var(--c-green)', 'var(--c-accent)', 'var(--c-rose)', 'var(--c-cream)']
const NAMES = ['Income', 'Taxable & cash', 'Tax-deferred', 'Tax-free']
const parts = (r: YearRow) => [r.other_income, r.withdrawals.cash + r.withdrawals.taxable, r.withdrawals.tax_deferred, r.withdrawals.tax_free]
const bars = computed<BarRow[]>(() => retired.value.map((r) => ({ label: `${r.year} (${r.age})`, parts: parts(r), line: r.total_spending })))
const selectedIdx = computed(() => retired.value.findIndex((r) => r.age === open.value))

function toggle(age: number) { open.value = open.value === age ? null : age }
async function pickBar(i: number) {
  open.value = retired.value[i].age
  await nextTick()
  document.getElementById(`yr-${open.value}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
}

async function setStrategy(patch: Partial<{ withdrawal_strategy: Strategy; roth_conversion_ceiling: number }>) {
  if (!profile.value || busy.value) return
  busy.value = true
  try {
    const next = { ...profile.value, ...patch }
    const r = await api.saveProfile(next)
    state.profile = r.profile
    invalidatePlan()
    await ensurePlan()
    compare.value = []
    toast('Strategy updated')
  } catch (e) { toast(errMsg(e), 'err') } finally { busy.value = false }
}

async function runCompare() {
  comparing.value = true
  try {
    const out = await Promise.all(strategies.map(async (s) => {
      const r = await api.simulate({ withdrawal_strategy: s.id }, 400, true)
      return { id: s.id, label: s.label, taxes: r.summary.lifetime_taxes, end: r.summary.portfolio_at_end, success: r.summary.success_rate }
    }))
    compare.value = out
  } catch (e) { toast(errMsg(e), 'err') } finally { comparing.value = false }
}
watch(() => state.plan, () => { compare.value = [] })

const best = computed(() => (compare.value.length ? compare.value.reduce((a, b) => (b.end > a.end ? b : a)).id : null))

function slices(r: YearRow) {
  return [
    { label: 'Income events', value: r.other_income, color: 'var(--c-green)' },
    { label: 'Taxable & cash', value: r.withdrawals.cash + r.withdrawals.taxable, color: 'var(--c-accent)' },
    { label: 'Tax-deferred', value: r.withdrawals.tax_deferred, color: 'var(--c-rose)' },
    { label: 'Tax-free', value: r.withdrawals.tax_free, color: 'var(--c-cream)' },
  ]
}
const sourcesTotal = (r: YearRow) => parts(r).reduce((a, b) => a + b, 0)
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div>
        <h1>Smart withdrawal strategy</h1>
        <p>A tax-aware sequence for turning your accounts into annual retirement income that supports your budgeted spending while minimizing taxes.</p>
      </div>
    </div>

    <section class="glass pad rise">
      <div class="card-head">
        <div class="tabs" role="tablist">
          <button v-for="s in strategies" :key="s.id" class="tab" role="tab" :disabled="busy" :class="{ on: profile?.withdrawal_strategy === s.id }" @click="setStrategy({ withdrawal_strategy: s.id })">{{ s.label }}</button>
        </div>
        <label class="row ceil"><span class="label">Roth conversions</span>
          <select class="input" :value="profile?.roth_conversion_ceiling ?? 0" :disabled="busy" aria-label="Roth conversion ceiling" @change="setStrategy({ roth_conversion_ceiling: +($event.target as HTMLSelectElement).value })">
            <option v-for="c in ceilings" :key="c.v" :value="c.v">{{ c.l }}</option>
          </select>
        </label>
      </div>
      <p class="small muted">{{ strategies.find((s) => s.id === profile?.withdrawal_strategy)?.blurb }}</p>
    </section>

    <section class="glass pad rise" style="margin-top: 18px; --i: 1">
      <div class="card-head"><h2>Retirement income sources</h2>
        <div class="legend"><span v-for="(n, i) in NAMES" :key="n"><i class="dot" :style="{ background: COLORS[i] }" />{{ n }}</span><span><i class="dot ln" />Budgeted spending</span></div>
      </div>
      <div v-if="loading" class="skeleton" style="height: 300px" />
      <div v-else-if="!retired.length" class="empty"><h3>No retirement years in this plan</h3><p>Set a retirement age earlier than your plan end age on the Plan page.</p></div>
      <StackedBars v-else :rows="bars" :colors="COLORS" :names="NAMES" :height="300" :selected="selectedIdx >= 0 ? selectedIdx : null" @select="pickBar" />
    </section>

    <section class="glass pad rise" style="margin-top: 18px; --i: 2">
      <div class="card-head">
        <div><h2>Compare strategies</h2><span class="sub">Same plan, three withdrawal orders (median market + simulations)</span></div>
        <button class="btn sm" :disabled="comparing || !retired.length" @click="runCompare"><span v-if="comparing" class="spin" /><Icon v-else name="layers" :size="16" /> Run comparison</button>
      </div>
      <div v-if="compare.length" class="cmp">
        <div v-for="c in compare" :key="c.id" class="cmpc" :class="{ best: c.id === best }">
          <div class="row"><h3>{{ c.label }}</h3><span v-if="c.id === best" class="chip good">Highest ending balance</span></div>
          <dl><div><dt>Lifetime taxes</dt><dd class="num">{{ compact(c.taxes) }}</dd></div><div><dt>Median ending portfolio</dt><dd class="num">{{ compact(c.end) }}</dd></div><div><dt>Success rate</dt><dd class="num">{{ pct(c.success) }}</dd></div></dl>
        </div>
      </div>
      <p v-else class="small faint">Run a comparison to see how each ordering affects lifetime taxes and what you leave behind.</p>
    </section>

    <section class="glass pad rise" style="margin-top: 18px; --i: 3">
      <div class="yhead"><span>Year (age)</span><span>Retirement income sources</span><span>Estimated taxes</span><span>Budgeted spending</span><span>Portfolio balance</span></div>
      <div v-if="loading" class="skeleton" style="height: 200px; margin-top: 12px" />
      <TransitionGroup v-else name="yr" tag="div">
        <article v-for="r in retired" :id="`yr-${r.age}`" :key="r.age" class="yr" :class="{ on: open === r.age, short: r.shortfall > 1 }">
          <button class="ysum" :aria-expanded="open === r.age" @click="toggle(r.age)">
            <span class="y"><b>{{ r.year }}</b> ({{ r.age }}) <Icon name="chevron" :size="15" class="chev" /></span>
            <span class="num">{{ money(sourcesTotal(r)) }}</span>
            <span class="num neg">-{{ money(r.taxes.total) }}</span>
            <span class="num neg">-{{ money(r.total_spending) }}</span>
            <span class="num" :class="{ neg: r.end.total <= 0 }">{{ money(r.end.total) }}</span>
          </button>
          <Transition name="exp">
            <div v-if="open === r.age" class="detail">
              <div class="dgrid">
                <div class="don"><DonutChart :segments="slices(r)" :size="170" :thickness="18" center-label="Smart withdrawal sources" :center-value="money(sourcesTotal(r))" /></div>
                <div class="col">
                  <h4>Income events <b class="num">{{ money(r.other_income) }}</b></h4>
                  <div v-for="i in r.income_items ?? []" :key="i.name" class="li"><span>{{ i.name }}</span><b class="num">{{ money(i.amount) }}</b></div>
                  <h4 style="margin-top: 14px">Portfolio withdrawals <b class="num">{{ money(r.withdrawals.cash + r.withdrawals.taxable + r.withdrawals.tax_deferred + r.withdrawals.tax_free) }}</b></h4>
                  <div v-if="r.withdrawals.cash > 0" class="li"><span><i class="dot" style="background: var(--c-mint)" /> Cash</span><b class="num">{{ money(r.withdrawals.cash) }}</b></div>
                  <div class="li"><span><i class="dot" style="background: var(--c-accent)" /> Taxable</span><b class="num">{{ money(r.withdrawals.taxable) }}</b></div>
                  <div class="li"><span><i class="dot" style="background: var(--c-rose)" /> Tax-deferred{{ r.withdrawals.rmd > 0 ? ' (incl. RMD)' : '' }}</span><b class="num">{{ money(r.withdrawals.tax_deferred) }}</b></div>
                  <div class="li"><span><i class="dot" style="background: var(--c-cream)" /> Tax-free</span><b class="num">{{ money(r.withdrawals.tax_free) }}</b></div>
                  <div v-if="r.withdrawals.roth_converted > 0" class="li conv"><span>Roth conversion</span><b class="num">{{ money(r.withdrawals.roth_converted) }}</b></div>
                </div>
                <div class="col">
                  <h4>Estimated taxes <b class="num neg">-{{ money(r.taxes.total) }}</b></h4>
                  <div class="li"><span>Federal income</span><b class="num">-{{ money(r.taxes.federal) }}</b></div>
                  <div class="li"><span>State income</span><b class="num">-{{ money(r.taxes.state) }}</b></div>
                  <div class="li"><span>Gains &amp; dividends</span><b class="num">-{{ money(r.taxes.gains) }}</b></div>
                  <div class="li"><span>Marginal bracket</span><b class="num">{{ pct(r.taxes.marginal_rate) }}</b></div>
                </div>
                <div class="col">
                  <h4>Budgeted spending <b class="num neg">-{{ money(r.total_spending) }}</b></h4>
                  <div class="li"><span>Retirement spending</span><b class="num">-{{ money(r.retirement_spending) }}</b></div>
                  <div v-for="i in r.spending_items ?? []" :key="i.name" class="li"><span>{{ i.name }}</span><b class="num">-{{ money(i.amount) }}</b></div>
                </div>
                <div class="col">
                  <h4>Portfolio balance <b class="num">{{ money(r.end.total) }}</b></h4>
                  <div class="li"><span>Taxable</span><b class="num">{{ money(r.end.taxable + r.end.cash) }}</b></div>
                  <div v-if="r.reinvested > 0" class="li sub"><span>Reinvested</span><b class="num">{{ money(r.reinvested) }}</b></div>
                  <div class="li"><span>Tax-deferred</span><b class="num">{{ money(r.end.tax_deferred) }}</b></div>
                  <div class="li"><span>Tax-free</span><b class="num">{{ money(r.end.tax_free) }}</b></div>
                </div>
              </div>
              <p v-for="t in r.insights ?? []" :key="t" class="insight"><Icon name="info" :size="18" /> {{ t }}</p>
            </div>
          </Transition>
        </article>
      </TransitionGroup>
    </section>
  </main>
</template>

<style scoped>
.ceil { gap: 10px; }
.ceil .input { width: 180px; height: 38px; }
.tab:disabled { cursor: wait; }
.dot.ln { background: #f5f0e8; height: 3px; border-radius: 2px; width: 14px; }
.cmp { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; }
.cmpc { padding: 16px 18px; border-radius: 18px; border: 1px solid var(--line); background: rgba(0, 0, 0, 0.16); transition: border-color 0.3s; }
.cmpc.best { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.cmpc h3 { flex: 1; }
.cmpc dl { margin: 12px 0 0; display: grid; gap: 6px; }
.cmpc dl div { display: flex; justify-content: space-between; font-size: 13.5px; }
.cmpc dt { color: var(--muted); }
.cmpc dd { margin: 0; font-weight: 700; }
.yhead, .ysum { display: grid; grid-template-columns: 1.1fr 1.2fr 1fr 1fr 1fr; gap: 12px; align-items: center; text-align: right; }
.yhead { font-size: 11.5px; font-weight: 700; letter-spacing: 0.05em; text-transform: uppercase; color: var(--muted); padding: 4px 14px 12px; border-bottom: 1px solid var(--line); }
.yhead span:first-child, .ysum .y { text-align: left; }
.yr { border-bottom: 1px solid rgba(255, 255, 255, 0.06); }
.ysum { width: 100%; padding: 16px 14px; background: transparent; border: 0; color: var(--text); font: inherit; font-size: 15.5px; cursor: pointer; border-radius: 14px; transition: background 0.2s; }
.ysum:hover { background: rgba(255, 255, 255, 0.05); }
.ysum:focus-visible { outline: 2px solid var(--accent); }
.y { display: inline-flex; align-items: center; gap: 6px; }
.chev { color: var(--accent); transition: transform 0.35s var(--ease); }
.yr.on .chev { transform: rotate(180deg); }
.yr.on { background: rgba(255, 255, 255, 0.03); border-radius: 18px; }
.yr.short .ysum { box-shadow: inset 3px 0 0 var(--danger); }
.detail { padding: 6px 14px 22px; }
.dgrid { display: grid; grid-template-columns: 190px repeat(4, minmax(0, 1fr)); gap: 22px; align-items: start; }
.don { display: grid; place-items: center; }
.col h4 { display: flex; justify-content: space-between; font-size: 13.5px; padding-bottom: 8px; margin-bottom: 4px; border-bottom: 1px solid var(--line); }
.li { display: flex; justify-content: space-between; gap: 8px; font-size: 13.5px; padding: 5px 0; color: var(--muted); }
.li b { color: var(--text); font-weight: 600; }
.li.sub { color: var(--faint); font-size: 12.5px; }
.li.conv b { color: var(--accent); }
.insight { display: flex; align-items: flex-start; gap: 10px; margin-top: 16px; padding: 12px 16px; border-radius: 14px; background: var(--accent-soft); font-size: 13.5px; }
.insight svg { color: var(--accent); flex: none; margin-top: 1px; }
.exp-enter-active, .exp-leave-active { transition: opacity 0.3s ease, transform 0.4s var(--ease); }
.exp-enter-from, .exp-leave-to { opacity: 0; transform: translateY(-8px); }
.yr-enter-active { transition: all 0.5s var(--ease); }
.yr-enter-from { opacity: 0; transform: translateY(10px); }
@media (max-width: 1100px) { .dgrid { grid-template-columns: 1fr 1fr; } .don { grid-column: 1 / -1; } .cmp { grid-template-columns: 1fr; } }
@media (max-width: 720px) { .yhead { display: none; } .ysum { grid-template-columns: 1fr 1fr; row-gap: 4px; } .ysum .y { grid-column: 1 / -1; } .dgrid { grid-template-columns: 1fr; } }
</style>

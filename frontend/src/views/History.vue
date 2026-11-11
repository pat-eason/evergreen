<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import CountUp from '@/components/CountUp.vue'
import Icon from '@/components/Icon.vue'
import LineChart, { type Series } from '@/components/LineChart.vue'
import ValueDrawer from '@/components/ValueDrawer.vue'
import { api } from '@/lib/api'
import { compact, dateTime, dayNumber, fromDayNumber, money, shortDate, signedMoney, signedPct } from '@/lib/format'
import { errMsg, loadData, ownerName, state, toast } from '@/lib/store'
import type { History, LedgerEntry } from '@/lib/types'

type Range = 3 | 6 | 12 | 0
type Mode = 'networth' | 'balance' | 'growth' | 'accounts'

const range = ref<Range>(12)
const mode = ref<Mode>('networth')
const history = ref<History | null>(null)
const loading = ref(true)
const entries = ref<LedgerEntry[]>([])
const total = ref(0)
const accountFilter = ref(0)
const loadingMore = ref(false)
const valueOpen = ref(false)

const ranges: { v: Range; l: string }[] = [{ v: 3, l: '3M' }, { v: 6, l: '6M' }, { v: 12, l: '1Y' }, { v: 0, l: 'All' }]
const modes: { id: Mode; l: string }[] = [
  { id: 'networth', l: 'Net worth' }, { id: 'balance', l: 'Assets vs liabilities' }, { id: 'growth', l: 'Growth vs contributions' }, { id: 'accounts', l: 'By account' },
]
const COLORS = ['var(--c-accent)', 'var(--c-mint)', 'var(--c-rose)', 'var(--c-cream)', 'var(--c-green)', 'var(--c-wine)']

async function loadHistory() {
  try { history.value = await api.history({ months: range.value, by_account: true }) } catch (e) { toast(errMsg(e), 'err') } finally { loading.value = false }
}
async function loadEntries(reset = true) {
  loadingMore.value = true
  try {
    const r = await api.ledger({ account_id: accountFilter.value || undefined, limit: 40, offset: reset ? 0 : entries.value.length })
    entries.value = reset ? r.entries : [...entries.value, ...r.entries]
    total.value = r.total
  } catch (e) { toast(errMsg(e), 'err') } finally { loadingMore.value = false }
}
async function refresh() { await Promise.all([loadHistory(), loadEntries()]) }

onMounted(async () => {
  try { await loadData() } catch { /* shown elsewhere */ }
  await refresh()
})
watch(range, loadHistory)
watch(accountFilter, () => loadEntries())

const pts = computed(() => history.value?.points ?? [])
const enough = computed(() => pts.value.length >= 2)
const sum = computed(() => history.value?.summary)
const spanDays = computed(() => (pts.value.length ? dayNumber(pts.value[pts.value.length - 1].date) - dayNumber(pts.value[0].date) : 0))

const xs = (f: (p: History['points'][number]) => number) => pts.value.map((p) => ({ x: dayNumber(p.date), y: f(p) }))

const series = computed<Series[]>(() => {
  const h = history.value
  if (!h || !enough.value) return []
  switch (mode.value) {
    case 'networth':
      return [{ key: 'nw', label: 'Net worth', color: 'var(--c-accent)', area: true, fill: 0.5, points: xs((p) => p.net_worth) }]
    case 'balance':
      return [
        { key: 'a', label: 'Assets', color: 'var(--c-green)', area: true, fill: 0.55, points: xs((p) => p.assets) },
        { key: 'l', label: 'Liabilities', color: 'var(--c-rose)', area: true, fill: 0.5, points: xs((p) => p.liabilities) },
      ]
    case 'growth':
      return [
        { key: 'bal', label: 'Cash & investments', color: 'var(--c-accent)', area: true, fill: 0.5, points: xs((p) => p.cash + p.investments) },
        { key: 'con', label: 'Money you put in', color: 'var(--c-cream)', dashed: true, width: 2, points: xs((p) => p.net_contributions) },
      ]
    default: {
      const last = pts.value.length - 1
      return (h.accounts ?? [])
        .filter((a) => a.alive)
        .map((a) => ({ a, s: h.series?.[String(a.id)] ?? [] }))
        .sort((x, y) => (y.s[last] ?? 0) - (x.s[last] ?? 0))
        .slice(0, 6)
        .map(({ a, s }, i) => ({ key: String(a.id), label: a.name, color: COLORS[i % COLORS.length], width: 2.2, points: pts.value.map((p, k) => ({ x: dayNumber(p.date), y: s[k] ?? 0 })) }))
    }
  }
})

const xTicks = computed(() => {
  if (!enough.value) return undefined
  const a = dayNumber(pts.value[0].date), b = dayNumber(pts.value[pts.value.length - 1].date)
  const n = 5
  return Array.from({ length: n + 1 }, (_, i) => Math.round(a + ((b - a) * i) / n))
})
const xFormat = (v: number) => {
  const d = fromDayNumber(v)
  return spanDays.value > 120 ? d.toLocaleDateString('en-US', { month: 'short', year: 'numeric', timeZone: 'UTC' }) : d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' })
}

const isGood = (v: number) => v >= 0

// ---- ledger rows ----
const reasonLabel = (e: LedgerEntry): string => {
  const asset = e.category === 'cash' || e.category === 'investment'
  switch (e.reason) {
    case 'deposit': return 'Deposit'
    case 'withdrawal': return 'Withdrawal'
    case 'payment': return 'Payment'
    case 'borrowing': return 'Borrowed more'
    case 'correction': return 'Correction'
    default: return asset ? 'Value change' : 'Interest / charges'
  }
}
const kindLabel = (e: LedgerEntry) => (e.first && e.kind !== 'deleted' ? 'Opening balance' : ({ created: 'Added', deleted: 'Removed', edited: 'Edited', updated: reasonLabel(e) })[e.kind])
const isLiab = (e: LedgerEntry) => !(e.category === 'cash' || e.category === 'investment')
/** Effect on net worth: more owed is bad. */
const impact = (e: LedgerEntry) => (isLiab(e) ? -e.delta : e.delta)
const chipClass = (e: LedgerEntry) => (e.first ? '' : e.kind === 'updated' || e.kind === 'created' ? (impact(e) >= 0 ? 'good' : 'bad') : e.kind === 'deleted' ? 'bad' : '')
const rowDate = (iso: string) => dateTime(iso)
const hasMore = computed(() => entries.value.length < total.value)
const noHistory = computed(() => !loading.value && !total.value && !entries.value.length)
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div>
        <h1>History</h1>
        <p>Every account you add, remove or update is written to a ledger, so you can see your net worth grow (or shrink) over time.</p>
      </div>
      <button class="btn accent" :disabled="!state.accounts.length" @click="valueOpen = true"><Icon name="plus" :size="17" /> Record a value</button>
    </div>

    <div v-if="sum && enough" class="grid cols-4 kpis">
      <div class="glass pad rise"><div class="stat-label">Net worth</div><div class="stat-value"><CountUp :value="sum.end_net_worth" /></div><div class="small faint">as of {{ shortDate(new Date(sum.to + 'T00:00:00'), true) }}</div></div>
      <div class="glass pad rise" style="--i: 1"><div class="stat-label">Change</div><div class="stat-value" :class="isGood(sum.change) ? 'pos' : 'neg'">{{ signedMoney(sum.change) }}</div><div class="small faint">{{ sum.start_net_worth !== 0 ? signedPct(sum.change_pct) + ' since ' : 'since ' }}{{ shortDate(new Date(sum.from + 'T00:00:00'), true) }}</div></div>
      <div class="glass pad rise" style="--i: 2"><div class="stat-label">Market growth</div><div class="stat-value" :class="isGood(sum.growth) ? 'pos' : 'neg'">{{ signedMoney(sum.growth) }}</div><div class="small faint">gains &amp; losses on cash and investments</div></div>
      <div class="glass pad rise" style="--i: 3"><div class="stat-label">Money you added</div><div class="stat-value">{{ signedMoney(sum.contributions) }}</div><div class="small faint">net deposits less withdrawals</div></div>
    </div>

    <section class="glass pad rise" style="margin-top: 18px; --i: 2">
      <div class="card-head">
        <div class="tabs" role="tablist" aria-label="Chart view"><button v-for="m in modes" :key="m.id" class="tab" role="tab" :class="{ on: mode === m.id }" @click="mode = m.id">{{ m.l }}</button></div>
        <div class="tabs" role="tablist" aria-label="Time range"><button v-for="r in ranges" :key="r.v" class="tab" role="tab" :class="{ on: range === r.v }" @click="range = r.v">{{ r.l }}</button></div>
      </div>

      <div v-if="loading" class="skeleton" style="height: 340px" />
      <div v-else-if="noHistory" class="empty"><h3>No history yet</h3><p>Add an account and your ledger starts filling in automatically.</p><RouterLink to="/accounts" class="btn sm" style="margin-top: 12px">Add accounts</RouterLink></div>
      <div v-else-if="!enough" class="empty">
        <h3>Your history starts today</h3>
        <p>You have a starting snapshot of <b class="text num">{{ money(pts[0]?.net_worth ?? 0) }}</b>. The timeline appears once balances change on a second day — record a value whenever you get a new statement.</p>
        <button class="btn sm accent" style="margin-top: 12px" @click="valueOpen = true"><Icon name="plus" :size="16" /> Record a value</button>
      </div>
      <template v-else>
        <LineChart :key="mode + range + series.length" :series="series" :height="340" :x-ticks="xTicks" :x-format="xFormat" :y-format="compact" :from-zero="mode === 'balance' || mode === 'accounts'" />
        <div class="legend" style="margin-top: 10px"><span v-for="s in series" :key="s.key"><i class="dot" :style="{ background: s.color }" />{{ s.label }}</span></div>
        <p v-if="mode === 'growth'" class="small muted" style="margin-top: 10px">The gap between the two lines is your market growth. Record deposits and withdrawals as such so they aren't counted as gains or losses.</p>
      </template>
    </section>

    <section class="glass pad rise" style="margin-top: 18px; --i: 3">
      <div class="card-head">
        <div><h2><Icon name="history" :size="18" /> Ledger</h2><span class="sub">{{ total }} {{ total === 1 ? 'entry' : 'entries' }}, newest first</span></div>
        <select v-model.number="accountFilter" class="input sel" aria-label="Filter by account">
          <option :value="0">All accounts</option>
          <option v-for="a in state.accounts" :key="a.id" :value="a.id">{{ a.name }}</option>
        </select>
      </div>

      <div v-if="loading" class="skeleton" style="height: 200px" />
      <p v-else-if="!entries.length" class="muted small">Nothing recorded yet.</p>
      <div v-else class="table-wrap">
        <table class="t lg">
          <thead><tr><th>When</th><th>Account</th><th>Event</th><th>Change</th><th>New balance</th></tr></thead>
          <tbody>
            <tr v-for="e in entries" :key="e.id">
              <td class="when">{{ rowDate(e.occurred_at) }}</td>
              <td class="acc"><b>{{ e.account_name }}</b><small class="faint">{{ ownerName(e.owner) }}</small></td>
              <td><span class="chip" :class="chipClass(e)">{{ kindLabel(e) }}</span><small v-if="e.note" class="note faint">{{ e.note }}</small></td>
              <td class="num">
                <template v-if="e.first && e.kind !== 'deleted'"><span class="muted">opened</span></template>
                <template v-else-if="e.kind === 'deleted'"><span class="muted">was {{ money(e.previous) }}</span></template>
                <template v-else-if="e.kind === 'edited'"><span class="muted">settings</span></template>
                <template v-else><span :class="impact(e) >= 0 ? 'pos' : 'neg'">{{ signedMoney(e.delta) }}</span></template>
              </td>
              <td class="num">{{ e.kind === 'deleted' ? '—' : money(e.balance) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="hasMore" class="row" style="justify-content: center; margin-top: 14px"><button class="btn sm" :disabled="loadingMore" @click="loadEntries(false)"><span v-if="loadingMore" class="spin" /> Load more</button></div>
    </section>

    <ValueDrawer :open="valueOpen" :account="null" @close="valueOpen = false" @saved="refresh" />
  </main>
</template>

<style scoped>
.kpis { margin-bottom: 0; }
.text { color: var(--text); }
.sel { width: auto; min-width: 200px; height: 40px; }
.t.lg td { padding: 13px 14px; }
/* account + event read left-to-right; amounts stay right-aligned */
.t.lg th:nth-child(2), .t.lg td:nth-child(2), .t.lg th:nth-child(3), .t.lg td:nth-child(3) { text-align: left; }
.t td.when { color: var(--muted); font-size: 13px; }
.t td.acc { display: table-cell; }
.t td.acc b { display: block; }
.t td.acc small { display: block; font-size: 12px; }
.note { display: block; margin-top: 3px; max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
@media (max-width: 680px) { .sel { min-width: 0; width: 100%; } }
</style>

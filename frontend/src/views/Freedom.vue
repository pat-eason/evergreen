<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import CountUp from '@/components/CountUp.vue'
import Icon from '@/components/Icon.vue'
import LineChart, { type Dot, type Series, type VLine } from '@/components/LineChart.vue'
import ProgressRing from '@/components/ProgressRing.vue'
import Slider from '@/components/Slider.vue'
import { api } from '@/lib/api'
import { compact, money, pct } from '@/lib/format'
import { errMsg, loadData, state, toast } from '@/lib/store'
import type { Calculation, FreedomInputs, FreedomResult } from '@/lib/types'

type Tab = 'crossover' | 'coast' | 'freedom'
const tab = ref<Tab>('crossover')
const res = ref<FreedomResult | null>(null)
const base = ref<FreedomInputs | null>(null)
const inputs = reactive<Partial<FreedomInputs>>({})
const loading = ref(true)
const calculating = ref(false)
const saved = ref<Calculation[]>([])

let seq = 0
let timer: ReturnType<typeof setTimeout> | undefined
async function run() {
  const my = ++seq
  calculating.value = true
  try {
    const r = await api.freedom({ ...inputs })
    if (my !== seq) return
    res.value = r
    if (!base.value) { base.value = { ...r.inputs }; Object.assign(inputs, r.inputs) }
  } catch (e) { if (my === seq) toast(errMsg(e), 'err') } finally { if (my === seq) { calculating.value = false; loading.value = false } }
}
function schedule() { clearTimeout(timer); timer = setTimeout(run, 160) }
watch(() => ({ ...inputs }), () => { if (base.value) schedule() })

watch(() => state.profileRev, () => { base.value = null; run() })

onMounted(async () => {
  try { await loadData() } catch { /* handled by run */ }
  await run()
  try { saved.value = await api.calculations('freedom') } catch { /* optional */ }
})

function reset() { if (base.value) Object.assign(inputs, base.value) }
const changed = computed(() => !!base.value && (Object.keys(base.value) as (keyof FreedomInputs)[]).some((k) => Math.abs((inputs[k] ?? 0) - base.value![k]) > 1e-9))

// ----- charts -----
const horizon = computed(() => {
  const r = res.value
  if (!r) return 0
  const target = Math.max(r.inputs.retirement_age, r.freedom.fi_age, r.crossover.age, r.coast.coast_age)
  return Math.min(100, Math.max(r.inputs.current_age + 6, target + 5))
})
const pts = computed(() => (res.value?.series ?? []).filter((p) => p.age <= horizon.value))
const retireLine = computed<VLine[]>(() => (res.value ? [{ x: res.value.inputs.retirement_age, label: `Retire ${res.value.inputs.retirement_age}` }] : []))

const chart = computed<{ series: Series[]; dots: Dot[]; vlines: VLine[] }>(() => {
  const r = res.value
  if (!r) return { series: [], dots: [], vlines: [] }
  const P = pts.value
  if (tab.value === 'crossover') {
    const c = r.crossover
    return {
      series: [
        { key: 'g', label: 'Investment growth / yr', color: 'var(--c-mint)', area: true, fill: 0.55, points: P.map((p) => ({ x: p.age, y: p.growth })) },
        { key: 'c', label: 'Your contributions / yr', color: 'var(--c-accent)', area: true, fill: 0.4, points: P.map((p) => ({ x: p.age, y: p.contributions })) },
      ],
      dots: c.reached ? [{ x: c.age, y: P.find((p) => p.age === c.age)?.growth ?? 0, label: 'Crossover', color: 'var(--c-accent)' }] : [],
      vlines: [],
    }
  }
  if (tab.value === 'coast') {
    const c = r.coast
    const at = P.find((p) => p.age === c.coast_age)
    return {
      series: [
        { key: 'pf', label: 'Your portfolio', color: 'var(--c-accent)', area: true, fill: 0.5, points: P.map((p) => ({ x: p.age, y: p.portfolio })) },
        { key: 'cn', label: 'Coast number', color: 'var(--c-rose)', width: 2.5, points: P.filter((p) => p.age <= r.inputs.retirement_age).map((p) => ({ x: p.age, y: p.coast_number })) },
        { key: 'fi', label: 'FI number', color: 'var(--c-cream)', dashed: true, width: 1.6, points: P.map((p) => ({ x: p.age, y: p.fi_number })) },
      ],
      dots: c.coast_age && at ? [{ x: c.coast_age, y: at.portfolio, label: 'Coast FIRE', color: 'var(--c-accent)' }] : [],
      vlines: retireLine.value,
    }
  }
  const f = r.freedom
  const at = P.find((p) => p.age === f.fi_age)
  return {
    series: [
      { key: 'pf', label: 'Your portfolio', color: 'var(--c-accent)', area: true, fill: 0.5, points: P.map((p) => ({ x: p.age, y: p.portfolio })) },
      { key: 'fat', label: 'Fat FI', color: 'var(--c-rose)', dashed: true, width: 1.4, points: P.map((p) => ({ x: p.age, y: f.fat_fi })) },
      { key: 'fi', label: 'FI number', color: 'var(--c-cream)', width: 2.2, points: P.map((p) => ({ x: p.age, y: p.fi_number })) },
      { key: 'lean', label: 'Lean FI', color: 'var(--c-mint)', dashed: true, width: 1.4, points: P.map((p) => ({ x: p.age, y: f.lean_fi })) },
    ],
    dots: f.fi_age && at ? [{ x: f.fi_age, y: at.portfolio, label: 'Financial freedom', color: 'var(--c-accent)' }] : [],
    vlines: retireLine.value,
  }
})

const tabInfo: Record<Tab, { title: string; icon: string; blurb: string }> = {
  crossover: { title: 'Contribution Crossover', icon: 'trend', blurb: 'The tipping point where your investments earn more each year than you add to them. After it, growth does the heavy lifting.' },
  coast: { title: 'Coast FIRE', icon: 'umbrella', blurb: 'Enough invested today that, with zero further contributions, compounding alone grows it to your FI number by retirement. You then only need to cover current expenses.' },
  freedom: { title: 'Financial Freedom', icon: 'spark', blurb: 'Your portfolio can fund your spending indefinitely at the safe withdrawal rate — your "FI number" is annual spending ÷ SWR.' },
}

const yrs = (n: number) => `${n} year${n === 1 ? '' : 's'}`

async function snapshot() {
  const name = prompt('Name this snapshot', `Freedom ${new Date().toLocaleDateString()}`)
  if (!name || !res.value) return
  try {
    saved.value.unshift(await api.saveCalculation({ kind: 'freedom', name, inputs: { ...inputs }, result: res.value }))
    toast('Snapshot saved')
  } catch (e) { toast(errMsg(e), 'err') }
}
async function delSnap(c: Calculation) {
  try { await api.deleteCalculation(c.id); saved.value = saved.value.filter((x) => x.id !== c.id) } catch (e) { toast(errMsg(e), 'err') }
}
function loadSnap(c: Calculation) { Object.assign(inputs, c.inputs as FreedomInputs); toast('Snapshot loaded into the levers') }

const age0 = computed(() => res.value?.inputs.current_age ?? state.profile?.current_age ?? 30)
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div>
        <h1>Financial freedom</h1>
        <p>Three milestones on the road to independence, calculated in today's dollars from your accounts and plan. Move the levers to see how each changes.</p>
      </div>
      <button class="btn accent" :disabled="!res" @click="snapshot"><Icon name="save" :size="17" /> Save snapshot</button>
    </div>

    <div v-if="loading || !res" class="grid cols-3"><div v-for="i in 3" :key="i" class="skeleton" style="height: 170px" /></div>
    <template v-else>
      <div class="grid cols-3 heroes">
        <button class="glass hover hero rise" :class="{ on: tab === 'crossover' }" @click="tab = 'crossover'">
          <div class="hl"><Icon name="trend" :size="18" /> Contribution Crossover</div>
          <div class="big"><template v-if="res.crossover.reached"><span class="small muted">age</span> <CountUp :value="res.crossover.age" format="years" /></template><template v-else>—</template></div>
          <div class="muted small">{{ res.crossover.reached ? (res.crossover.years_away === 0 ? 'You are already past it' : `in ${yrs(res.crossover.years_away)}`) : 'Not reached — try contributing more' }}</div>
          <div class="meter"><i :style="{ width: Math.min(100, res.crossover.ratio_now * 100) + '%' }" /></div>
          <div class="small faint">Growth covers {{ pct(res.crossover.ratio_now) }} of your yearly contributions</div>
        </button>
        <button class="glass hover hero rise" style="--i: 1" :class="{ on: tab === 'coast' }" @click="tab = 'coast'">
          <div class="hl"><Icon name="umbrella" :size="18" /> Coast FIRE</div>
          <div class="big"><template v-if="res.coast.coasting"><span class="chip good" style="font-size: 15px; height: 32px">Coasting now</span></template><template v-else-if="res.coast.coast_age"><span class="small muted">age</span> <CountUp :value="res.coast.coast_age" format="years" /></template><template v-else>—</template></div>
          <div class="muted small">{{ res.coast.coasting ? `You can stop saving — you're ahead by ${compact(res.inputs.investable - res.coast.coast_number_today)}` : res.coast.coast_age ? `in ${yrs(res.coast.years_to_coast)} · need ${compact(res.coast.coast_number_today)} today` : 'Not reached by retirement' }}</div>
          <div class="meter"><i :style="{ width: res.coast.progress * 100 + '%' }" /></div>
          <div class="small faint">{{ pct(res.coast.progress) }} of today's Coast number</div>
        </button>
        <button class="glass hover hero rise" style="--i: 2" :class="{ on: tab === 'freedom' }" @click="tab = 'freedom'">
          <div class="hl"><Icon name="spark" :size="18" /> Financial Freedom</div>
          <div class="big"><template v-if="res.freedom.fi_age"><span class="small muted">age</span> <CountUp :value="res.freedom.fi_age" format="years" /></template><template v-else>—</template></div>
          <div class="muted small">{{ res.freedom.fi_age ? (res.freedom.years_to_fi === 0 ? 'You have reached FI' : `in ${yrs(res.freedom.years_to_fi)} · ${res.freedom.fi_by_retirement ? 'before' : 'after'} your retirement age`) : 'Not reachable by age 100' }}</div>
          <div class="meter"><i :style="{ width: res.freedom.progress * 100 + '%' }" /></div>
          <div class="small faint">{{ pct(res.freedom.progress) }} of your {{ compact(res.freedom.fi_number) }} FI number</div>
        </button>
      </div>

      <div class="grid split" style="margin-top: 18px">
        <section class="glass pad rise" style="--i: 3">
          <div class="card-head">
            <div class="tabs" role="tablist"><button v-for="(t, id) in tabInfo" :key="id" class="tab" role="tab" :class="{ on: tab === id }" @click="tab = id as Tab">{{ t.title }}</button></div>
            <span v-if="calculating" class="spin" aria-label="Recalculating" />
          </div>
          <p class="small muted blurb">{{ tabInfo[tab].blurb }}</p>
          <LineChart :key="tab" :series="chart.series" :dots="chart.dots" :vlines="chart.vlines" :height="330" :x-format="(v: number) => (v === age0 ? `Age ${v}` : String(v))" />
          <div class="legend" style="margin-top: 10px"><span v-for="s in chart.series" :key="s.key"><i class="dot" :style="{ background: s.color }" />{{ s.label }}</span></div>

          <hr class="divider" />
          <!-- concept details -->
          <div v-if="tab === 'crossover'" class="facts">
            <div><span>Growth this year</span><b class="num">{{ money(res.crossover.growth_now) }}</b></div>
            <div><span>Contributions this year</span><b class="num">{{ money(res.crossover.contrib_now) }}</b></div>
            <div><span>Portfolio needed to cross now</span><b class="num">{{ compact(res.crossover.portfolio_needed_now) }}</b></div>
            <div><span>Portfolio at crossover</span><b class="num">{{ res.crossover.reached ? compact(res.crossover.portfolio_at_cross) : '—' }}</b></div>
          </div>
          <div v-else-if="tab === 'coast'" class="facts">
            <div><span>Coast number today</span><b class="num">{{ money(res.coast.coast_number_today) }}</b></div>
            <div><span>Invested today</span><b class="num">{{ money(res.inputs.investable) }}</b></div>
            <div><span>{{ res.coast.shortfall > 0 ? 'Still to go' : 'Ahead by' }}</span><b class="num" :class="res.coast.shortfall > 0 ? '' : 'pos'">{{ money(res.coast.shortfall > 0 ? res.coast.shortfall : res.inputs.investable - res.coast.coast_number_today) }}</b></div>
            <div><span>If you stopped saving, at {{ res.inputs.retirement_age }}</span><b class="num">{{ compact(res.coast.projected_if_stop_now) }} <small class="faint">/ {{ compact(res.coast.fi_number_at_retirement) }}</small></b></div>
            <div class="wide"><span>While coasting, earned income only needs to cover</span><b class="num">{{ money(res.coast.monthly_expenses_to_cover) }}/mo</b></div>
          </div>
          <div v-else class="facts">
            <div><span>FI number ({{ pct(res.inputs.safe_withdrawal_rate, 1) }} SWR)</span><b class="num">{{ money(res.freedom.fi_number) }}</b></div>
            <div><span>Lean / Fat FI</span><b class="num">{{ compact(res.freedom.lean_fi) }} / {{ compact(res.freedom.fat_fi) }}</b></div>
            <div><span>Passive income today</span><b class="num">{{ money(res.freedom.passive_monthly_now) }}/mo</b></div>
            <div><span>Still needed each month</span><b class="num">{{ money(res.freedom.monthly_gap) }}/mo</b></div>
            <div><span>Freedom ratio</span><b class="num">{{ pct(res.freedom.freedom_ratio) }}</b></div>
            <div><span>Savings rate</span><b class="num">{{ pct(res.freedom.savings_rate) }}</b></div>
          </div>
        </section>

        <aside class="glass pad rise" style="--i: 4">
          <div class="card-head"><h2><Icon name="sliders" :size="18" /> What-if levers</h2><button class="btn sm" :disabled="!changed" @click="reset">Reset</button></div>
          <div v-if="base" class="stack levers">
            <Slider v-model="inputs.retirement_age as number" label="Retirement age" :min="age0 + 1" :max="80" :step="1" :format="(v) => String(v)" />
            <Slider v-model="inputs.annual_contribution as number" label="Yearly contributions" :min="0" :max="Math.max(150000, base.annual_contribution * 2)" :step="1000" :format="money" />
            <Slider v-model="inputs.annual_spending as number" label="Retirement spending / yr" :min="10000" :max="Math.max(250000, base.annual_spending * 2)" :step="1000" :format="money" />
            <Slider v-model="inputs.return_rate as number" label="Expected return" :min="0.02" :max="0.12" :step="0.001" :format="(v) => pct(v, 1)" />
            <Slider v-model="inputs.safe_withdrawal_rate as number" label="Safe withdrawal rate" :min="0.025" :max="0.06" :step="0.0005" :format="(v) => pct(v, 2)" />
            <Slider v-model="inputs.inflation as number" label="Inflation" :min="0" :max="0.06" :step="0.001" :format="(v) => pct(v, 1)" />
            <Slider v-model="inputs.investable as number" label="Invested today" :min="0" :max="Math.max(1000000, base.investable * 2)" :step="5000" :format="money" />
          </div>
          <p class="small faint" style="margin-top: 16px">Defaults come from your accounts and profile. Changes here are for exploring and aren't saved unless you save a snapshot.</p>
          <div class="ringrow">
            <ProgressRing :value="res.freedom.progress" :size="96" :stroke="9" />
            <span class="small muted">Progress to FI at today's numbers</span>
          </div>
        </aside>
      </div>

      <section v-if="saved.length" class="glass pad rise" style="margin-top: 18px">
        <div class="card-head"><h2><Icon name="history" :size="18" /> Saved snapshots</h2></div>
        <div class="snaps">
          <div v-for="c in saved" :key="c.id" class="snap">
            <div><h3>{{ c.name }}</h3><small class="muted">{{ new Date(c.created_at).toLocaleString() }}</small></div>
            <span class="chip">Crossover {{ c.result?.crossover?.reached ? 'at ' + c.result.crossover.age : '—' }}</span>
            <span class="chip">Coast {{ c.result?.coast?.coasting ? 'now' : c.result?.coast?.coast_age ? 'at ' + c.result.coast.coast_age : '—' }}</span>
            <span class="chip good">FI {{ c.result?.freedom?.fi_age ? 'at ' + c.result.freedom.fi_age : '—' }}</span>
            <button class="btn sm" @click="loadSnap(c)">Load</button>
            <button class="btn icon sm danger" aria-label="Delete snapshot" @click="delSnap(c)"><Icon name="trash" :size="15" /></button>
          </div>
        </div>
      </section>
    </template>
  </main>
</template>

<style scoped>
.hero { text-align: left; padding: 22px; color: var(--text); font: inherit; cursor: pointer; display: grid; gap: 6px; }
.hero.on { border-color: var(--accent); box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.14), 0 0 0 1px var(--accent), 0 20px 50px -20px var(--accent-glow); }
.hl { display: flex; align-items: center; gap: 8px; font-weight: 700; color: var(--accent); font-size: 13.5px; }
.big { font-size: 46px; font-weight: 800; letter-spacing: -0.04em; line-height: 1.1; min-height: 52px; display: flex; align-items: baseline; gap: 8px; }
.meter { height: 7px; border-radius: 99px; background: rgba(255, 255, 255, 0.1); overflow: hidden; margin-top: 8px; }
.meter i { display: block; height: 100%; border-radius: 99px; background: linear-gradient(90deg, var(--c-moss), var(--accent)); transition: width 1s var(--ease); box-shadow: 0 0 12px var(--accent-glow); }
.blurb { margin: -4px 0 14px; max-width: 640px; }
.facts { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 2px 28px; }
.facts div { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; padding: 10px 0; border-bottom: 1px solid rgba(255, 255, 255, 0.06); font-size: 14px; }
.facts span { color: var(--muted); }
.facts .wide { grid-column: 1 / -1; }
.levers { gap: 20px; }
.ringrow { display: flex; align-items: center; gap: 16px; margin-top: 18px; padding-top: 16px; border-top: 1px solid var(--line); }
.snaps { display: grid; gap: 10px; }
.snap { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; padding: 12px 16px; border-radius: 16px; background: rgba(0, 0, 0, 0.16); border: 1px solid var(--line); }
.snap > div { flex: 1; min-width: 160px; }
@media (max-width: 680px) { .facts { grid-template-columns: 1fr; } .big { font-size: 38px; } }
</style>

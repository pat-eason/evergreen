<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { niceScale } from '@/lib/chart'
import { compact, money } from '@/lib/format'

export interface BarRow { label: number | string; parts: number[]; line?: number }
const props = withDefaults(defineProps<{ rows: BarRow[]; colors: string[]; names: string[]; height?: number; selected?: number | null; lineLabel?: string }>(), {
  height: 300, selected: null, lineLabel: 'Budgeted spending',
})
const emit = defineEmits<{ select: [index: number] }>()

const wrap = ref<HTMLElement>()
const W = ref(700)
let ro: ResizeObserver | undefined
const ready = ref(false)
onMounted(() => {
  W.value = wrap.value?.clientWidth || 700
  ro = new ResizeObserver(([e]) => { W.value = e.contentRect.width || W.value })
  if (wrap.value) ro.observe(wrap.value)
  requestAnimationFrame(() => requestAnimationFrame(() => (ready.value = true)))
})
onBeforeUnmount(() => ro?.disconnect())

const pad = { l: 6, r: 52, t: 12, b: 28 }
const innerW = computed(() => Math.max(10, W.value - pad.l - pad.r))
const innerH = computed(() => props.height - pad.t - pad.b)
const n = computed(() => Math.max(1, props.rows.length))
const slot = computed(() => innerW.value / n.value)
const barW = computed(() => Math.max(2, slot.value * 0.78))
const scale = computed(() => niceScale(Math.max(1, ...props.rows.map((r) => Math.max(r.parts.reduce((a, b) => a + b, 0), r.line ?? 0))) * 1.05, 4))
const sy = (v: number) => pad.t + innerH.value - (v / scale.value.max) * innerH.value

const bars = computed(() =>
  props.rows.map((r, i) => {
    let acc = 0
    const x = pad.l + i * slot.value + (slot.value - barW.value) / 2
    return {
      x,
      segs: r.parts.map((v, k) => {
        const y0 = sy(acc), y1 = sy(acc + v)
        acc += v
        return { k, y: y1, h: Math.max(0, y0 - y1) }
      }),
    }
  }),
)
const linePath = computed(() => {
  let d = ''
  props.rows.forEach((r, i) => {
    if (r.line == null) return
    const x0 = pad.l + i * slot.value, x1 = x0 + slot.value, y = sy(r.line)
    d += `${d ? ' L' : 'M'}${x0},${y} L${x1},${y}`
  })
  return d
})
const hover = ref<number | null>(null)
const tipLeft = computed(() => {
  if (hover.value == null) return 0
  const x = pad.l + hover.value * slot.value + slot.value / 2
  return x > W.value * 0.6 ? x - 196 : x + 12
})
const xLabels = computed(() => {
  const step = n.value > 40 ? 10 : n.value > 20 ? 5 : 2
  return props.rows.map((r, i) => ({ i, r })).filter(({ i }) => i % step === 0 || i === n.value - 1)
})
</script>

<template>
  <div ref="wrap" class="sb" :style="{ height: height + 'px' }">
    <svg :width="W" :height="height" role="img" aria-label="Retirement income sources by year">
      <g>
        <g v-for="t in scale.ticks" :key="t">
          <line :x1="pad.l" :x2="W - pad.r" :y1="sy(t)" :y2="sy(t)" stroke="rgba(255,255,255,.08)" stroke-dasharray="2 5" />
          <text :x="W - pad.r + 8" :y="sy(t) + 4" class="ax">{{ compact(t) }}</text>
        </g>
        <text v-for="{ i, r } in xLabels" :key="i" :x="pad.l + i * slot + slot / 2" :y="height - 8" class="ax" text-anchor="middle">{{ r.label }}</text>
      </g>
      <g v-for="(b, i) in bars" :key="i" class="bar" :class="{ sel: selected === i, dim: hover != null && hover !== i }" @mouseenter="hover = i" @mouseleave="hover = null" @click="emit('select', i)">
        <rect :x="pad.l + i * slot" :y="pad.t" :width="slot" :height="innerH" fill="transparent" />
        <rect
          v-for="s in b.segs" :key="s.k" :x="b.x" :y="s.y" :width="barW" :height="s.h" :fill="colors[s.k]" rx="2"
          :style="{ transform: ready ? 'scaleY(1)' : 'scaleY(0)', transitionDelay: i * 14 + 'ms' }"
        />
      </g>
      <path :d="linePath" fill="none" stroke="#f5f0e8" stroke-width="2" stroke-linejoin="round" :style="{ opacity: ready ? 0.9 : 0 }" class="budget" />
    </svg>
    <div v-if="hover != null" class="tip glass" :style="{ left: tipLeft + 'px' }">
      <div class="tt">{{ rows[hover].label }}</div>
      <div v-for="(nm, k) in names" :key="nm" v-show="rows[hover].parts[k] > 0" class="tr"><i class="dot" :style="{ background: colors[k] }" /> <span>{{ nm }}</span><b class="num">{{ money(rows[hover].parts[k]) }}</b></div>
      <div v-if="rows[hover].line != null" class="tr line"><i class="dot ln" /> <span>{{ lineLabel }}</span><b class="num">{{ money(rows[hover].line!) }}</b></div>
    </div>
  </div>
</template>

<style scoped>
.sb { position: relative; width: 100%; }
svg { display: block; overflow: visible; }
.ax { fill: var(--muted); font-size: 11.5px; font-weight: 500; }
.bar { cursor: pointer; transition: opacity 0.2s; }
.bar rect[rx] { transform-box: fill-box; transform-origin: bottom; transition: transform 0.9s cubic-bezier(0.22, 1, 0.36, 1); }
.bar.dim { opacity: 0.55; }
.bar.sel rect[rx] { filter: drop-shadow(0 0 6px rgba(205, 247, 61, 0.55)); }
.budget { transition: opacity 0.8s 0.9s; }
.tip { position: absolute; top: 8px; width: 186px; padding: 10px 12px; border-radius: 14px; pointer-events: none; background: rgba(24, 8, 10, 0.9); z-index: 3; }
.tt { font-weight: 700; margin-bottom: 6px; font-size: 13px; }
.tr { display: flex; align-items: center; gap: 7px; font-size: 12px; color: var(--muted); padding: 2px 0; }
.tr b { margin-left: auto; color: var(--text); }
.ln { background: #f5f0e8; height: 3px; border-radius: 2px; width: 12px; }
</style>

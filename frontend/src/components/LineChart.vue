<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { monotonePath, nearestIndex, niceBounds, niceRange, niceScale, type Pt } from '@/lib/chart'
import { compact } from '@/lib/format'
import { useTween } from '@/lib/anim'

export interface Series { key: string; label: string; color: string; points: Pt[]; area?: boolean; dashed?: boolean; width?: number; fill?: number }
export interface VLine { x: number; label: string; color?: string }
export interface Dot { x: number; y: number; label: string; color?: string }
export interface TipRow { key: string; label: string; color: string; y: number }
export interface TipExtra { label: string; value: string; tone?: 'pos' | 'neg' }

const props = withDefaults(defineProps<{
  series: Series[]
  height?: number
  vlines?: VLine[]
  dots?: Dot[]
  yFormat?: (v: number) => string
  xFormat?: (v: number) => string
  xTicks?: number[]
  yMax?: number
  /** false zooms the y axis to the data instead of anchoring at zero (good for timelines of large balances) */
  fromZero?: boolean
  /** drag across the plot to pick an x range; emits `select` with data coordinates */
  selectable?: boolean
  /** extra summary rows shown under the series values in the hover tooltip (e.g. a total) */
  tooltipExtra?: (rows: TipRow[]) => TipExtra[]
}>(), {
  fromZero: true, height: 320, vlines: () => [], dots: () => [], yFormat: compact, xFormat: (v: number) => String(v),
})

const emit = defineEmits<{ select: [from: number, to: number] }>()

const uid = useId().replace(/[^a-zA-Z0-9]/g, '')
const wrap = ref<HTMLElement>()
const W = ref(640)
let ro: ResizeObserver | undefined
onMounted(() => {
  W.value = wrap.value?.clientWidth || 640
  ro = new ResizeObserver(([e]) => { W.value = e.contentRect.width || W.value })
  if (wrap.value) ro.observe(wrap.value)
  requestAnimationFrame(() => (go.value = 1))
})
onBeforeUnmount(() => ro?.disconnect())

const go = ref(0)
const reveal = useTween(go, 1400)

const pad = { l: 6, r: 52, t: 14, b: 30 }
const innerW = computed(() => Math.max(10, W.value - pad.l - pad.r))
const innerH = computed(() => props.height - pad.t - pad.b)

const xs = computed(() => {
  const all = props.series.flatMap((s) => s.points.map((p) => p.x))
  return { min: Math.min(...all, Infinity) === Infinity ? 0 : Math.min(...all), max: Math.max(...all, -Infinity) === -Infinity ? 1 : Math.max(...all) }
})
const yScale = computed(() => {
  const vals = props.series.flatMap((s) => s.points.map((p) => p.y))
  if (!props.fromZero && vals.length) {
    const mn = Math.min(...vals), mx = Math.max(...vals)
    const pad = (mx - mn || Math.abs(mx) * 0.1 || 1) * 0.12
    return niceBounds(mn - pad, mx + pad)
  }
  const lo = Math.min(0, ...vals), hi = Math.max(0, ...vals)
  if (props.yMax && lo >= 0) { const s = niceScale(props.yMax); return { min: 0, max: s.max, ticks: s.ticks } }
  return niceRange(lo * (lo < 0 ? 1.04 : 1), hi * 1.04)
})
const sx = (x: number) => pad.l + ((x - xs.value.min) / (xs.value.max - xs.value.min || 1)) * innerW.value
const sy = (y: number) => pad.t + innerH.value - ((y - yScale.value.min) / (yScale.value.max - yScale.value.min || 1)) * innerH.value

const paths = computed(() =>
  props.series.map((s) => {
    const pts = s.points.map((p) => ({ x: sx(p.x), y: sy(p.y) }))
    const line = monotonePath(pts)
    const base = sy(Math.max(0, yScale.value.min)) // fill down to zero, or to the floor when zoomed
    const area = pts.length ? `${line} L${pts[pts.length - 1].x},${base} L${pts[0].x},${base} Z` : ''
    return { line, area }
  }),
)

const xTickList = computed(() => {
  if (props.xTicks) return props.xTicks
  const { min, max } = xs.value
  const span = max - min
  const step = span <= 12 ? 1 : span <= 30 ? 5 : span <= 60 ? 10 : 20
  const out: number[] = []
  for (let v = Math.ceil(min / step) * step; v <= max; v += step) out.push(v)
  if (!out.includes(min)) {
    out.unshift(min)
    // drop the next tick if it would collide with the first label (e.g. ages 38 and 40)
    if (out.length > 1 && out[1] - out[0] < step * 0.6) out.splice(1, 1)
  }
  return out
})

// hover
const hoverX = ref<number | null>(null)
const baseXs = computed(() => (props.series[0]?.points ?? []).map((p) => p.x))
const hoverIdx = computed(() => {
  if (hoverX.value == null || !baseXs.value.length) return null
  const dataX = xs.value.min + ((hoverX.value - pad.l) / innerW.value) * (xs.value.max - xs.value.min)
  return nearestIndex(baseXs.value, dataX)
})
const hoverData = computed(() => {
  const i = hoverIdx.value
  if (i == null) return null
  const x = baseXs.value[i]
  return {
    x,
    rows: props.series.map((s) => {
      const p = s.points[nearestIndex(s.points.map((q) => q.x), x)]
      return { key: s.key, label: s.label, color: s.color, y: p?.y ?? 0 }
    }),
  }
})
const hoverExtra = computed<TipExtra[]>(() => (hoverData.value && props.tooltipExtra ? props.tooltipExtra(hoverData.value.rows) : []))

// ---- drag to select an x range ----
const drag = ref<{ a: number; b: number } | null>(null)
const clampPx = (px: number) => Math.min(pad.l + innerW.value, Math.max(pad.l, px))
const dataAt = (px: number) => xs.value.min + ((px - pad.l) / innerW.value) * (xs.value.max - xs.value.min)
const localX = (e: PointerEvent) => e.clientX - (e.currentTarget as SVGElement).getBoundingClientRect().left
const localY = (e: PointerEvent) => e.clientY - (e.currentTarget as SVGElement).getBoundingClientRect().top

function onDown(e: PointerEvent) {
  if (!props.selectable || e.button !== 0 || xs.value.max <= xs.value.min) return
  const y = localY(e)
  if (y < pad.t || y > pad.t + innerH.value) return
  const px = clampPx(localX(e))
  drag.value = { a: px, b: px }
  ;(e.currentTarget as SVGElement).setPointerCapture(e.pointerId)
}
function onMove(e: PointerEvent) {
  const x = localX(e)
  hoverX.value = x
  if (drag.value) drag.value.b = clampPx(x)
}
function finishDrag(e: PointerEvent, commit: boolean) {
  const d = drag.value
  drag.value = null
  try { (e.currentTarget as SVGElement).releasePointerCapture(e.pointerId) } catch { /* not captured */ }
  if (!d || !commit || Math.abs(d.b - d.a) < 8) return // a click, not a drag
  const lo = Math.min(d.a, d.b), hi = Math.max(d.a, d.b)
  emit('select', dataAt(lo), dataAt(hi))
}
const onUp = (e: PointerEvent) => finishDrag(e, true)
const onCancel = (e: PointerEvent) => finishDrag(e, false)
function onLeave() { if (!drag.value) hoverX.value = null }

const sel = computed(() => {
  const d = drag.value
  if (!d) return null
  const x0 = Math.min(d.a, d.b), x1 = Math.max(d.a, d.b)
  return { x: x0, w: x1 - x0, from: Math.round(dataAt(x0)), to: Math.round(dataAt(x1)) }
})

const tipLeft = computed(() => {
  if (!hoverData.value) return 0
  const x = sx(hoverData.value.x)
  return x > W.value * 0.62 ? x - 214 : x + 14
})
</script>

<template>
  <div ref="wrap" class="lc" :style="{ height: height + 'px' }">
    <svg :width="W" :height="height" role="img" :class="{ pick: selectable }" @pointerdown="onDown" @pointermove="onMove" @pointerup="onUp" @pointercancel="onCancel" @pointerleave="onLeave">
      <defs>
        <linearGradient v-for="(s, i) in series" :key="s.key" :id="`${uid}g${i}`" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" :stop-color="s.color" :stop-opacity="s.fill ?? 0.55" />
          <stop offset="100%" :stop-color="s.color" stop-opacity="0.04" />
        </linearGradient>
        <clipPath :id="`${uid}c`"><rect x="0" y="0" :width="Math.max(0, reveal * W)" :height="height" /></clipPath>
      </defs>

      <g class="grid">
        <g v-for="t in yScale.ticks" :key="t">
          <line :x1="pad.l" :x2="W - pad.r" :y1="sy(t)" :y2="sy(t)" stroke="rgba(255,255,255,.08)" stroke-dasharray="2 5" />
          <text :x="W - pad.r + 8" :y="sy(t) + 4" class="ax">{{ yFormat(t) }}</text>
        </g>
        <text v-for="t in xTickList" :key="t" :x="sx(t)" :y="height - 8" class="ax" :text-anchor="t === xs.min ? 'start' : t === xs.max ? 'end' : 'middle'">{{ xFormat(t) }}</text>
      </g>

      <g :clip-path="`url(#${uid}c)`">
        <template v-for="(s, i) in series" :key="s.key">
          <path v-if="s.area" :d="paths[i].area" :fill="`url(#${uid}g${i})`" />
          <path :d="paths[i].line" fill="none" :stroke="s.color" :stroke-width="s.width ?? 2.5" stroke-linecap="round" stroke-linejoin="round" :stroke-dasharray="s.dashed ? '6 6' : undefined"
            :style="{ filter: s.area ? 'none' : `drop-shadow(0 0 6px ${s.color}66)` }" />
        </template>
      </g>

      <g v-for="v in vlines" :key="v.label + v.x" :style="{ opacity: reveal > 0.6 ? 1 : 0, transition: 'opacity .6s' }">
        <line :x1="sx(v.x)" :x2="sx(v.x)" :y1="pad.t" :y2="pad.t + innerH" :stroke="v.color ?? 'rgba(255,255,255,.55)'" stroke-width="1.5" stroke-dasharray="4 4" />
        <text :x="sx(v.x) + 6" :y="pad.t + 11" class="vl" :fill="v.color ?? 'rgba(255,255,255,.8)'">{{ v.label }}</text>
      </g>

      <g v-for="d in dots" :key="d.label" :style="{ opacity: reveal > 0.85 ? 1 : 0, transition: 'opacity .6s' }">
        <circle :cx="sx(d.x)" :cy="sy(d.y)" r="11" :fill="d.color ?? 'var(--accent)'" opacity=".22"><animate attributeName="r" values="8;15;8" dur="2.6s" repeatCount="indefinite" /></circle>
        <circle :cx="sx(d.x)" :cy="sy(d.y)" r="5.5" :fill="d.color ?? 'var(--accent)'" stroke="#250104" stroke-width="2" />
      </g>

      <g v-if="sel" class="selection">
        <rect :x="sel.x" :y="pad.t" :width="sel.w" :height="innerH" fill="var(--accent)" opacity="0.14" />
        <line :x1="sel.x" :x2="sel.x" :y1="pad.t" :y2="pad.t + innerH" stroke="var(--accent)" stroke-width="1.5" />
        <line :x1="sel.x + sel.w" :x2="sel.x + sel.w" :y1="pad.t" :y2="pad.t + innerH" stroke="var(--accent)" stroke-width="1.5" />
        <text :x="sel.x + sel.w / 2" :y="pad.t + 14" text-anchor="middle" class="sellabel">{{ xFormat(sel.from) }} – {{ xFormat(sel.to) }}</text>
      </g>

      <g v-if="hoverData && !drag">
        <line :x1="sx(hoverData.x)" :x2="sx(hoverData.x)" :y1="pad.t" :y2="pad.t + innerH" stroke="rgba(255,255,255,.5)" />
        <circle v-for="r in hoverData.rows" :key="r.label" :cx="sx(hoverData.x)" :cy="sy(r.y)" r="5" :fill="r.color" stroke="#fff" stroke-width="1.5" />
      </g>
      <rect :x="pad.l" :y="pad.t" :width="innerW" :height="innerH" fill="transparent" />
    </svg>
    <div v-if="hoverData && !drag" class="tip glass" :style="{ left: tipLeft + 'px' }">
      <div class="tt">{{ xFormat(hoverData.x) }}</div>
      <div v-for="r in hoverData.rows" :key="r.key" class="tr"><i class="dot" :style="{ background: r.color }" /> <span>{{ r.label }}</span><b class="num">{{ yFormat(r.y) }}</b></div>
      <template v-if="hoverExtra.length">
        <div class="tsep" />
        <div v-for="x in hoverExtra" :key="x.label" class="tr total"><span>{{ x.label }}</span><b class="num" :class="x.tone">{{ x.value }}</b></div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.lc { position: relative; width: 100%; }
svg { display: block; overflow: visible; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
svg.pick { cursor: crosshair; }
.sellabel { fill: var(--text); font-size: 12px; font-weight: 700; paint-order: stroke; stroke: rgba(24, 8, 10, 0.85); stroke-width: 4px; }
.tsep { height: 1px; background: var(--line); margin: 6px 0 4px; }
.tr.total { color: var(--text); font-weight: 600; }
.tr.total b.pos { color: var(--accent); }
.tr.total b.neg { color: var(--danger); }
.ax { fill: var(--muted); font-size: 11.5px; font-weight: 500; }
.vl { font-size: 11.5px; font-weight: 700; }
.tip { position: absolute; top: 10px; width: 200px; padding: 10px 12px; border-radius: 14px; pointer-events: none; background: rgba(24, 8, 10, 0.88); z-index: 3; }
.tt { font-weight: 700; margin-bottom: 6px; font-size: 13px; }
.tr { display: flex; align-items: center; gap: 7px; font-size: 12px; color: var(--muted); padding: 2px 0; }
.tr b { margin-left: auto; color: var(--text); }
</style>

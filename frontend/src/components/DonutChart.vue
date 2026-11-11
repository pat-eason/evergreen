<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { money, pct } from '@/lib/format'

export interface Segment { label: string; value: number; color: string }
const props = withDefaults(defineProps<{ segments: Segment[]; size?: number; thickness?: number; centerLabel?: string; centerValue?: string }>(), {
  size: 180, thickness: 22,
})
const ready = ref(false)
onMounted(() => requestAnimationFrame(() => requestAnimationFrame(() => (ready.value = true))))
const hover = ref<number | null>(null)

const total = computed(() => props.segments.reduce((s, x) => s + Math.max(0, x.value), 0))
const r = computed(() => (props.size - props.thickness) / 2 - 2)
const C = computed(() => 2 * Math.PI * r.value)
const arcs = computed(() => {
  let acc = 0
  const gap = props.segments.filter((s) => s.value > 0).length > 1 ? 3 : 0
  return props.segments.map((s) => {
    const frac = total.value > 0 ? Math.max(0, s.value) / total.value : 0
    const len = Math.max(0, frac * C.value - gap)
    const start = acc * C.value
    acc += frac
    return { len, start, frac }
  })
})
const active = computed(() => (hover.value != null ? props.segments[hover.value] : null))
</script>

<template>
  <div class="donut" :style="{ width: size + 'px', height: size + 'px' }">
    <svg :width="size" :height="size" :viewBox="`0 0 ${size} ${size}`" role="img" :aria-label="'Breakdown chart'">
      <circle :cx="size / 2" :cy="size / 2" :r="r" fill="none" stroke="rgba(255,255,255,.07)" :stroke-width="thickness" />
      <g :transform="`rotate(-90 ${size / 2} ${size / 2})`">
        <circle
          v-for="(s, i) in segments" :key="s.label + i"
          :cx="size / 2" :cy="size / 2" :r="r" fill="none" :stroke="s.color"
          :stroke-width="hover === i ? thickness + 6 : thickness" stroke-linecap="butt"
          :stroke-dasharray="`${ready ? arcs[i].len : 0} ${C}`" :stroke-dashoffset="-arcs[i].start"
          :style="{ transition: `stroke-dasharray 1.1s cubic-bezier(.22,1,.36,1) ${i * 90}ms, stroke-width .25s`, opacity: hover == null || hover === i ? 1 : 0.4, cursor: 'pointer' }"
          @mouseenter="hover = i" @mouseleave="hover = null"
        />
      </g>
    </svg>
    <div class="mid">
      <template v-if="active">
        <small>{{ active.label }}</small>
        <strong>{{ money(active.value) }}</strong>
        <small>{{ pct(total ? active.value / total : 0, 1) }}</small>
      </template>
      <template v-else>
        <small>{{ centerLabel }}</small>
        <strong>{{ centerValue }}</strong>
      </template>
    </div>
  </div>
</template>

<style scoped>
.donut { position: relative; flex: none; }
svg { display: block; }
.mid { position: absolute; inset: 0; display: grid; place-content: center; text-align: center; gap: 1px; pointer-events: none; padding: 0 26px; }
.mid small { color: var(--muted); font-size: 11.5px; font-weight: 600; }
.mid strong { font-size: 19px; letter-spacing: -0.02em; }
</style>

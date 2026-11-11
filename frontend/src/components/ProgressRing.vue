<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import CountUp from './CountUp.vue'

const props = withDefaults(defineProps<{ value: number; display?: number; size?: number; stroke?: number; showValue?: boolean; color?: string }>(), {
  size: 150, stroke: 12, showValue: true, color: 'var(--accent)',
})
const r = computed(() => (props.size - props.stroke) / 2)
const c = computed(() => 2 * Math.PI * r.value)
const ready = ref(false)
onMounted(() => requestAnimationFrame(() => requestAnimationFrame(() => (ready.value = true))))
const offset = computed(() => c.value * (1 - (ready.value ? Math.max(0, Math.min(1, props.value)) : 0)))
</script>

<template>
  <div class="ring" :style="{ width: size + 'px', height: size + 'px' }">
    <svg :width="size" :height="size" :viewBox="`0 0 ${size} ${size}`">
      <circle :cx="size / 2" :cy="size / 2" :r="r" fill="none" stroke="rgba(255,255,255,.1)" :stroke-width="stroke" />
      <circle
        :cx="size / 2" :cy="size / 2" :r="r" fill="none" :stroke="color" :stroke-width="stroke" stroke-linecap="round"
        :stroke-dasharray="c" :stroke-dashoffset="offset" :transform="`rotate(-90 ${size / 2} ${size / 2})`"
        style="transition: stroke-dashoffset 1.4s cubic-bezier(.22,1,.36,1); filter: drop-shadow(0 0 8px var(--accent-glow))"
      />
    </svg>
    <div class="mid">
      <slot>
        <strong v-if="showValue"><CountUp :value="display ?? value" format="pct" /></strong>
      </slot>
    </div>
  </div>
</template>

<style scoped>
.ring { position: relative; flex: none; }
.ring svg { display: block; }
.mid { position: absolute; inset: 0; display: grid; place-items: center; text-align: center; }
.mid strong { font-size: 30px; letter-spacing: -0.03em; }
</style>

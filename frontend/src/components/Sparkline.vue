<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
const props = withDefaults(defineProps<{ values: number[]; width?: number; height?: number; color?: string }>(), { width: 120, height: 36, color: 'var(--accent)' })
const ready = ref(false)
onMounted(() => requestAnimationFrame(() => (ready.value = true)))
const d = computed(() => {
  const v = props.values
  if (v.length < 2) return ''
  const min = Math.min(...v), max = Math.max(...v), span = max - min || 1
  return v.map((y, i) => `${i ? 'L' : 'M'}${(i / (v.length - 1)) * props.width},${props.height - 3 - ((y - min) / span) * (props.height - 6)}`).join(' ')
})
</script>
<template>
  <svg :width="width" :height="height" :viewBox="`0 0 ${width} ${height}`" aria-hidden="true">
    <path :d="d" fill="none" :stroke="color" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" pathLength="1"
      :style="{ strokeDasharray: 1, strokeDashoffset: ready ? 0 : 1, transition: 'stroke-dashoffset 1.2s cubic-bezier(.22,1,.36,1)' }" />
  </svg>
</template>

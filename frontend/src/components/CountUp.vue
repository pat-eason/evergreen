<script setup lang="ts">
import { computed, toRef } from 'vue'
import { useTween } from '@/lib/anim'
import { compact, money, pct } from '@/lib/format'

const props = withDefaults(defineProps<{ value: number; format?: 'money' | 'compact' | 'pct' | 'int' | 'years'; digits?: number; duration?: number }>(), {
  format: 'money', digits: 0, duration: 900,
})
const shown = useTween(toRef(props, 'value'), props.duration)
const text = computed(() => {
  const v = shown.value
  switch (props.format) {
    case 'compact': return compact(v)
    case 'pct': return pct(v, props.digits)
    case 'int': return Math.round(v).toLocaleString('en-US')
    case 'years': return `${Math.round(v)}`
    default: return money(v)
  }
})
</script>

<template><span class="num">{{ text }}</span></template>

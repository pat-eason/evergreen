<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ modelValue: number; min: number; max: number; step: number; label: string; format: (v: number) => string }>()
const emit = defineEmits<{ 'update:modelValue': [v: number] }>()
const clamped = computed(() => Math.min(props.max, Math.max(props.min, props.modelValue)))
const fill = computed(() => `${((clamped.value - props.min) / (props.max - props.min || 1)) * 100}%`)
</script>
<template>
  <label class="sl">
    <span class="top"><span>{{ label }}</span><b class="num">{{ format(modelValue) }}</b></span>
    <input type="range" :min="min" :max="max" :step="step" :value="clamped" :style="{ '--fill': fill }" @input="emit('update:modelValue', +($event.target as HTMLInputElement).value)" />
  </label>
</template>
<style scoped>
.sl { display: grid; gap: 10px; }
.top { display: flex; justify-content: space-between; font-size: 13px; color: var(--muted); font-weight: 600; }
.top b { color: var(--text); }
</style>

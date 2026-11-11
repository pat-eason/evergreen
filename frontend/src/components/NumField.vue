<script setup lang="ts">
import { ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: number | undefined
  label: string
  kind?: 'money' | 'pct' | 'int' | 'num'
  hint?: string
  suffix?: string
  placeholder?: string
  full?: boolean
}>(), { kind: 'num' })
const emit = defineEmits<{ 'update:modelValue': [v: number] }>()

const fmt = (v: number | undefined): string => {
  if (v == null || Number.isNaN(v)) return ''
  const x = props.kind === 'pct' ? Math.round(v * 1e6) / 1e4 : v
  return x === 0 ? '' : String(x)
}
const parse = (t: string): number => {
  const n = parseFloat(t.replace(/[$,%\s]/g, ''))
  if (Number.isNaN(n)) return 0
  return props.kind === 'pct' ? n / 100 : n
}
const text = ref(fmt(props.modelValue))
watch(() => props.modelValue, (v) => { if (Math.abs(parse(text.value) - (v ?? 0)) > 1e-9) text.value = fmt(v) })
function onInput(e: Event) {
  text.value = (e.target as HTMLInputElement).value
  emit('update:modelValue', props.kind === 'int' ? Math.round(parse(text.value)) : parse(text.value))
}
</script>

<template>
  <label class="field" :class="{ full }">
    <span>{{ label }} <em v-if="hint" class="hint">{{ hint }}</em></span>
    <div class="input-wrap" :class="{ 'has-l': kind === 'money', 'has-r': kind === 'pct' || suffix }">
      <i v-if="kind === 'money'" class="adorn l">$</i>
      <input class="input num" inputmode="decimal" :value="text" :placeholder="placeholder ?? '0'" @input="onInput" />
      <i v-if="kind === 'pct'" class="adorn r">%</i>
      <i v-else-if="suffix" class="adorn r">{{ suffix }}</i>
    </div>
  </label>
</template>

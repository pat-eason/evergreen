<script setup lang="ts">
import { ref } from 'vue'
import Icon from './Icon.vue'

defineProps<{ modelValue: string; label: string; autocomplete: string; minlength?: number; maxlength?: number }>()
const emit = defineEmits<{ 'update:modelValue': [v: string] }>()
const visible = ref(false)
</script>

<template>
  <label class="field">
    <span>{{ label }}</span>
    <div class="input-wrap has-r">
      <input
        class="input" :type="visible ? 'text' : 'password'" :value="modelValue" :autocomplete="autocomplete"
        :minlength="minlength" :maxlength="maxlength" required spellcheck="false" autocapitalize="off"
        @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      />
      <button type="button" class="eye" :aria-label="visible ? 'Hide password' : 'Show password'" :aria-pressed="visible" @click="visible = !visible">
        <Icon :name="visible ? 'eye-off' : 'eye'" :size="18" />
      </button>
    </div>
  </label>
</template>

<style scoped>
.input-wrap.has-r .input { padding-right: 46px; }
.eye { position: absolute; right: 6px; top: 50%; transform: translateY(-50%); width: 34px; height: 34px; display: grid; place-items: center; border: 0; border-radius: 50%; background: transparent; color: var(--muted); cursor: pointer; transition: color 0.2s, background 0.2s; }
.eye:hover { color: var(--text); background: rgba(255, 255, 255, 0.08); }
.eye:focus-visible { outline: 2px solid var(--accent); }
</style>

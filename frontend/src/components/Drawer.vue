<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import Icon from './Icon.vue'

const props = defineProps<{ open: boolean; title: string; width?: number }>()
const emit = defineEmits<{ close: [] }>()
const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') emit('close') }
watch(() => props.open, (o) => {
  if (o) { window.addEventListener('keydown', onKey); document.body.style.overflow = 'hidden' }
  else { window.removeEventListener('keydown', onKey); document.body.style.overflow = '' }
}, { immediate: true })
onBeforeUnmount(() => { window.removeEventListener('keydown', onKey); document.body.style.overflow = '' })
</script>

<template>
  <Teleport to="body">
    <Transition name="dr">
      <div v-if="open" class="scrim" @mousedown.self="emit('close')">
        <aside class="panel glass" role="dialog" aria-modal="true" :aria-label="title" :style="{ maxWidth: (width ?? 520) + 'px' }">
          <header>
            <h2>{{ title }}</h2>
            <button class="btn icon" aria-label="Close" @click="emit('close')"><Icon name="close" :size="18" /></button>
          </header>
          <div class="body"><slot /></div>
          <footer v-if="$slots.footer"><slot name="footer" /></footer>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.scrim { position: fixed; inset: 0; z-index: 60; background: rgba(7, 2, 3, 0.55); backdrop-filter: blur(6px); display: flex; justify-content: flex-end; padding: 14px; }
.panel { width: 100%; height: 100%; display: flex; flex-direction: column; background: linear-gradient(160deg, rgba(60, 12, 18, 0.82), rgba(10, 28, 12, 0.86)); border-radius: 26px; overflow: hidden; }
header { display: flex; align-items: center; justify-content: space-between; padding: 20px 22px 10px; }
.body { flex: 1; overflow: auto; padding: 12px 22px 22px; }
footer { display: flex; gap: 10px; justify-content: flex-end; padding: 16px 22px; border-top: 1px solid var(--line); background: rgba(0, 0, 0, 0.18); }
.dr-enter-active, .dr-leave-active { transition: opacity 0.3s ease; }
.dr-enter-active .panel, .dr-leave-active .panel { transition: transform 0.45s cubic-bezier(0.22, 1, 0.36, 1); }
.dr-enter-from, .dr-leave-to { opacity: 0; }
.dr-enter-from .panel, .dr-leave-to .panel { transform: translateX(40px); }
@media (max-width: 600px) { .scrim { padding: 0; } .panel { border-radius: 0; max-width: none !important; } }
</style>

<script setup lang="ts">
import { watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppShell from '@/components/AppShell.vue'
import { state } from '@/lib/store'

const route = useRoute()
const router = useRouter()
// If the session expires while using the app, bounce to login.
watch(() => state.user, (u) => { if (!u && !route.meta.public) router.push({ name: 'login' }) })
</script>

<template>
  <div class="orbs" aria-hidden="true"><i /><i /><i /></div>
  <AppShell v-if="state.user && !route.meta.public" />
  <RouterView v-slot="{ Component }">
    <Transition name="page" mode="out-in">
      <component :is="Component" :key="route.path" />
    </Transition>
  </RouterView>
  <div class="toasts" aria-live="polite">
    <div v-for="t in state.toasts" :key="t.id" class="toast" :class="t.kind">{{ t.text }}</div>
  </div>
</template>

<style>
.page-enter-active, .page-leave-active { transition: opacity 0.28s ease, transform 0.4s cubic-bezier(0.22, 1, 0.36, 1); }
.page-enter-from { opacity: 0; transform: translateY(14px); }
.page-leave-to { opacity: 0; transform: translateY(-6px); }
</style>

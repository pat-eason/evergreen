<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppShell from '@/components/AppShell.vue'
import { state, ui } from '@/lib/store'

const route = useRoute()
const router = useRouter()
const showNav = computed(() => !!state.user && !route.meta.public)
// If the session expires while using the app, bounce to login.
watch(() => state.user, (u) => { if (!u && !route.meta.public) router.push({ name: 'login' }) })
</script>

<template>
  <div class="orbs" aria-hidden="true"><i /><i /><i /></div>
  <div class="shell" :class="{ 'has-nav': showNav, collapsed: ui.navCollapsed }">
    <AppShell v-if="showNav" />
    <RouterView v-slot="{ Component }">
      <Transition name="page" mode="out-in">
        <component :is="Component" :key="route.path" />
      </Transition>
    </RouterView>
  </div>
  <div class="toasts" aria-live="polite">
    <div v-for="t in state.toasts" :key="t.id" class="toast" :class="t.kind">{{ t.text }}</div>
  </div>
</template>

<style>
/* Wide viewports (tablet landscape and up): the nav is a fixed left sidebar. */
@media (min-width: 1024px) {
  .shell.has-nav { padding-left: 276px; transition: padding-left 0.35s cubic-bezier(0.22, 1, 0.36, 1); }
  .shell.has-nav.collapsed { padding-left: 108px; }
}
.page-enter-active, .page-leave-active { transition: opacity 0.28s ease, transform 0.4s cubic-bezier(0.22, 1, 0.36, 1); }
.page-enter-from { opacity: 0; transform: translateY(14px); }
.page-leave-to { opacity: 0; transform: translateY(-6px); }
</style>

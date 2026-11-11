<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Icon from './Icon.vue'
import AccountSettings from './AccountSettings.vue'
import ProfileForm from './ProfileForm.vue'
import { errMsg, loadData, signOut, state, toast } from '@/lib/store'

const router = useRouter()
const links = [
  { to: '/', label: 'Dashboard', icon: 'home' },
  { to: '/accounts', label: 'Accounts', icon: 'wallet' },
  { to: '/budget', label: 'Budget', icon: 'calendar' },
  { to: '/plan', label: 'Plan', icon: 'chart' },
  { to: '/withdrawal', label: 'Smart Withdrawal', icon: 'layers' },
  { to: '/freedom', label: 'Freedom', icon: 'spark' },
]
const menuOpen = ref(false)
const profileOpen = ref(false)
const accountOpen = ref(false)
async function openProfile() {
  menuOpen.value = false
  try { await loadData(); profileOpen.value = true } catch (e) { toast(errMsg(e), 'err') }
}
function openAccount() { menuOpen.value = false; accountOpen.value = true }
const menuEl = ref<HTMLElement>()
async function out() {
  menuOpen.value = false
  await signOut()
  router.push('/login')
}
const onDoc = (e: MouseEvent) => { if (menuOpen.value && !menuEl.value?.contains(e.target as Node)) menuOpen.value = false }
const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') menuOpen.value = false }
onMounted(() => { document.addEventListener('mousedown', onDoc); document.addEventListener('keydown', onKey) })
onBeforeUnmount(() => { document.removeEventListener('mousedown', onDoc); document.removeEventListener('keydown', onKey) })
</script>

<template>
  <header class="top">
    <div class="bar glass">
      <RouterLink to="/" class="brand" aria-label="Evergreen home">
        <svg width="30" height="30" viewBox="0 0 32 32"><rect width="32" height="32" rx="10" fill="var(--accent)" /><path d="M8 21l5-6 4 3 7-9" fill="none" stroke="#250104" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" /></svg>
        <span>Evergreen</span>
      </RouterLink>
      <nav class="nav" aria-label="Main">
        <RouterLink v-for="l in links" :key="l.to" :to="l.to" :title="l.label">
          <Icon :name="l.icon" :size="18" /><span>{{ l.label }}</span>
        </RouterLink>
      </nav>
      <div ref="menuEl" class="user">
        <button class="who" :aria-expanded="menuOpen" aria-haspopup="menu" aria-label="Account menu" @click="menuOpen = !menuOpen">
          <span class="avatar" aria-hidden="true">{{ state.user?.email?.[0]?.toUpperCase() }}</span>
          <span class="uname">{{ state.user?.email }}</span>
          <Icon name="chevron" :size="16" class="chev" :class="{ up: menuOpen }" />
        </button>
        <Transition name="pop">
          <div v-if="menuOpen" class="menu glass" role="menu">
            <div class="me"><span class="small faint">Signed in as</span><b>{{ state.user?.email }}</b></div>
            <button class="item" role="menuitem" @click="openProfile"><Icon name="target" :size="17" /> Edit profile</button>
            <button class="item" role="menuitem" @click="openAccount"><Icon name="shield" :size="17" /> Account &amp; security</button>
            <div class="sep" />
            <button class="item" role="menuitem" @click="out"><Icon name="logout" :size="17" /> Sign out</button>
          </div>
        </Transition>
      </div>
    </div>
    <ProfileForm :open="profileOpen" mode="profile" @close="profileOpen = false" />
    <AccountSettings :open="accountOpen" @close="accountOpen = false" />
  </header>
</template>

<style scoped>
.top { position: sticky; top: 0; z-index: 30; padding: 14px 28px 0; max-width: 1320px; margin: 0 auto; }
.bar { display: flex; align-items: center; gap: 18px; padding: 9px 12px 9px 16px; border-radius: 999px; background: linear-gradient(145deg, rgba(40, 8, 12, 0.7), rgba(8, 24, 10, 0.62)); }
.brand { display: flex; align-items: center; gap: 10px; font-weight: 800; letter-spacing: -0.02em; font-size: 18px; color: var(--text); }
.brand:hover { text-decoration: none; }
.nav { display: flex; gap: 2px; margin: 0 auto; overflow-x: auto; scrollbar-width: none; }
.nav::-webkit-scrollbar { display: none; }
.nav a { display: inline-flex; align-items: center; gap: 8px; height: 40px; padding: 0 16px; border-radius: 999px; color: var(--muted); font-weight: 600; font-size: 14px; white-space: nowrap; transition: color 0.2s, background 0.3s var(--ease); }
.nav a:hover { color: var(--text); background: rgba(255, 255, 255, 0.07); text-decoration: none; }
.nav a.router-link-exact-active { background: var(--accent); color: var(--accent-ink); box-shadow: 0 6px 20px -6px var(--accent-glow); }
.user { position: relative; }
.who { display: flex; align-items: center; gap: 10px; height: 44px; padding: 0 12px 0 4px; border-radius: 999px; border: 1px solid transparent; background: transparent; color: var(--text); font: inherit; cursor: pointer; transition: background 0.2s, border-color 0.2s; }
.who:hover, .who[aria-expanded='true'] { background: rgba(255, 255, 255, 0.08); border-color: var(--glass-border); }
.who:focus-visible { outline: 2px solid var(--accent); }
.chev { color: var(--muted); transition: transform 0.3s var(--ease); }
.chev.up { transform: rotate(180deg); }
.menu { position: absolute; right: 0; top: calc(100% + 10px); min-width: 230px; padding: 8px; border-radius: 18px; background: rgba(24, 8, 10, 0.92); z-index: 40; }
.me { display: grid; gap: 2px; padding: 10px 12px 12px; border-bottom: 1px solid var(--line); margin-bottom: 6px; word-break: break-all; }
.sep { height: 1px; background: var(--line); margin: 6px 4px; }
.item { display: flex; align-items: center; gap: 10px; width: 100%; height: 40px; padding: 0 12px; border: 0; border-radius: 12px; background: transparent; color: var(--text); font: inherit; font-weight: 600; cursor: pointer; transition: background 0.2s; }
.item:hover { background: rgba(255, 255, 255, 0.09); }
.item:focus-visible { outline: 2px solid var(--accent); }
.pop-enter-active, .pop-leave-active { transition: opacity 0.2s ease, transform 0.3s var(--ease); }
.pop-enter-from, .pop-leave-to { opacity: 0; transform: translateY(-6px) scale(0.98); }
.avatar { width: 36px; height: 36px; border-radius: 50%; display: grid; place-items: center; font-weight: 800; background: linear-gradient(135deg, var(--c-wine), var(--c-moss)); border: 1px solid var(--glass-border); }
.uname { font-weight: 600; font-size: 14px; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 1100px) { .nav a span { display: none; } .nav a { padding: 0 13px; } .uname { display: none; } .who { padding: 0 8px 0 4px; } }
@media (max-width: 680px) { .top { padding: 10px 12px 0; } .brand span { display: none; } .bar { gap: 8px; } }
</style>

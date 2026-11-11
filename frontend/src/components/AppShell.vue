<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Icon from './Icon.vue'
import AccountSettings from './AccountSettings.vue'
import ProfileForm from './ProfileForm.vue'
import { displayName, errMsg, loadData, signOut, state, toast, toggleNav, ui } from '@/lib/store'

const router = useRouter()
const links = [
  { to: '/', label: 'Dashboard', icon: 'home' },
  { to: '/accounts', label: 'Accounts', icon: 'wallet' },
  { to: '/budget', label: 'Budget', icon: 'calendar' },
  { to: '/history', label: 'History', icon: 'history' },
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
onMounted(() => { loadData().catch(() => {}); document.addEventListener('mousedown', onDoc); document.addEventListener('keydown', onKey) })
onBeforeUnmount(() => { document.removeEventListener('mousedown', onDoc); document.removeEventListener('keydown', onKey) })
</script>

<template>
  <header class="top" :class="{ collapsed: ui.navCollapsed }">
    <div class="bar glass">
      <RouterLink to="/" class="brand" aria-label="Evergreen home">
        <svg width="30" height="30" viewBox="0 0 32 32"><rect width="32" height="32" rx="10" fill="var(--accent)" /><path d="M8 21l5-6 4 3 7-9" fill="none" stroke="#250104" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" /></svg>
        <span>Evergreen</span>
      </RouterLink>
      <nav class="nav" aria-label="Main">
        <RouterLink v-for="l in links" :key="l.to" :to="l.to" :aria-label="l.label">
          <Icon :name="l.icon" :size="18" /><span class="nl">{{ l.label }}</span>
          <span class="tip" aria-hidden="true">{{ l.label }}</span>
        </RouterLink>
      </nav>
      <button class="collapse" type="button" :aria-expanded="!ui.navCollapsed" :aria-label="ui.navCollapsed ? 'Expand navigation' : 'Collapse navigation'" @click="toggleNav">
        <Icon name="left" :size="18" class="cico" :class="{ flip: ui.navCollapsed }" /><span class="nl">Collapse</span>
        <span class="tip" aria-hidden="true">Expand</span>
      </button>
      <div ref="menuEl" class="user">
        <button class="who" :aria-expanded="menuOpen" aria-haspopup="menu" aria-label="Account menu" @click="menuOpen = !menuOpen">
          <span class="avatar" aria-hidden="true">{{ displayName?.[0]?.toUpperCase() }}</span>
          <span class="uname">{{ displayName }}</span>
          <Icon name="chevron" :size="16" class="chev" :class="{ up: menuOpen }" />
          <span class="tip" aria-hidden="true">{{ displayName }}</span>
        </button>
        <Transition name="pop">
          <div v-if="menuOpen" class="menu glass" role="menu">
            <div class="me"><b>{{ displayName }}</b><span class="small muted">{{ state.user?.email }}</span><span v-if="state.profile" class="small faint">Age {{ state.profile.current_age }}</span></div>
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
/* ---- default (below tablet landscape): top bar ---- */
.top { position: sticky; top: 0; z-index: 30; padding: 14px 28px 0; max-width: 1320px; margin: 0 auto; }
.bar { display: flex; align-items: center; gap: 18px; padding: 9px 12px 9px 16px; border-radius: 999px; background: linear-gradient(145deg, rgba(40, 8, 12, 0.7), rgba(8, 24, 10, 0.62)); }
.brand { display: flex; align-items: center; gap: 10px; font-weight: 800; letter-spacing: -0.02em; font-size: 18px; color: var(--text); }
.brand:hover { text-decoration: none; }
.nav { display: flex; gap: 2px; margin: 0 auto; overflow-x: auto; scrollbar-width: none; }
.nav::-webkit-scrollbar { display: none; }
.nav a { display: inline-flex; align-items: center; gap: 8px; height: 40px; padding: 0 16px; border-radius: 999px; color: var(--muted); font-weight: 600; font-size: 14px; white-space: nowrap; transition: color 0.2s, background 0.3s var(--ease); }
.nav a:hover { color: var(--text); background: rgba(255, 255, 255, 0.07); text-decoration: none; }
.nav a.router-link-exact-active { background: var(--accent); color: var(--accent-ink); box-shadow: 0 6px 20px -6px var(--accent-glow); }
.nav a:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
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
.avatar { width: 36px; height: 36px; border-radius: 50%; display: grid; place-items: center; flex: none; font-weight: 800; background: linear-gradient(135deg, var(--c-wine), var(--c-moss)); border: 1px solid var(--glass-border); }
.uname { font-weight: 600; font-size: 14px; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* collapse control only exists with the sidebar */
.collapse { display: none; }
.tip { display: none; }

/* tablets (portrait) and phones: icon-only nav */
@media (max-width: 1023px) { .nav a .nl { display: none; } .nav a { padding: 0 13px; } .uname { display: none; } .who { padding: 0 8px 0 4px; } }
@media (max-width: 680px) { .top { padding: 10px 12px 0; } .brand span { display: none; } .bar { gap: 8px; } }

/* ---- tablet landscape and up: fixed left sidebar ---- */
@media (min-width: 1024px) {
  .top { position: fixed; z-index: 30; inset: 0 auto 0 0; width: 276px; max-width: none; margin: 0; padding: 16px 0 16px 16px; }
  .bar { flex-direction: column; align-items: stretch; gap: 6px; height: 100%; padding: 20px 12px 14px; border-radius: 28px; }
  .brand { padding: 0 10px 18px; font-size: 21px; gap: 12px; border-bottom: 1px solid var(--line); margin-bottom: 10px; }
  .nav { flex-direction: column; gap: 4px; margin: 0; overflow: visible; }
  .nav a { height: 46px; padding: 0 14px; gap: 14px; font-size: 14.5px; }
  .nav a, .collapse, .who { position: relative; }
  .nav a .nl, .collapse .nl { display: inline; }
  .collapse { display: flex; align-items: center; gap: 14px; height: 44px; margin-top: auto; padding: 0 14px; border: 0; border-radius: 999px; background: transparent; color: var(--faint); font: inherit; font-weight: 600; font-size: 14px; cursor: pointer; transition: color 0.2s, background 0.2s; }
  .collapse:hover { color: var(--text); background: rgba(255, 255, 255, 0.07); }
  .collapse:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .cico { transition: transform 0.35s var(--ease); flex: none; }
  .cico.flip { transform: rotate(180deg); }
  .user { padding-top: 12px; margin-top: 6px; border-top: 1px solid var(--line); }
  .who { width: 100%; padding: 0 10px 0 6px; height: 54px; border-radius: 18px; text-align: left; }
  .uname { flex: 1; min-width: 0; max-width: none; display: block; }
  .menu { right: 0; left: 0; top: auto; bottom: calc(100% + 10px); min-width: 0; transform-origin: bottom; }
  .pop-enter-from, .pop-leave-to { transform: translateY(6px) scale(0.98); }
  .top { transition: width 0.35s var(--ease); }
  .bar { overflow: visible; }
  .nav a .nl, .collapse .nl, .uname { white-space: nowrap; }

  /* ---- collapsed: icon rail with hover/focus popovers ---- */
  .top.collapsed { width: 92px; }
  .collapsed .brand { justify-content: center; padding: 0 0 18px; }
  .top.collapsed .brand span, .top.collapsed .nav a .nl, .top.collapsed .collapse .nl, .top.collapsed .uname, .top.collapsed .chev { display: none; }
  .top.collapsed .nav a, .top.collapsed .collapse { justify-content: center; padding: 0; gap: 0; }
  .collapsed .who { justify-content: center; padding: 0; }
  .collapsed .menu { left: calc(100% + 12px); right: auto; bottom: 0; min-width: 240px; transform-origin: left bottom; }
  .tip { position: absolute; left: calc(100% + 14px); top: 50%; z-index: 50; padding: 7px 12px; border-radius: 12px; font-size: 13px; font-weight: 700; color: var(--text); white-space: nowrap; pointer-events: none; opacity: 0; transform: translate(-6px, -50%); transition: opacity 0.15s ease, transform 0.2s var(--ease); background: rgba(24, 8, 10, 0.94); border: 1px solid var(--glass-border); box-shadow: 0 12px 30px -10px rgba(0, 0, 0, 0.7); }
  .tip::before { content: ''; position: absolute; left: -5px; top: 50%; width: 9px; height: 9px; background: inherit; border-left: 1px solid var(--glass-border); border-bottom: 1px solid var(--glass-border); transform: translateY(-50%) rotate(45deg); }
  .collapsed .nav a:hover .tip, .collapsed .nav a:focus-visible .tip,
  .collapsed .collapse:hover .tip, .collapsed .collapse:focus-visible .tip,
  .collapsed .who:hover .tip, .collapsed .who:focus-visible .tip { display: block; opacity: 1; transform: translate(0, -50%); }
  .collapsed .who[aria-expanded='true'] .tip { display: none; }
  .collapsed .tip { display: block; }
}
</style>

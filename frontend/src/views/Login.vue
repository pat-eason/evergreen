<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PasswordField from '@/components/PasswordField.vue'
import { api } from '@/lib/api'
import { errMsg, resetData, state } from '@/lib/store'

const router = useRouter()
const route = useRoute()
const mode = ref<'login' | 'register'>('login')
const email = ref('')
const password = ref('')
const confirm = ref('')
const registrationOpen = ref(true)
const inviteRequired = ref(false)
const inviteCode = ref('')
const busy = ref(false)
const error = ref('')

// A server can close sign-ups once its owner has registered; hide the tab then.
onMounted(async () => {
  try {
    const cfg = await api.config()
    registrationOpen.value = cfg.registration !== 'closed'
    inviteRequired.value = cfg.registration === 'invite'
  } catch { /* assume open */ }
  if (!registrationOpen.value) mode.value = 'login'
})

async function submit() {
  error.value = ''
  if (mode.value === 'register' && password.value !== confirm.value) { error.value = 'Passwords do not match'; return }
  busy.value = true
  try {
    resetData()
    state.user = mode.value === 'login' ? await api.login(email.value, password.value) : await api.register(email.value, password.value, inviteRequired.value ? inviteCode.value : undefined)
    router.push((route.query.next as string) || '/')
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="wrap">
    <section class="hero rise">
      <div class="logo">
        <svg width="44" height="44" viewBox="0 0 32 32"><rect width="32" height="32" rx="10" fill="var(--accent)" /><path d="M8 21l5-6 4 3 7-9" fill="none" stroke="#250104" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" /></svg>
        <b>Evergreen</b>
      </div>
      <h1>Know your number.<br /><em>Own your timeline.</em></h1>
      <p class="muted">Budget by month, project your retirement with Monte Carlo, plan tax-smart withdrawals, and see exactly when Coast FIRE and financial freedom arrive — all stored locally on your machine.</p>
      <ul>
        <li><i class="dot" style="background: var(--c-accent)" />Contribution Crossover</li>
        <li><i class="dot" style="background: var(--c-mint)" />Coast FIRE</li>
        <li><i class="dot" style="background: var(--c-rose)" />Financial Freedom</li>
      </ul>
    </section>

    <section class="card glass rise" style="--i: 2">
      <div v-if="registrationOpen" class="tabs" role="tablist">
        <button class="tab" :class="{ on: mode === 'login' }" role="tab" @click="mode = 'login'; error = ''">Sign in</button>
        <button class="tab" :class="{ on: mode === 'register' }" role="tab" @click="mode = 'register'; error = ''">Create account</button>
      </div>
      <h2 v-else class="signin-title">Sign in</h2>
      <form class="stack" @submit.prevent="submit">
        <label class="field"><span>Email</span><input v-model.trim="email" class="input" type="email" inputmode="email" autocomplete="email" placeholder="you@example.com" required maxlength="254" autofocus /></label>
        <PasswordField v-model="password" label="Password" :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" :minlength="8" :maxlength="72" />
        <PasswordField v-if="mode === 'register'" v-model="confirm" label="Confirm password" autocomplete="new-password" :minlength="8" :maxlength="72" />
        <label v-if="mode === 'register' && inviteRequired" class="field"><span>Registration code</span><input v-model.trim="inviteCode" class="input" autocomplete="off" spellcheck="false" required maxlength="128" /></label>
        <p v-if="mode === 'register'" class="small faint">At least 8 characters. Your data never leaves this machine.</p>
        <p v-if="error" class="err" role="alert">{{ error }}</p>
        <button class="btn accent" :disabled="busy" style="height: 48px">
          <span v-if="busy" class="spin" /> {{ mode === 'login' ? 'Sign in' : 'Create account' }}
        </button>
      </form>
    </section>
  </main>
</template>

<style scoped>
.wrap { position: relative; z-index: 1; min-height: 100vh; display: grid; grid-template-columns: 1.15fr 0.85fr; gap: 56px; align-items: center; max-width: 1180px; margin: 0 auto; padding: 40px 28px; }
.logo { display: flex; align-items: center; gap: 12px; font-size: 22px; margin-bottom: 36px; }
.hero h1 { font-size: clamp(36px, 5.2vw, 62px); line-height: 1.04; letter-spacing: -0.04em; }
.hero h1 em { font-style: normal; color: var(--accent); }
.hero p { margin: 22px 0; max-width: 520px; font-size: 16.5px; }
ul { list-style: none; padding: 0; margin: 0; display: flex; flex-wrap: wrap; gap: 10px; }
li { display: inline-flex; align-items: center; gap: 9px; padding: 8px 16px; border-radius: 999px; background: rgba(255, 255, 255, 0.07); border: 1px solid var(--glass-border); font-weight: 600; font-size: 13.5px; }
.card { padding: 28px; display: grid; gap: 22px; background: linear-gradient(145deg, rgba(255, 255, 255, 0.13), rgba(255, 255, 255, 0.05)); }
.card .tabs { justify-self: start; }
.signin-title { font-size: 22px; }
@media (max-width: 900px) { .wrap { grid-template-columns: 1fr; gap: 30px; padding: 28px 16px; } }
</style>

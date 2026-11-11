<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import Drawer from './Drawer.vue'
import PasswordField from './PasswordField.vue'
import { api } from '@/lib/api'
import { errMsg, state, toast } from '@/lib/store'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const email = reactive({ value: '', password: '', busy: false, error: '' })
const pw = reactive({ current: '', next: '', confirm: '', busy: false, error: '' })
const emailPwKey = ref(0)

watch(() => props.open, (o) => {
  if (!o) return
  Object.assign(email, { value: state.user?.email ?? '', password: '', busy: false, error: '' })
  Object.assign(pw, { current: '', next: '', confirm: '', busy: false, error: '' })
  emailPwKey.value++
})

async function saveEmail() {
  email.error = ''
  email.busy = true
  try {
    const u = await api.changeEmail(email.value, email.password)
    state.user = u
    email.password = ''
    toast('Email updated')
  } catch (e) { email.error = errMsg(e) } finally { email.busy = false }
}

async function savePassword() {
  pw.error = ''
  if (pw.next !== pw.confirm) { pw.error = 'New passwords do not match'; return }
  pw.busy = true
  try {
    await api.changePassword(pw.current, pw.next)
    Object.assign(pw, { current: '', next: '', confirm: '' })
    toast('Password updated — other devices were signed out')
  } catch (e) { pw.error = errMsg(e) } finally { pw.busy = false }
}
</script>

<template>
  <Drawer :open="open" title="Account & security" @close="emit('close')">
    <div class="stack" style="gap: 26px">
      <form class="stack" @submit.prevent="saveEmail">
        <h3>Email address</h3>
        <label class="field"><span>Email</span><input v-model.trim="email.value" class="input" type="email" autocomplete="email" required maxlength="254" /></label>
        <PasswordField :key="'e' + emailPwKey" v-model="email.password" label="Current password" autocomplete="current-password" />
        <p v-if="email.error" class="err" role="alert">{{ email.error }}</p>
        <button class="btn accent" :disabled="email.busy || email.value === state.user?.email"><span v-if="email.busy" class="spin" /> Update email</button>
      </form>

      <hr class="divider" style="margin: 0" />

      <form class="stack" @submit.prevent="savePassword">
        <h3>Change password</h3>
        <PasswordField :key="'c' + emailPwKey" v-model="pw.current" label="Current password" autocomplete="current-password" />
        <PasswordField :key="'n' + emailPwKey" v-model="pw.next" label="New password" autocomplete="new-password" :minlength="8" :maxlength="72" />
        <PasswordField :key="'m' + emailPwKey" v-model="pw.confirm" label="Confirm new password" autocomplete="new-password" :minlength="8" :maxlength="72" />
        <p class="small faint">At least 8 characters. Changing it signs you out everywhere else.</p>
        <p v-if="pw.error" class="err" role="alert">{{ pw.error }}</p>
        <button class="btn accent" :disabled="pw.busy"><span v-if="pw.busy" class="spin" /> Update password</button>
      </form>
    </div>
  </Drawer>
</template>

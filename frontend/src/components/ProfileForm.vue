<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import Drawer from './Drawer.vue'
import NumField from './NumField.vue'
import { api } from '@/lib/api'
import { errMsg, invalidatePlan, state, toast } from '@/lib/store'
import type { Profile } from '@/lib/types'

const props = defineProps<{ open: boolean; mode: 'profile' | 'assumptions' }>()
const emit = defineEmits<{ close: [] }>()
const form = reactive<Profile>({} as Profile)
const busy = ref(false)
const error = ref('')

watch(() => props.open, (o) => {
  if (o && state.profile) { Object.assign(form, state.profile); error.value = '' }
})

async function save() {
  error.value = ''
  busy.value = true
  try {
    const r = await api.saveProfile({ ...form })
    state.profile = r.profile
    state.configured = true
    invalidatePlan()
    state.profileRev++
    toast(props.mode === 'profile' ? 'Profile saved' : 'Assumptions saved')
    emit('close')
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Drawer :open="open" :title="mode === 'profile' ? 'Edit profile' : 'Edit assumptions'" @close="emit('close')">
    <form id="profile-form" class="stack" @submit.prevent="save">
      <div v-if="mode === 'profile'" class="form-grid">
        <NumField v-model="form.current_age" kind="int" label="Your age today" />
        <NumField v-model="form.retirement_age" kind="int" label="Retirement age" />
        <NumField v-model="form.end_age" kind="int" label="Plan through age" hint="life expectancy" />
        <label class="field"><span>Tax filing status</span>
          <select v-model="form.filing_status" class="input"><option value="married">Married filing jointly</option><option value="single">Single</option></select>
        </label>
        <NumField v-model="form.retirement_spending" kind="money" label="Retirement spending" hint="per year, today's $" full />
        <NumField v-model="form.annual_income" kind="money" label="Current take-home income" hint="per year" />
        <NumField v-model="form.annual_expenses" kind="money" label="Current living expenses" hint="per year" />
        <p class="full small muted">Social Security, pensions, health care and other goals are added as events on the Plan page. Accounts and contributions come from the Accounts page.</p>
      </div>
      <div v-else class="form-grid">
        <NumField v-model="form.return_pre" kind="pct" label="Return before retirement" hint="nominal, avg." />
        <NumField v-model="form.return_post" kind="pct" label="Return in retirement" hint="nominal, avg." />
        <NumField v-model="form.return_stdev" kind="pct" label="Market volatility" hint="std. deviation" />
        <NumField v-model="form.inflation" kind="pct" label="Inflation" />
        <NumField v-model="form.contribution_growth" kind="pct" label="Contribution growth" hint="raises per year" />
        <NumField v-model="form.safe_withdrawal_rate" kind="pct" label="Safe withdrawal rate" hint="for FI number" />
        <NumField v-model="form.state_tax_rate" kind="pct" label="State income tax" hint="flat estimate" />
        <NumField v-model="form.dividend_yield" kind="pct" label="Taxable dividend yield" />
        <p class="full small muted">Returns are averages; the Monte Carlo simulation applies random year-to-year market swings using the volatility you set (stocks ≈ 15%, balanced ≈ 10–12%).</p>
      </div>
      <p v-if="error" class="err" role="alert">{{ error }}</p>
    </form>
    <template #footer>
      <button class="btn" type="button" @click="emit('close')">Cancel</button>
      <button class="btn accent" form="profile-form" :disabled="busy"><span v-if="busy" class="spin" /> Save</button>
    </template>
  </Drawer>
</template>

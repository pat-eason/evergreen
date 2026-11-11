<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import Drawer from './Drawer.vue'
import Icon from './Icon.vue'
import NumField from './NumField.vue'
import { api } from '@/lib/api'
import { incomeTypes, spendingTypes } from '@/lib/eventTypes'
import { errMsg, invalidatePlan, state, toast } from '@/lib/store'
import type { PlanEvent } from '@/lib/types'

const props = defineProps<{ open: boolean; kind: 'income' | 'spending'; event: PlanEvent | null }>()
const emit = defineEmits<{ close: [] }>()
const form = reactive<Omit<PlanEvent, 'id'> & { id?: number }>({ kind: 'income', type: 'other', name: '', start_age: 65, years: 0, annual_amount: 0, inflation_adjusted: true, taxable: false })
const forLife = ref(true)
const busy = ref(false)
const error = ref('')
const types = () => (props.kind === 'income' ? incomeTypes : spendingTypes)

function applyType(id: string) {
  const t = types().find((x) => x.id === id)!
  form.type = t.id
  form.name = t.name
  form.annual_amount = t.amount
  form.start_age = Math.max(t.startAge, state.profile?.current_age ?? 0)
  form.years = t.years
  forLife.value = t.years === 0
  form.taxable = t.taxable
}

watch(() => props.open, (o) => {
  if (!o) return
  error.value = ''
  if (props.event) {
    Object.assign(form, props.event)
    forLife.value = props.event.years === 0
  } else {
    delete form.id
    form.kind = props.kind
    form.inflation_adjusted = true
    applyType(types()[0].id)
  }
})

async function save() {
  error.value = ''
  if (!form.name.trim()) { error.value = 'Give it a name'; return }
  busy.value = true
  try {
    const body = { ...form, kind: props.kind, years: forLife.value ? 0 : Math.max(1, form.years || 1), taxable: props.kind === 'income' && form.taxable } as Omit<PlanEvent, 'id'>
    if (form.id) {
      const saved = await api.updateEvent({ ...body, id: form.id })
      const i = state.events.findIndex((e) => e.id === saved.id)
      if (i >= 0) state.events[i] = saved
    } else {
      state.events.push(await api.createEvent(body))
    }
    state.events.sort((a, b) => a.start_age - b.start_age)
    invalidatePlan()
    toast('Saved')
    emit('close')
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Drawer :open="open" :title="(event ? 'Edit ' : 'Add ') + (kind === 'income' ? 'income event' : 'spending goal')" @close="emit('close')">
    <form id="event-form" class="stack" @submit.prevent="save">
      <div v-if="!event" class="types">
        <button v-for="t in types()" :key="t.id" type="button" class="chip pick" :class="{ good: form.type === t.id }" @click="applyType(t.id)"><Icon :name="t.icon" :size="15" /> {{ t.label }}</button>
      </div>
      <label class="field"><span>Name</span><input v-model="form.name" class="input" maxlength="80" required /></label>
      <div class="form-grid">
        <NumField v-model="form.annual_amount" kind="money" label="Amount per year" hint="today's dollars" />
        <NumField v-model="form.start_age" kind="int" label="Starts at age" />
        <label class="full row sw">
          <span class="switch"><input v-model="forLife" type="checkbox" /><i /></span><span><b>For life</b></span>
        </label>
        <NumField v-if="!forLife" v-model="form.years" kind="int" label="Lasts for" suffix="yrs" full />
        <label class="full row sw">
          <span class="switch"><input v-model="form.inflation_adjusted" type="checkbox" /><i /></span>
          <span><b>Adjust for inflation</b><br /><small class="muted">Amount grows with the inflation assumption each year.</small></span>
        </label>
        <label v-if="kind === 'income' && form.type !== 'social_security'" class="full row sw">
          <span class="switch"><input v-model="form.taxable" type="checkbox" /><i /></span>
          <span><b>Taxable income</b><br /><small class="muted">Counts as ordinary income for federal and state tax.</small></span>
        </label>
        <p v-if="form.type === 'social_security'" class="full small muted">Social Security is taxed using the IRS provisional-income rules (up to 85% taxable).</p>
      </div>
      <p v-if="error" class="err" role="alert">{{ error }}</p>
    </form>
    <template #footer>
      <button class="btn" type="button" @click="emit('close')">Cancel</button>
      <button class="btn accent" form="event-form" :disabled="busy"><span v-if="busy" class="spin" /> Save</button>
    </template>
  </Drawer>
</template>

<style scoped>
.types { display: flex; flex-wrap: wrap; gap: 8px; }
.pick { height: 34px; cursor: pointer; border: 1px solid var(--line); font: inherit; font-weight: 600; font-size: 13px; }
.pick:hover { border-color: rgba(255, 255, 255, 0.3); }
.pick.good { border-color: var(--accent); }
.sw { gap: 14px; align-items: flex-start; cursor: pointer; }
</style>

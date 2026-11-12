<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import Drawer from './Drawer.vue'
import Icon from './Icon.vue'
import NumField from './NumField.vue'
import { api } from '@/lib/api'
import { money, shortDate, todayISO, parseDay } from '@/lib/format'
import { errMsg, invalidatePlan, ownerName, state, toast } from '@/lib/store'
import type { Account, ValueReason } from '@/lib/types'

const props = defineProps<{ open: boolean; account: Account | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()

const form = reactive({ accountId: '', balance: 0, homeValue: 0, reason: 'value' as ValueReason, asOf: todayISO(), note: '' })
const busy = ref(false)
const error = ref('')

const account = computed(() => state.accounts.find((a) => a.id === form.accountId) ?? null)
const isAsset = computed(() => account.value?.category === 'cash' || account.value?.category === 'investment')
const isMortgage = computed(() => account.value?.category === 'mortgage')

const reasons = computed<{ id: ValueReason; label: string; hint: string }[]>(() =>
  isAsset.value
    ? [
        { id: 'value', label: 'Market / value change', hint: 'Counts as growth or loss' },
        { id: 'deposit', label: 'Deposit / contribution', hint: 'Money you added' },
        { id: 'withdrawal', label: 'Withdrawal', hint: 'Money you took out' },
        { id: 'correction', label: 'Correction', hint: 'Fixing an earlier number' },
      ]
    : [
        { id: 'payment', label: 'Payment', hint: 'You paid the balance down' },
        { id: 'value', label: 'Interest / charges', hint: 'The balance grew on its own' },
        { id: 'borrowing', label: 'New borrowing', hint: 'You borrowed more' },
        { id: 'correction', label: 'Correction', hint: 'Fixing an earlier number' },
      ],
)

watch(() => props.open, (o) => {
  if (!o) return
  error.value = ''
  const a = props.account ?? state.accounts[0] ?? null
  form.accountId = a?.id ?? ''
  form.balance = a?.config.balance ?? 0
  form.homeValue = a?.config.home_value ?? 0
  form.asOf = todayISO()
  form.note = ''
  form.reason = a && (a.category === 'cash' || a.category === 'investment') ? 'value' : 'payment'
})

function pickAccount() {
  const a = account.value
  if (!a) return
  form.balance = a.config.balance
  form.homeValue = a.config.home_value ?? 0
  form.reason = a.category === 'cash' || a.category === 'investment' ? 'value' : 'payment'
}

const delta = computed(() => (account.value ? form.balance - account.value.config.balance : 0))
const backdated = computed(() => form.asOf < todayISO())

async function save() {
  error.value = ''
  if (!account.value) { error.value = 'Choose an account'; return }
  busy.value = true
  try {
    const r = await api.recordValue(account.value.id, {
      balance: form.balance,
      home_value: isMortgage.value ? form.homeValue : undefined,
      reason: form.reason,
      as_of: backdated.value ? form.asOf : undefined,
      note: form.note || undefined,
    })
    if (r.applied) {
      const i = state.accounts.findIndex((a) => a.id === r.account.id)
      if (i >= 0) state.accounts[i] = r.account
      invalidatePlan()
      state.profileRev++
      toast('Value recorded')
    } else {
      toast(`Saved to history for ${shortDate(parseDay(form.asOf), true)} — your current balance is unchanged`)
    }
    emit('saved')
    emit('close')
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Drawer :open="open" title="Record a value" @close="emit('close')">
    <form id="value-form" class="stack" @submit.prevent="save">
      <p class="small muted">Log a new balance. Each entry is added to your history so growth and loss show up over time. Deposits and withdrawals are tracked separately from market changes.</p>

      <label class="field"><span>Account</span>
        <select v-model="form.accountId" class="input" @change="pickAccount">
          <option v-for="a in state.accounts" :key="a.id" :value="a.id">{{ a.name }} · {{ ownerName(a.owner) }}</option>
        </select>
      </label>

      <template v-if="account">
        <div class="now">
          <span class="muted">Current balance</span><b class="num">{{ money(account.config.balance) }}</b>
        </div>
        <div class="form-grid">
          <NumField v-model="form.balance" kind="money" :label="isAsset ? 'New balance' : 'New amount owed'" />
          <NumField v-if="isMortgage" v-model="form.homeValue" kind="money" label="Home value" />
        </div>
        <p v-if="delta !== 0" class="chg num" :class="(isAsset ? delta > 0 : delta < 0) ? 'pos' : 'neg'">
          {{ delta > 0 ? '+' : '−' }}{{ money(Math.abs(delta)) }} {{ isAsset ? (delta > 0 ? 'up' : 'down') : (delta < 0 ? 'less owed' : 'more owed') }}
        </p>

        <div class="field"><span>What changed?</span>
          <div class="reasons" role="radiogroup" aria-label="Reason">
            <button v-for="r in reasons" :key="r.id" type="button" role="radio" :aria-checked="form.reason === r.id" class="reason" :class="{ on: form.reason === r.id }" @click="form.reason = r.id">
              <b>{{ r.label }}</b><small>{{ r.hint }}</small>
            </button>
          </div>
        </div>

        <div class="form-grid">
          <label class="field"><span>As of</span><input v-model="form.asOf" class="input" type="date" :max="todayISO()" min="1990-01-01" required /></label>
          <label class="field"><span>Note <em class="hint">optional</em></span><input v-model="form.note" class="input" maxlength="200" placeholder="e.g. quarterly statement" /></label>
        </div>
        <p v-if="backdated" class="payoff"><Icon name="info" :size="16" /> A past date adds to your history only. It won't change today's balance unless it's newer than your latest entry.</p>
      </template>
      <p v-else class="muted">Add an account first.</p>
      <p v-if="error" class="err" role="alert">{{ error }}</p>
    </form>
    <template #footer>
      <button class="btn" type="button" @click="emit('close')">Cancel</button>
      <button class="btn accent" form="value-form" :disabled="busy || !account"><span v-if="busy" class="spin" /> Record value</button>
    </template>
  </Drawer>
</template>

<style scoped>
.now { display: flex; justify-content: space-between; align-items: baseline; padding: 12px 14px; border-radius: 14px; background: rgba(0, 0, 0, 0.22); border: 1px solid var(--line); }
.now b { font-size: 18px; }
.chg { font-weight: 700; margin: -4px 0 0; }
.reasons { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.reason { display: grid; gap: 2px; text-align: left; padding: 10px 12px; border-radius: 14px; border: 1px solid var(--line); background: rgba(255, 255, 255, 0.04); color: var(--text); font: inherit; cursor: pointer; transition: all 0.2s var(--ease); }
.reason small { color: var(--faint); font-size: 11.5px; }
.reason:hover { border-color: rgba(255, 255, 255, 0.3); }
.reason.on { border-color: var(--accent); background: var(--accent-soft); box-shadow: 0 0 0 3px var(--accent-soft); }
.reason:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.payoff { display: flex; align-items: flex-start; gap: 8px; padding: 10px 14px; border-radius: 12px; background: var(--accent-soft); font-size: 13px; }
.payoff svg { color: var(--accent); flex: none; margin-top: 2px; }
@media (max-width: 520px) { .reasons { grid-template-columns: 1fr; } }
</style>

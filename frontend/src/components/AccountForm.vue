<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import Drawer from './Drawer.vue'
import Icon from './Icon.vue'
import NumField from './NumField.vue'
import { api } from '@/lib/api'
import { amortPayment, durationLabel, payoffMonths } from '@/lib/loan'
import { money2, pct } from '@/lib/format'
import { errMsg, invalidatePlan, people, state, toast } from '@/lib/store'
import type { Account, AccountConfig, Category, Owner } from '@/lib/types'

const props = defineProps<{ open: boolean; account: Account | null; initialCategory?: Category }>()
const emit = defineEmits<{ close: []; saved: [] }>()

const cats: { id: Category; label: string; icon: string; blurb: string }[] = [
  { id: 'cash', label: 'Cash', icon: 'bank', blurb: 'Checking, savings, HYSA' },
  { id: 'investment', label: 'Investments', icon: 'trend', blurb: '401(k), IRA, Roth, brokerage' },
  { id: 'mortgage', label: 'Mortgage', icon: 'house', blurb: 'Home loan & housing costs' },
  { id: 'credit_card', label: 'Credit card', icon: 'card', blurb: 'Revolving balances' },
  { id: 'debt', label: 'Debts', icon: 'debt', blurb: 'Student, auto, personal loans' },
]

const form = reactive<{ name: string; category: Category; owner: Owner; config: AccountConfig }>({ name: '', category: 'cash', owner: 'self', config: { balance: 0 } })
const busy = ref(false)
const error = ref('')

function defaults(cat: Category): AccountConfig {
  switch (cat) {
    case 'cash': return { balance: 0, apy: 0.04, monthly_contribution: 0 }
    case 'investment': return { balance: 0, tax_treatment: 'tax_deferred', monthly_contribution: 0, employer_match: 0 }
    case 'mortgage': return { balance: 0, interest_rate: 0.065, remaining_years: 30, home_value: 0, home_appreciation: 0.03 }
    case 'credit_card': return { balance: 0, apr: 0.22, pay_in_full: false }
    case 'debt': return { balance: 0, interest_rate: 0.06, remaining_years: 5, debt_kind: 'Student loan' }
  }
}

watch(() => props.open, (o) => {
  if (!o) return
  error.value = ''
  if (props.account) {
    form.name = props.account.name
    form.category = props.account.category
    form.owner = props.account.owner || 'self'
    form.config = { ...props.account.config }
  } else {
    const cat = props.initialCategory ?? 'cash'
    form.name = ''
    form.owner = 'self'
    form.category = cat
    form.config = defaults(cat)
  }
})

function pick(cat: Category) {
  if (props.account) return
  form.category = cat
  form.config = defaults(cat)
}

const title = computed(() => (props.account ? 'Edit account' : 'Add account'))
const estPayment = computed(() => amortPayment(form.config.balance, form.config.interest_rate ?? 0, form.config.remaining_years || 0))
const payoff = computed(() =>
  form.category === 'mortgage' || form.category === 'debt' || form.category === 'credit_card'
    ? durationLabel(payoffMonths({ id: 0, name: form.name, category: form.category, owner: form.owner, config: form.config }))
    : '',
)
const utilization = computed(() => (form.config.credit_limit ? form.config.balance / form.config.credit_limit : 0))
const namePlaceholder = computed(() => ({ cash: 'e.g. Marcus HYSA', investment: 'e.g. 401(k)', mortgage: 'e.g. Home mortgage', credit_card: 'e.g. Visa rewards', debt: 'e.g. Student loan' })[form.category])

async function save() {
  error.value = ''
  if (!form.name.trim()) { error.value = 'Give the account a name'; return }
  busy.value = true
  try {
    const body = { name: form.name.trim(), category: form.category, owner: form.owner, config: { ...form.config } }
    if (props.account) {
      const saved = await api.updateAccount({ ...body, id: props.account.id })
      const i = state.accounts.findIndex((a) => a.id === saved.id)
      if (i >= 0) state.accounts[i] = saved
    } else {
      state.accounts.push(await api.createAccount(body))
    }
    invalidatePlan()
    toast(props.account ? 'Account updated' : 'Account added')
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
  <Drawer :open="open" :title="title" @close="emit('close')">
    <form id="acct-form" class="stack" @submit.prevent="save">
      <div v-if="!account" class="cats">
        <button v-for="c in cats" :key="c.id" type="button" class="cat" :class="{ on: form.category === c.id }" @click="pick(c.id)">
          <Icon :name="c.icon" :size="20" /><b>{{ c.label }}</b><small>{{ c.blurb }}</small>
        </button>
      </div>

      <div class="form-grid">
        <label class="field"><span>Account name</span><input v-model="form.name" class="input" :placeholder="namePlaceholder" maxlength="80" required /></label>
        <label class="field"><span>Owner</span>
          <select v-model="form.owner" class="input">
            <option v-for="p in people" :key="p.key" :value="p.key">{{ p.name }}{{ p.key === 'self' ? ' (you)' : '' }}</option>
            <option value="joint">Joint</option>
          </select>
        </label>
      </div>

      <!-- CASH -->
      <div v-if="form.category === 'cash'" class="form-grid">
        <NumField v-model="form.config.balance" kind="money" label="Current balance" />
        <NumField v-model="form.config.apy" kind="pct" label="Interest rate (APY)" />
        <NumField v-model="form.config.monthly_contribution" kind="money" label="Monthly deposit" hint="added until retirement" full />
      </div>

      <!-- INVESTMENT -->
      <div v-else-if="form.category === 'investment'" class="form-grid">
        <label class="field full">
          <span>Tax treatment <em class="hint">controls how withdrawals are taxed</em></span>
          <select v-model="form.config.tax_treatment" class="input">
            <option value="tax_deferred">Tax-deferred — 401(k), traditional IRA</option>
            <option value="tax_free">Tax-free — Roth IRA / Roth 401(k), HSA</option>
            <option value="taxable">Taxable — brokerage</option>
          </select>
        </label>
        <NumField v-model="form.config.balance" kind="money" label="Current balance" />
        <NumField v-model="form.config.monthly_contribution" kind="money" label="Your monthly contribution" />
        <NumField v-model="form.config.employer_match" kind="money" label="Employer match" hint="monthly" />
        <NumField v-model="form.config.expected_return" kind="pct" label="Expected return" hint="blank = plan default" />
        <NumField v-model="form.config.expense_ratio" kind="pct" label="Fund fees" hint="expense ratio" />
        <NumField v-if="form.config.tax_treatment === 'taxable'" v-model="form.config.cost_basis" kind="money" label="Cost basis" hint="blank = balance" />
      </div>

      <!-- MORTGAGE -->
      <div v-else-if="form.category === 'mortgage'" class="form-grid">
        <NumField v-model="form.config.balance" kind="money" label="Loan balance" />
        <NumField v-model="form.config.interest_rate" kind="pct" label="Interest rate" />
        <NumField v-model="form.config.remaining_years" kind="num" label="Years remaining" suffix="yrs" />
        <NumField v-model="form.config.monthly_payment" kind="money" label="Monthly P&I" :hint="estPayment ? `blank = ${money2(estPayment)}` : ''" />
        <NumField v-model="form.config.home_value" kind="money" label="Home value" />
        <NumField v-model="form.config.home_appreciation" kind="pct" label="Appreciation / yr" />
        <NumField v-model="form.config.property_tax" kind="money" label="Property tax" hint="per year" />
        <NumField v-model="form.config.insurance" kind="money" label="Home insurance" hint="per year" />
        <p class="full small muted">Property tax and insurance keep applying after the loan is paid off. Payments count as a spending goal in retirement.</p>
      </div>

      <!-- CREDIT CARD -->
      <div v-else-if="form.category === 'credit_card'" class="form-grid">
        <NumField v-model="form.config.balance" kind="money" label="Current balance" />
        <NumField v-model="form.config.apr" kind="pct" label="APR" />
        <NumField v-model="form.config.credit_limit" kind="money" label="Credit limit" :hint="form.config.credit_limit ? `${pct(utilization)} used` : ''" />
        <NumField v-model="form.config.monthly_payment" kind="money" label="Monthly payment" hint="blank = minimum" />
        <label class="full row switchrow">
          <span class="switch"><input v-model="form.config.pay_in_full" type="checkbox" /><i /></span>
          <span><b>Paid in full every month</b><br /><small class="muted">No interest or long-term payments; the balance is just routine spending.</small></span>
        </label>
      </div>

      <!-- DEBT -->
      <div v-else class="form-grid">
        <label class="field full"><span>Type of debt</span>
          <select v-model="form.config.debt_kind" class="input">
            <option>Student loan</option><option>Auto loan</option><option>Personal loan</option><option>Medical</option><option>Other</option>
          </select>
        </label>
        <NumField v-model="form.config.balance" kind="money" label="Balance" />
        <NumField v-model="form.config.interest_rate" kind="pct" label="Interest rate" />
        <NumField v-model="form.config.remaining_years" kind="num" label="Years remaining" suffix="yrs" />
        <NumField v-model="form.config.monthly_payment" kind="money" label="Monthly payment" :hint="estPayment ? `blank = ${money2(estPayment)}` : ''" />
      </div>

      <p v-if="account && form.config.balance !== account.config.balance" class="payoff"><Icon name="info" :size="16" /> This balance change is saved to your history as a value change. To log a deposit, withdrawal or payment, use <b>Record a value</b> instead.</p>
      <p v-if="payoff && form.config.balance > 0" class="payoff"><Icon name="info" :size="16" /> Paid off in <b>{{ payoff }}</b> at this payment.</p>
      <p v-if="error" class="err" role="alert">{{ error }}</p>
    </form>
    <template #footer>
      <button class="btn" type="button" @click="emit('close')">Cancel</button>
      <button class="btn accent" form="acct-form" :disabled="busy"><span v-if="busy" class="spin" /> {{ account ? 'Save changes' : 'Add account' }}</button>
    </template>
  </Drawer>
</template>

<style scoped>
.cats { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 10px; }
.cat { display: grid; gap: 3px; text-align: left; padding: 12px 14px; border-radius: 16px; border: 1px solid var(--line); background: rgba(255, 255, 255, 0.04); color: var(--text); font: inherit; cursor: pointer; transition: all 0.25s var(--ease); }
.cat svg { color: var(--muted); margin-bottom: 4px; }
.cat small { color: var(--faint); font-size: 11.5px; line-height: 1.3; }
.cat:hover { border-color: rgba(255, 255, 255, 0.28); transform: translateY(-2px); }
.cat.on { border-color: var(--accent); background: var(--accent-soft); box-shadow: 0 0 0 3px var(--accent-soft); }
.cat.on svg { color: var(--accent); }
.switchrow { gap: 14px; align-items: flex-start; cursor: pointer; }
.payoff { display: flex; align-items: center; gap: 8px; padding: 10px 14px; border-radius: 12px; background: var(--accent-soft); color: var(--text); font-size: 13.5px; }
.payoff svg { color: var(--accent); flex: none; }
</style>

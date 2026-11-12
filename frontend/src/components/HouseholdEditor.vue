<script setup lang="ts">
import { reactive, ref } from 'vue'
import Icon from './Icon.vue'
import NumField from './NumField.vue'
import { api } from '@/lib/api'
import { errMsg, invalidatePlan, state, toast } from '@/lib/store'
import type { Member } from '@/lib/types'

const rels: { id: Member['relationship']; label: string }[] = [
  { id: 'spouse', label: 'Spouse' }, { id: 'partner', label: 'Partner' }, { id: 'child', label: 'Child' }, { id: 'parent', label: 'Parent' }, { id: 'other', label: 'Other' },
]
const label = (id: string) => rels.find((r) => r.id === id)?.label ?? id

const editing = ref<string | 'new' | null>(null)
const form = reactive<Omit<Member, 'id'>>({ first_name: '', last_name: '', relationship: 'spouse', age: 0 })
const busy = ref(false)
const error = ref('')

function start(m?: Member) {
  error.value = ''
  if (m) Object.assign(form, { first_name: m.first_name, last_name: m.last_name, relationship: m.relationship, age: m.age })
  else Object.assign(form, { first_name: '', last_name: state.profile?.last_name ?? '', relationship: state.members.length ? 'child' : 'spouse', age: 0 })
  editing.value = m ? m.id : 'new'
}

async function save() {
  error.value = ''
  if (!form.first_name.trim()) { error.value = 'First name is required'; return }
  busy.value = true
  try {
    if (editing.value === 'new') state.members.push(await api.createMember({ ...form }))
    else {
      const saved = await api.updateMember({ ...form, id: editing.value as string })
      const i = state.members.findIndex((m) => m.id === saved.id)
      if (i >= 0) state.members[i] = saved
    }
    invalidatePlan()
    state.profileRev++ // ages feed the projections
    editing.value = null
  } catch (e) { error.value = errMsg(e) } finally { busy.value = false }
}

async function remove(m: Member) {
  if (!confirm(`Remove ${m.first_name}? Their accounts become joint and their income events move to your timeline.`)) return
  try {
    await api.deleteMember(m.id)
    state.members = state.members.filter((x) => x.id !== m.id)
    // accounts / events owned by them were reassigned server-side
    state.accounts.forEach((a) => { if (a.owner === String(m.id)) a.owner = 'joint' })
    state.events.forEach((e) => { if (e.owner === String(m.id)) e.owner = 'self' })
    invalidatePlan()
    state.profileRev++
    toast(`${m.first_name} removed`)
  } catch (e) { toast(errMsg(e), 'err') }
}
</script>

<template>
  <section class="hh">
    <div class="head"><h3>Household members</h3><button v-if="editing === null" type="button" class="btn sm" @click="start()"><Icon name="plus" :size="15" /> Add member</button></div>
    <p class="small muted">Add the people in your plan. You can assign accounts to them (or to the household as Joint) and tie income like Social Security to their age.</p>

    <ul v-if="state.members.length" class="list">
      <li v-for="m in state.members" :key="m.id" :class="{ off: editing === m.id }">
        <span class="av" aria-hidden="true">{{ m.first_name[0]?.toUpperCase() }}</span>
        <div class="who"><b>{{ m.first_name }} {{ m.last_name }}</b><small class="muted">{{ label(m.relationship) }} · age {{ m.age }}</small></div>
        <button type="button" class="btn icon sm" :aria-label="`Edit ${m.first_name}`" @click="start(m)"><Icon name="edit" :size="15" /></button>
        <button type="button" class="btn icon sm danger" :aria-label="`Remove ${m.first_name}`" @click="remove(m)"><Icon name="trash" :size="15" /></button>
      </li>
    </ul>
    <p v-else-if="editing === null" class="small faint empty-note">Just you for now.</p>

    <div v-if="editing !== null" class="editor">
      <div class="form-grid">
        <label class="field"><span>First name</span><input v-model="form.first_name" class="input" maxlength="40" autocomplete="off" /></label>
        <label class="field"><span>Last name</span><input v-model="form.last_name" class="input" maxlength="40" autocomplete="off" /></label>
        <label class="field"><span>Relationship</span>
          <select v-model="form.relationship" class="input"><option v-for="r in rels" :key="r.id" :value="r.id">{{ r.label }}</option></select>
        </label>
        <NumField v-model="form.age" kind="int" label="Age" />
      </div>
      <p v-if="error" class="err" role="alert">{{ error }}</p>
      <div class="row" style="justify-content: flex-end; margin-top: 12px">
        <button type="button" class="btn sm" @click="editing = null">Cancel</button>
        <button type="button" class="btn accent sm" :disabled="busy" @click="save"><span v-if="busy" class="spin" /> {{ editing === 'new' ? 'Add' : 'Save' }}</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.hh { display: grid; gap: 10px; padding-top: 4px; }
.head { display: flex; align-items: center; justify-content: space-between; }
.list { list-style: none; margin: 4px 0 0; padding: 0; display: grid; gap: 8px; }
.list li { display: flex; align-items: center; gap: 12px; padding: 10px 12px; border-radius: 16px; background: rgba(0, 0, 0, 0.2); border: 1px solid var(--line); transition: opacity 0.2s; }
.list li.off { opacity: 0.45; }
.av { width: 36px; height: 36px; border-radius: 50%; display: grid; place-items: center; font-weight: 800; flex: none; background: linear-gradient(135deg, var(--c-moss), var(--c-wine)); }
.who { flex: 1; min-width: 0; display: grid; }
.empty-note { padding: 6px 2px; }
.editor { padding: 14px; border-radius: 16px; border: 1px dashed rgba(255, 255, 255, 0.22); background: rgba(255, 255, 255, 0.03); }
</style>

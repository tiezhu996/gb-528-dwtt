<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Ban, CheckCircle2, ClipboardCheck, UserRound, Wrench } from 'lucide-vue-next'
import { ElMessage } from 'element-plus'
import { errorMessage } from '../../api/client'
import type { BlockerDecision, RehearsalRun } from '../../types/rehearsal'
import { formatMillis, formatTimestamp } from '../../utils/timeline'

const props = defineProps<{
  run: RehearsalRun
  canRegister: boolean
  register: (run: RehearsalRun, evidenceKey: string, decision: BlockerDecision, reason: string) => Promise<RehearsalRun>
}>()

// One editor per blocker key; drafts are seeded from the run whenever the
// selected run changes. The run version is global and bumps per save.
const drafts = reactive(new Map<string, { decision: BlockerDecision; reason: string }>())
const savingKey = ref('')
const localError = ref('')

function seedDrafts(run: RehearsalRun) {
  drafts.clear()
  for (const blocker of run.blockers) {
    drafts.set(blocker.evidence_key, {
      decision: blocker.disposition?.decision ?? 'accepted',
      reason: blocker.disposition?.reason ?? '',
    })
  }
}

// Seed once per selected run; saves bump props.run but must not wipe text the
// reviewer has typed into the other blocker editors.
watch(
  () => props.run.id,
  () => seedDrafts(props.run),
  { immediate: true },
)

function draft(key: string) {
  return drafts.get(key) ?? { decision: 'accepted' as BlockerDecision, reason: '' }
}

const pending = computed(() => props.run.blockers.filter((blocker) => !blocker.disposition))
const rectifying = computed(() => props.run.blockers.filter((blocker) => blocker.disposition?.decision === 'needs_rectification'))
const accepted = computed(() => props.run.blockers.filter((blocker) => blocker.disposition?.decision === 'accepted'))
const progressLabel = computed(() => {
  if (props.run.blocker_count === 0) return 'No blocking evidence on this run.'
  return `${accepted.value.length}/${props.run.blocker_count} accepted · ${pending.value.length} unregistered · ${rectifying.value.length} to rectify`
})

async function save(blocker: { evidence_key: string; disposition: { reason: string; decision: BlockerDecision } | null }) {
  const key = blocker.evidence_key
  const draft = drafts.get(key)
  localError.value = ''
  if (!draft || draft.reason.trim().length < 4) {
    localError.value = 'VALIDATION_ERROR: write a reason (at least 4 characters) for this blocker.'
    ElMessage.error('Write a reason before registering the blocker decision')
    return
  }
  savingKey.value = key
  try {
    await props.register(props.run, key, draft.decision, draft.reason.trim())
    ElMessage.success(`Blocker ${draft.decision === 'accepted' ? 'accepted' : 'marked for rectification'}`)
  } catch (cause) {
    localError.value = errorMessage(cause)
  } finally {
    savingKey.value = ''
  }
}

const tagType = (decision: BlockerDecision) => (decision === 'accepted' ? 'success' : 'danger')
</script>

<template>
  <section v-if="run.blocker_count > 0" class="data-section blocker-review">
    <div class="section-heading">
      <div>
        <p class="eyebrow">BLOCKER DISPOSITIONS</p>
        <h2>Register one decision per blocker</h2>
      </div>
      <span>{{ progressLabel }}</span>
    </div>
    <el-alert v-if="localError" :title="localError" type="error" :closable="false" show-icon />
    <el-progress
      :percentage="run.blocker_count === 0 ? 0 : Math.round((run.blockers_accepted / run.blocker_count) * 100)"
      :status="run.blockers_accepted === run.blocker_count ? 'success' : undefined"
      :stroke-width="10"
      class="blocker-progress"
    />
    <el-alert
      v-if="run.blockers_pending_disposition > 0 || run.blockers_needs_rectification > 0"
      type="warning"
      :closable="false"
      show-icon
      class="blocker-gate"
    >
      <template #title>
        Approval is withheld until every blocker is registered.
        <span v-if="run.blockers_pending_disposition > 0">Missing disposition: {{ run.blockers_pending_disposition }}.</span>
        <span v-if="run.blockers_needs_rectification > 0"> Marked for rectification: {{ run.blockers_needs_rectification }}.</span>
      </template>
      <ul v-if="pending.length > 0" class="gate-list">
        <li v-for="blocker in pending" :key="`missing-${blocker.evidence_key}`">{{ blocker.rule_code }} · {{ (blocker.cue_codes ?? []).join(', ') || 'rule set' }} · {{ formatMillis(blocker.window_start_ms) }}–{{ formatMillis(blocker.window_end_ms) }}</li>
      </ul>
      <ul v-if="rectifying.length > 0" class="gate-list">
        <li v-for="blocker in rectifying" :key="`rectify-${blocker.evidence_key}`">{{ blocker.rule_code }} · {{ (blocker.cue_codes ?? []).join(', ') || 'rule set' }} — revise the cue and rerun the rehearsal before approval.</li>
      </ul>
    </el-alert>
    <div v-for="blocker in run.blockers" :key="blocker.evidence_key" class="blocker-row">
      <header>
        <el-tag type="danger" effect="plain"><Ban :size="13" />{{ blocker.result }}</el-tag>
        <strong>{{ blocker.rule_code }}</strong>
        <span class="blocker-scope">{{ (blocker.cue_codes ?? []).join(', ') || 'rule set' }} · {{ (blocker.device_codes ?? []).join(', ') || 'no device scope' }}</span>
        <span class="blocker-window">{{ formatMillis(blocker.window_start_ms) }}–{{ formatMillis(blocker.window_end_ms) }} · {{ blocker.actual_value }} / {{ blocker.threshold_value }} {{ blocker.unit }}</span>
        <el-tag v-if="blocker.disposition" :type="tagType(blocker.disposition.decision)" effect="dark">
          <component :is="blocker.disposition.decision === 'accepted' ? CheckCircle2 : Wrench" :size="13" />
          {{ blocker.disposition.decision.replaceAll('_', ' ') }}
        </el-tag>
        <el-tag v-else type="warning" effect="plain"><ClipboardCheck :size="13" />unregistered</el-tag>
      </header>
      <p class="blocker-message">{{ blocker.message }}</p>
      <div v-if="blocker.disposition" class="blocker-record">
        <UserRound :size="14" />
        <span><strong>{{ blocker.disposition.reviewer_name }}</strong> registered {{ formatTimestamp(blocker.disposition.registered_at) }}</span>
        <span class="blocker-reason">“{{ blocker.disposition.reason }}”</span>
      </div>
      <div v-if="canRegister" class="blocker-editor">
        <el-radio-group v-model="draft(blocker.evidence_key).decision" size="small">
          <el-radio-button value="accepted"><CheckCircle2 :size="13" /> accept + reason</el-radio-button>
          <el-radio-button value="needs_rectification"><Wrench :size="13" /> mark for rectification</el-radio-button>
        </el-radio-group>
        <el-input
          v-model="draft(blocker.evidence_key).reason"
          type="textarea"
          :rows="2"
          maxlength="500"
          show-word-limit
          :placeholder="draft(blocker.evidence_key).decision === 'accepted' ? 'Explain why this blocker is accepted for offline rehearsal planning.' : 'Describe the cue/device rectification required before resubmission.'"
        />
        <el-button
          type="primary"
          plain
          :loading="savingKey === blocker.evidence_key"
          @click="save(blocker)"
        >
          {{ blocker.disposition ? 'Update disposition' : 'Register disposition' }}
        </el-button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.blocker-review { margin-top: 30px; }
.blocker-progress { margin: 14px 0 4px; }
.blocker-gate { margin: 12px 0; }
.gate-list { margin: 6px 0 0; padding-left: 18px; }
.gate-list li { font-size: 0.78rem; }
.blocker-row { padding: 14px 0; border-bottom: 1px solid var(--line); display: grid; gap: 8px; }
.blocker-row header { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.blocker-scope { font-size: 0.82rem; }
.blocker-window { color: var(--muted); font-size: 0.74rem; margin-left: auto; }
.blocker-message { margin: 0; font-size: 0.86rem; }
.blocker-record { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; background: #f0f4f2; padding: 8px 10px; font-size: 0.78rem; }
.blocker-reason { font-style: italic; }
.blocker-editor { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 10px; align-items: start; }
@media (max-width: 900px) {
  .blocker-editor { grid-template-columns: 1fr; }
  .blocker-window { margin-left: 0; }
}
</style>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { CheckCircle2, ClipboardCheck, Hammer, Play, RefreshCw, Send, UserRound, XCircle } from 'lucide-vue-next'
import { ElMessage } from 'element-plus'
import PageHeader from '../components/common/PageHeader.vue'
import TimelineTrack from '../components/common/TimelineTrack.vue'
import RuleEvidenceTable from '../components/common/RuleEvidenceTable.vue'
import { errorMessage } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { useRehearsalRun } from '../hooks/useRehearsalRun'
import { useCueStore } from '../stores/cues'
import type { DispositionDecision, RehearsalRun } from '../types/rehearsal'
import type { RuleEvidence } from '../types/interlock'
import { allBlockersAccepted, blockerEvidence, dispositionFor, dispositionGaps, formatDispositionError } from '../utils/disposition'
import { formatMillis, formatTimestamp } from '../utils/timeline'

const cues = useCueStore()
const { store: runs, polling, poll } = useRehearsalRun()
const { canProgram, canReview } = useAuth()
const selectedCueIds = ref<number[]>([])
const reason = ref('Offline timing, rule evidence, and collision windows reviewed for rehearsal planning.')
const localError = ref('')
const savingKey = ref('')
const draftDecision = ref<Record<string, DispositionDecision>>({})
const draftReason = ref<Record<string, string>>({})
const selected = computed(() => runs.selected)

const statusType = (status: string) => ({ evaluated: 'primary', blocked: 'danger', pending_review: 'warning', approved_for_rehearsal: 'success', rejected: 'info' }[status] ?? 'info')
const blockers = computed(() => (selected.value ? blockerEvidence(selected.value) : []))
const gaps = computed(() => (selected.value ? dispositionGaps(selected.value) : { unregistered: [], needsRectification: [] }))
const registeredCount = computed(() => blockers.value.length - gaps.value.unregistered.length)
const inReview = computed(() => selected.value?.run_status === 'pending_review')
const canApprove = computed(() => selected.value ? allBlockersAccepted(selected.value) : false)

function scopeLine(evidence: RuleEvidence): string {
  const cuesPart = evidence.cue_codes?.join(', ') || 'Rule set'
  const devicesPart = evidence.device_codes?.join(', ') || 'No device scope'
  return `${cuesPart} · ${devicesPart} · ${formatMillis(evidence.window_start_ms)}–${formatMillis(evidence.window_end_ms)}`
}

function decisionFor(evidence: RuleEvidence) {
  return selected.value ? dispositionFor(selected.value, evidence.evidence_key) : undefined
}

function tagType(decision?: DispositionDecision | string) {
  if (decision === 'accepted') return 'success'
  if (decision === 'needs_rectification') return 'danger'
  return 'info'
}

function startDraft(evidence: RuleEvidence) {
  const existing = decisionFor(evidence)
  if (!draftDecision.value[evidence.evidence_key]) draftDecision.value[evidence.evidence_key] = existing?.decision ?? 'accepted'
  if (draftReason.value[evidence.evidence_key] === undefined) draftReason.value[evidence.evidence_key] = existing?.reason ?? ''
}

async function saveDisposition(evidence: RuleEvidence) {
  if (!selected.value) return
  const decision = draftDecision.value[evidence.evidence_key]
  const justification = draftReason.value[evidence.evidence_key] ?? ''
  if (!decision) return
  if (justification.trim().length < 4) {
    localError.value = 'A disposition reason of at least 4 characters is required.'
    return
  }
  localError.value = ''
  savingKey.value = evidence.evidence_key
  try {
    await runs.registerDisposition(selected.value, evidence.evidence_key, decision, justification)
    ElMessage.success(`Disposition for ${evidence.evidence_key} registered`)
  } catch (cause) {
    localError.value = errorMessage(cause)
  } finally {
    savingKey.value = ''
  }
}

async function run() {
  localError.value = ''
  try {
    const created = await runs.run(selectedCueIds.value)
    poll(created.id)
    ElMessage.success(`Immutable rehearsal #${created.id} created`)
  } catch (cause) {
    localError.value = errorMessage(cause)
  }
}

async function submit() {
  if (!selected.value) return
  localError.value = ''
  try {
    await runs.submit(selected.value, reason.value)
    ElMessage.success(blockers.value.length > 0 ? `Blocker run submitted: ${blockers.value.length} evidence item(s) await disposition` : 'Rehearsal submitted to safety review')
  } catch (cause) {
    localError.value = errorMessage(cause)
  }
}

async function review(decision: 'approve' | 'reject') {
  if (!selected.value) return
  localError.value = ''
  try {
    const updated = await runs.review(selected.value, decision, reason.value)
    ElMessage.success(`Rehearsal ${updated.run_status.replaceAll('_', ' ')}`)
  } catch (cause) {
    localError.value = formatDispositionError(cause)
  }
}

onMounted(async () => {
  await Promise.all([cues.load(), runs.load()]).catch(() => undefined)
  selectedCueIds.value = cues.locked.map((item) => item.id)
})
</script>

<template>
  <PageHeader eyebrow="DETERMINISTIC OFFLINE RUN" title="Rehearsal evidence" description="Expand locked Cue versions into one immutable timeline, then inspect every warning, blocker, and collision interval before human review.">
    <el-button :icon="RefreshCw" :loading="runs.loading" @click="runs.load">Refresh</el-button>
  </PageHeader>
  <el-alert v-if="runs.error || cues.error || localError" :title="localError || runs.error || cues.error" type="error" :closable="false" show-icon />
  <section class="run-launcher">
    <div><p class="eyebrow">LOCKED INPUT SET</p><h2>Select cue versions</h2><p>{{ cues.locked.length }} locked cues available. Dependencies must be included and sequence numbers must be unique.</p></div>
    <el-select v-model="selectedCueIds" multiple collapse-tags collapse-tags-tooltip placeholder="Choose locked cues">
      <el-option v-for="cue in cues.locked" :key="cue.id" :value="cue.id" :label="`${cue.cue_code} · v${cue.version} · ${cue.name}`" />
    </el-select>
    <el-button v-if="canProgram" type="primary" :icon="Play" :loading="runs.running" :disabled="selectedCueIds.length === 0" @click="run">Run offline rehearsal</el-button>
  </section>
  <div class="run-selector">
    <button v-for="item in runs.items" :key="item.id" :class="{ selected: runs.selectedId === item.id }" @click="runs.selectedId = item.id">
      <span>#{{ item.id }}</span><strong>{{ item.cue_set_version }}</strong><el-tag :type="statusType(item.run_status)" effect="plain">{{ item.run_status.replaceAll('_',' ') }}</el-tag><small>{{ formatTimestamp(item.finished_at) }}</small>
    </button>
  </div>
  <template v-if="selected">
    <section class="run-summary" :class="selected.highest_severity">
      <div><p class="eyebrow">RUN #{{ selected.id }} · VERSION {{ selected.version }}</p><h2>{{ selected.run_status.replaceAll('_', ' ') }}</h2></div>
      <div class="run-stat"><span>Highest evidence</span><strong>{{ selected.highest_severity }}</strong></div>
      <div class="run-stat"><span>Rule results</span><strong>{{ selected.rule_results.length }}</strong></div>
      <div class="run-stat"><span>Collision windows</span><strong>{{ selected.collision_windows.length }}</strong></div>
      <span v-if="polling" class="polling"><RefreshCw :size="14" /> refreshing</span>
    </section>
    <section class="data-section timeline-section">
      <div class="section-heading"><div><p class="eyebrow">TIMELINE SNAPSHOT</p><h2>{{ selected.cue_set_version }}</h2></div><span>{{ selected.timeline_snapshot.timeline_step_ms }} ms evaluation step</span></div>
      <TimelineTrack :events="selected.timeline_snapshot.timeline" />
    </section>
    <div class="evidence-layout">
      <section class="data-section">
        <div class="section-heading"><div><p class="eyebrow">RULE OUTPUT</p><h2>Interlock evidence</h2></div><span>{{ selected.rule_results.length }} checks</span></div>
        <RuleEvidenceTable :evidence="selected.rule_results" />
      </section>
      <aside class="assumption-panel">
        <p class="eyebrow">SNAPSHOT ASSUMPTIONS</p>
        <ul><li v-for="assumption in selected.timeline_snapshot.assumptions" :key="assumption">{{ assumption }}</li></ul>
        <p class="eyebrow">COLLISION WINDOWS</p>
        <div v-if="selected.collision_windows.length === 0" class="empty-inline">No shared-zone overlap in this snapshot.</div>
        <div v-for="window in selected.collision_windows" :key="`${window.safety_zone}-${window.start_ms}`" class="collision-row"><strong>{{ window.safety_zone }}</strong><span>{{ window.cue_codes.join(' + ') }}</span><small>{{ window.start_ms }}–{{ window.end_ms }} ms</small></div>
      </aside>
    </div>
    <section v-if="blockers.length > 0" class="data-section disposition-panel">
      <div class="section-heading">
        <div><p class="eyebrow">BLOCKER DISPOSITIONS</p><h2>{{ blockers.length }} blocker item(s)</h2></div>
        <el-tag :type="canApprove ? 'success' : 'warning'" effect="plain">{{ registeredCount }}/{{ blockers.length }} registered · {{ gaps.needsRectification.length }} for rectification</el-tag>
      </div>
      <p class="disposition-hint">Blocker or invalid runs can be submitted for review, but approval is withheld until every item below is registered. Accepted items require a written acceptance reason; items not accepted must be marked for rectification.</p>
      <el-alert v-if="inReview && !canApprove" type="warning" :closable="false" show-icon class="gap-alert">
        <template #title>Approval cannot pass yet</template>
        <div v-if="gaps.unregistered.length > 0">Missing disposition ({{ gaps.unregistered.length }}): <strong>{{ gaps.unregistered.map((item) => `${item.evidence_key} · ${item.rule_code}`).join('；') }}</strong></div>
        <div v-if="gaps.needsRectification.length > 0">Marked for rectification ({{ gaps.needsRectification.length }}): <strong>{{ gaps.needsRectification.map((item) => `${item.evidence_key} · ${item.rule_code}`).join('；') }}</strong></div>
      </el-alert>
      <article v-for="evidence in blockers" :key="evidence.evidence_key" class="disposition-row">
        <header>
          <el-tag type="danger" effect="dark" size="small">{{ evidence.result }}</el-tag>
          <strong>{{ evidence.evidence_key }} · {{ evidence.rule_code }}</strong>
          <span class="subtle">{{ scopeLine(evidence) }}</span>
        </header>
        <p class="disposition-message">{{ evidence.message }} — actual {{ evidence.actual_value }} / threshold {{ evidence.threshold_value }} {{ evidence.unit }}</p>
        <div v-if="decisionFor(evidence)" class="disposition-record">
          <el-tag :type="tagType(decisionFor(evidence)?.decision)" effect="plain">
            <component :is="decisionFor(evidence)?.decision === 'accepted' ? ClipboardCheck : Hammer" :size="13" />
            {{ decisionFor(evidence)?.decision === 'accepted' ? 'accepted' : 'needs rectification' }}
          </el-tag>
          <span class="record-meta"><UserRound :size="13" /> {{ decisionFor(evidence)?.reviewer }} · {{ formatTimestamp(decisionFor(evidence)?.disposed_at) }}</span>
          <p class="record-reason">{{ decisionFor(evidence)?.reason }}</p>
        </div>
        <div v-if="canReview && inReview" class="disposition-form">
          <el-radio-group v-model="draftDecision[evidence.evidence_key]" @change="startDraft(evidence)">
            <el-radio-button label="accepted">Accept with reason</el-radio-button>
            <el-radio-button label="needs_rectification">Mark for rectification</el-radio-button>
          </el-radio-group>
          <el-input v-model="draftReason[evidence.evidence_key]" type="textarea" :rows="2" maxlength="500" show-word-limit placeholder="Write the acceptance justification, or the required rectification." @focus="startDraft(evidence)" />
          <el-button type="primary" :icon="ClipboardCheck" :loading="savingKey === evidence.evidence_key" @click="saveDisposition(evidence)">
            {{ decisionFor(evidence) ? 'Update disposition' : 'Register disposition' }}
          </el-button>
        </div>
      </article>
    </section>
    <section class="review-strip">
      <el-input v-model="reason" type="textarea" :rows="2" maxlength="500" show-word-limit />
      <div>
        <el-button v-if="canProgram && (selected.run_status === 'evaluated' || selected.run_status === 'blocked')" type="primary" :icon="Send" @click="submit">Submit safety review</el-button>
        <el-button v-if="canReview && selected.run_status === 'pending_review'" type="success" :icon="CheckCircle2" @click="review('approve')">Approve rehearsal evidence</el-button>
        <el-button v-if="canReview && selected.run_status === 'pending_review'" :icon="XCircle" @click="review('reject')">Reject evidence</el-button>
      </div>
      <p v-if="canReview && selected.run_status === 'pending_review' && blockers.length > 0 && !canApprove" class="gate-note">
        Approval stays blocked until all {{ blockers.length }} blocker item(s) are registered as accepted. {{ gaps.unregistered.length }} unregistered, {{ gaps.needsRectification.length }} marked for rectification.
      </p>
      <p>Human approval records review of this offline snapshot only. It is not an executable cue, machinery release, or live safety authorization.</p>
    </section>
  </template>
  <div v-else class="empty-state"><Play :size="28" /><h2>No rehearsal selected</h2><p>Choose locked cue versions and run the deterministic evaluator.</p></div>
</template>

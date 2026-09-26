import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import BlockerDispositionPanel from './BlockerDispositionPanel.vue'
import type { RehearsalRun } from '../../types/rehearsal'

vi.mock('element-plus', () => ({ ElMessage: { success: vi.fn(), error: vi.fn() } }))

const blocker = (overrides: Partial<RehearsalRun['blockers'][number]> = {}): RehearsalRun['blockers'][number] => ({
  evidence_key: 'ev-first',
  rule_code: 'LOAD-ALL-01',
  rule_type: 'load_limit',
  result: 'blocker',
  severity: 'blocker',
  cue_codes: ['Q-010'],
  device_codes: ['TRUSS-FOH-01'],
  window_start_ms: 0,
  window_end_ms: 10000,
  actual_value: 900,
  threshold_value: 800,
  unit: 'kg',
  message: 'modeled load exceeds the rule threshold',
  disposition: null,
  ...overrides,
})

const run = (blockers: RehearsalRun['blockers']): RehearsalRun =>
  ({
    id: 7,
    cue_set_version: 'cue-set-x',
    run_status: 'pending_review',
    timeline_snapshot: { cue_set_version: 'cue-set-x', cue_ids: [1], cues: [], timeline: [], rule_versions: {}, timeline_step_ms: 250, assumptions: [] },
    rule_results: [],
    collision_windows: [],
    highest_severity: 'blocker',
    blockers,
    blocker_count: blockers.length,
    blockers_pending_disposition: blockers.filter((item) => !item.disposition).length,
    blockers_needs_rectification: blockers.filter((item) => item.disposition?.decision === 'needs_rectification').length,
    blockers_accepted: blockers.filter((item) => item.disposition?.decision === 'accepted').length,
    started_by: 1,
    reviewed_by: null,
    review_reason: '',
    version: 3,
    finished_at: '2026-09-26T00:00:00Z',
    reviewed_at: null,
    created_at: '2026-09-26T00:00:00Z',
  }) as RehearsalRun

const stubs = {
  'el-tag': { template: '<span class="tag"><slot /></span>' },
  'el-progress': { template: '<div />' },
  'el-alert': { template: '<div class="alert"><slot name="title" /><slot /></div>' },
  'el-radio-group': { template: '<div><slot /></div>' },
  'el-radio-button': { template: '<label />' },
  'el-input': { props: ['modelValue'], emits: ['update:modelValue'], template: '<textarea :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' },
  'el-button': { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' },
}

describe('BlockerDispositionPanel', () => {
  it('names every unregistered and rectified blocker in the approval gate', () => {
    const wrapper = mount(
      BlockerDispositionPanel,
      {
        props: {
          run: run([
            blocker(),
            blocker({ evidence_key: 'ev-second', rule_code: 'ZONE-A-01', cue_codes: ['Q-020'], disposition: { evidence_key: 'ev-second', rule_code: 'ZONE-A-01', decision: 'needs_rectification', reason: 'rework', reviewed_by: 2, reviewer_name: 'reviewer', registered_at: '2026-09-26T01:00:00Z' } }),
          ]),
          canRegister: false,
          register: vi.fn(),
        },
        global: { stubs, config: { warnHandler: () => undefined } },
      },
    )
    const gate = wrapper.find('.alert').text()
    expect(gate).toContain('Missing disposition: 1')
    expect(gate).toContain('Marked for rectification: 1')
    expect(gate).toContain('LOAD-ALL-01')
    expect(gate).toContain('ZONE-A-01')
  })

  it('requires a written reason when registering an acceptance', async () => {
    const register = vi.fn()
    const wrapper = mount(
      BlockerDispositionPanel,
      { props: { run: run([blocker()]), canRegister: true, register }, global: { stubs, config: { warnHandler: () => undefined } } },
    )
    await wrapper.find('.blocker-editor button').trigger('click')
    expect(register).not.toHaveBeenCalled()

    await wrapper.find('textarea').setValue('Load verified tolerable for offline rehearsal planning.')
    await wrapper.find('.blocker-editor button').trigger('click')
    expect(register).toHaveBeenCalledTimes(1)
    expect(register).toHaveBeenCalledWith(expect.anything(), 'ev-first', 'accepted', expect.stringContaining('tolerable'))
  })
})

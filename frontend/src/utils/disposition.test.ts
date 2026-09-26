import { describe, expect, it } from 'vitest'
import { ApiError } from '../api/client'
import { allBlockersAccepted, blockerEvidence, dispositionGaps, formatDispositionError } from './disposition'
import type { RehearsalRun } from '../types/rehearsal'
import type { RuleEvidence } from '../types/interlock'

const evidence = (evidence_key: string, result: RuleEvidence['result']): RuleEvidence => ({
  evidence_key,
  rule_code: `RULE-${evidence_key}`,
  rule_type: 'load_limit',
  result,
  severity: result,
  cue_codes: ['Q-1'],
  device_codes: ['D-1'],
  window_start_ms: 0,
  window_end_ms: 1000,
  actual_value: 900,
  threshold_value: 600,
  unit: 'kg',
  message: 'over threshold',
})

const runWith = (results: RuleEvidence[], dispositions: RehearsalRun['blocker_dispositions'] = []): RehearsalRun =>
  ({ id: 1, rule_results: results, blocker_dispositions: dispositions } as unknown as RehearsalRun)

describe('blocker disposition helpers', () => {
  it('selects only blocker and invalid evidence', () => {
    const run = runWith([evidence('evidence-0', 'pass'), evidence('evidence-1', 'warning'), evidence('evidence-2', 'blocker'), evidence('evidence-3', 'invalid')])
    expect(blockerEvidence(run).map((item) => item.evidence_key)).toEqual(['evidence-2', 'evidence-3'])
  })

  it('reports every unregistered blocker item', () => {
    const run = runWith([evidence('evidence-0', 'blocker'), evidence('evidence-1', 'invalid')])
    const gaps = dispositionGaps(run)
    expect(gaps.unregistered.map((item) => item.evidence_key)).toEqual(['evidence-0', 'evidence-1'])
    expect(gaps.needsRectification).toEqual([])
    expect(allBlockersAccepted(run)).toBe(false)
  })

  it('keeps approval blocked when an item needs rectification', () => {
    const run = runWith([evidence('evidence-0', 'blocker')], [
      { evidence_key: 'evidence-0', rule_code: 'RULE-evidence-0', decision: 'needs_rectification', reason: 'fix it', reviewer_id: 2, reviewer: 'reviewer', disposed_at: '2026-09-26T00:00:00Z' },
    ])
    const gaps = dispositionGaps(run)
    expect(gaps.unregistered).toEqual([])
    expect(gaps.needsRectification.map((item) => item.evidence_key)).toEqual(['evidence-0'])
    expect(allBlockersAccepted(run)).toBe(false)
  })

  it('clears approval when every blocker is accepted', () => {
    const run = runWith([evidence('evidence-0', 'blocker'), evidence('evidence-1', 'warning')], [
      { evidence_key: 'evidence-0', rule_code: 'RULE-evidence-0', decision: 'accepted', reason: 'justified', reviewer_id: 2, reviewer: 'reviewer', disposed_at: '2026-09-26T00:00:00Z' },
    ])
    expect(allBlockersAccepted(run)).toBe(true)
  })

  it('passes a run without blocker evidence', () => {
    const run = runWith([evidence('evidence-0', 'pass')])
    expect(allBlockersAccepted(run)).toBe(true)
  })

  it('formats server gap details with the exact missing items', () => {
    const error = new ApiError(422, 'BLOCKER_DISPOSITION_INCOMPLETE', 'approval withheld: 1 blocker evidence item(s) still lack a disposition and 1 are marked for rectification', {
      unregistered: [{ evidence_key: 'evidence-0', rule_code: 'LOAD-1', result: 'blocker' }],
      needs_rectification: [{ evidence_key: 'evidence-2', rule_code: 'TRAVEL-1', result: 'invalid' }],
    }, 'req-1')
    const message = formatDispositionError(error)
    expect(message).toContain('evidence-0 (LOAD-1 / blocker)')
    expect(message).toContain('evidence-2 (TRAVEL-1 / invalid)')
  })
})

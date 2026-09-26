import { ApiError, errorMessage } from '../api/client'
import type { RuleEvidence } from '../types/interlock'
import type { BlockerDisposition, RehearsalRun } from '../types/rehearsal'

/** Evidence items a safety reviewer must disposition before approval. */
export function blockerEvidence(run: Pick<RehearsalRun, 'rule_results'>): RuleEvidence[] {
  return run.rule_results.filter((item) => item.result === 'blocker' || item.result === 'invalid')
}

export function dispositionFor(run: Pick<RehearsalRun, 'blocker_dispositions'>, evidenceKey: string): BlockerDisposition | undefined {
  return run.blocker_dispositions?.find((item) => item.evidence_key === evidenceKey)
}

export interface DispositionGaps {
  unregistered: RuleEvidence[]
  needsRectification: RuleEvidence[]
}

/** Every blocker item that still withholds approval: missing disposition or marked for rectification. */
export function dispositionGaps(run: RehearsalRun): DispositionGaps {
  const gaps: DispositionGaps = { unregistered: [], needsRectification: [] }
  for (const evidence of blockerEvidence(run)) {
    const disposition = dispositionFor(run, evidence.evidence_key)
    if (!disposition) {
      gaps.unregistered.push(evidence)
    } else if (disposition.decision === 'needs_rectification') {
      gaps.needsRectification.push(evidence)
    }
  }
  return gaps
}

export function allBlockersAccepted(run: RehearsalRun): boolean {
  if (blockerEvidence(run).length === 0) return true
  const gaps = dispositionGaps(run)
  return gaps.unregistered.length === 0 && gaps.needsRectification.length === 0
}

interface GapDetail {
  evidence_key?: string
  rule_code?: string
  result?: string
}

interface GapDetails {
  unregistered?: GapDetail[]
  needs_rectification?: GapDetail[]
}

function describeGap(label: string, gaps: GapDetail[]): string {
  if (gaps.length === 0) return ''
  const items = gaps.map((gap) => `${gap.evidence_key ?? 'unknown'} (${gap.rule_code ?? 'unknown rule'} / ${gap.result ?? 'unknown result'})`).join('; ')
  return `${label}: ${items}`
}

/**
 * Renders the server's BLOCKER_DISPOSITION_INCOMPLETE details so reviewers can
 * see exactly which blocker items still withhold approval.
 */
export function formatDispositionError(error: unknown): string {
  if (!(error instanceof ApiError) || error.code !== 'BLOCKER_DISPOSITION_INCOMPLETE') {
    return errorMessage(error)
  }
  const details = error.details as GapDetails | null
  const unregistered = details?.unregistered ?? []
  const rectify = details?.needs_rectification ?? []
  const parts = [
    describeGap(`${unregistered.length} item(s) still need a disposition`, unregistered),
    describeGap(`${rectify.length} item(s) are marked for rectification`, rectify),
  ].filter(Boolean)
  const detail = parts.length > 0 ? ` ${parts.join(' · ')}` : ''
  return `${error.code}: ${error.message}${detail}`
}

import type { InterlockResult, RuleEvidence } from './interlock'

export type RunStatus = 'evaluated' | 'blocked' | 'pending_review' | 'approved_for_rehearsal' | 'rejected'

export type BlockerDecision = 'accepted' | 'needs_rectification'

export interface BlockerDisposition {
  evidence_key: string
  rule_code: string
  decision: BlockerDecision
  reason: string
  reviewed_by: number
  reviewer_name: string
  registered_at: string
}

export interface BlockerEvidence extends RuleEvidence {
  evidence_key: string
  disposition: BlockerDisposition | null
}

export interface GraphCue {
  id: number
  cue_code: string
  sequence_no: number
  start_offset_ms: number
  duration_ms: number
  dependency_ids: number[] | null
}

export interface TimelineEvent {
  cue_id: number
  cue_code: string
  cue_sequence: number
  cue_version: number
  device_id: number
  device_code: string
  device_name: string
  safety_zone: string
  start_ms: number
  end_ms: number
  from_position_m: number
  to_position_m: number
  load_kg: number
  speed_ms: number
}

export interface CollisionWindow {
  safety_zone: string
  cue_codes: string[]
  device_ids: number[]
  device_codes: string[]
  start_ms: number
  end_ms: number
}

export interface TimelineSnapshot {
  cue_set_version: string
  cue_ids: number[]
  cues: GraphCue[]
  timeline: TimelineEvent[]
  rule_versions: Record<string, number>
  timeline_step_ms: number
  assumptions: string[]
}

export interface RehearsalRun {
  id: number
  cue_set_version: string
  run_status: RunStatus
  timeline_snapshot: TimelineSnapshot
  rule_results: RuleEvidence[]
  collision_windows: CollisionWindow[]
  highest_severity: InterlockResult
  blockers: BlockerEvidence[]
  blocker_count: number
  blockers_pending_disposition: number
  blockers_needs_rectification: number
  blockers_accepted: number
  started_by: number
  reviewed_by: number | null
  review_reason: string
  version: number
  finished_at: string
  reviewed_at: string | null
  created_at: string
}

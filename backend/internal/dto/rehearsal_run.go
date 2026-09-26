package dto

import (
	"encoding/json"
	"fmt"
	"time"

	"stage-rigging-cue-interlock/backend/internal/constants"
	"stage-rigging-cue-interlock/backend/internal/interlock"
	"stage-rigging-cue-interlock/backend/internal/model"
)

type RunRehearsalRequest struct {
	CueIDs []uint `json:"cue_ids" binding:"required,min=1,max=40,dive,gte=1"`
}

type RunTransitionRequest struct {
	Version uint   `json:"version" binding:"required,gte=1"`
	Reason  string `json:"reason" binding:"required,min=4,max=500"`
}

type ReviewRunRequest struct {
	Version  uint   `json:"version" binding:"required,gte=1"`
	Decision string `json:"decision" binding:"required,oneof=approve reject"`
	Reason   string `json:"reason" binding:"required,min=4,max=500"`
}

// RegisterBlockerDispositionRequest registers one reviewer decision against
// one piece of blocking evidence. Both decisions require a written reason:
// accepted explains why the offline evidence is tolerable for rehearsal
// planning, needs_rectification flags the cue set for rework.
type RegisterBlockerDispositionRequest struct {
	Version     uint   `json:"version" binding:"required,gte=1"`
	EvidenceKey string `json:"evidence_key" binding:"required,min=8,max=64,startswith=ev-"`
	Decision    string `json:"decision" binding:"required,oneof=accepted needs_rectification"`
	Reason      string `json:"reason" binding:"required,min=4,max=500"`
}

// BlockerDispositionResponse is the persisted reviewer decision on a blocker.
type BlockerDispositionResponse struct {
	EvidenceKey  string    `json:"evidence_key"`
	RuleCode     string    `json:"rule_code"`
	Decision     string    `json:"decision"`
	Reason       string    `json:"reason"`
	ReviewedBy   uint      `json:"reviewed_by"`
	ReviewerName string    `json:"reviewer_name"`
	RegisteredAt time.Time `json:"registered_at"`
}

// BlockerEvidenceView pairs one blocking evidence row with its stable key and
// the reviewer disposition (nil until the reviewer registers one).
type BlockerEvidenceView struct {
	interlock.RuleEvidence
	EvidenceKey string                      `json:"evidence_key"`
	Disposition *BlockerDispositionResponse `json:"disposition"`
}

type TimelineSnapshot struct {
	CueSetVersion  string                    `json:"cue_set_version"`
	CueIDs         []uint                    `json:"cue_ids"`
	Cues           []interlock.GraphCue      `json:"cues"`
	CueInputs      []interlock.CueInput      `json:"cue_inputs"`
	DeviceInputs   []interlock.DeviceInput   `json:"device_inputs"`
	RuleInputs     []interlock.RuleInput     `json:"rule_inputs"`
	Timeline       []interlock.TimelineEvent `json:"timeline"`
	RuleVersions   map[string]uint           `json:"rule_versions"`
	TimelineStepMS int64                     `json:"timeline_step_ms"`
	Assumptions    []string                  `json:"assumptions"`
}

type RehearsalRunResponse struct {
	ID                         uint                        `json:"id"`
	CueSetVersion              string                      `json:"cue_set_version"`
	RunStatus                  constants.RehearsalStatus   `json:"run_status"`
	TimelineSnapshot           TimelineSnapshot            `json:"timeline_snapshot"`
	RuleResults                []interlock.RuleEvidence    `json:"rule_results"`
	CollisionWindows           []interlock.CollisionWindow `json:"collision_windows"`
	HighestSeverity            constants.InterlockResult   `json:"highest_severity"`
	Blockers                   []BlockerEvidenceView       `json:"blockers"`
	BlockerCount               int                         `json:"blocker_count"`
	BlockersPendingDisposition int                         `json:"blockers_pending_disposition"`
	BlockersNeedsRectification int                         `json:"blockers_needs_rectification"`
	BlockersAccepted           int                         `json:"blockers_accepted"`
	StartedBy                  uint                        `json:"started_by"`
	ReviewedBy                 *uint                       `json:"reviewed_by"`
	ReviewReason               string                      `json:"review_reason"`
	Version                    uint                        `json:"version"`
	FinishedAt                 time.Time                   `json:"finished_at"`
	ReviewedAt                 *time.Time                  `json:"reviewed_at"`
	CreatedAt                  time.Time                   `json:"created_at"`
}

// RunFromModel maps a persisted run, attaching reviewer blocker dispositions
// keyed by evidence_key. Pass a nil slice for runs without dispositions.
func RunFromModel(item model.RehearsalRun, dispositions []model.BlockerDisposition) (RehearsalRunResponse, error) {
	snapshot := TimelineSnapshot{}
	if err := json.Unmarshal(item.TimelineSnapshotJSON, &snapshot); err != nil {
		return RehearsalRunResponse{}, fmt.Errorf("decode run %d timeline snapshot: %w", item.ID, err)
	}
	results := []interlock.RuleEvidence{}
	if err := json.Unmarshal(item.RuleResultsJSON, &results); err != nil {
		return RehearsalRunResponse{}, fmt.Errorf("decode run %d rule results: %w", item.ID, err)
	}
	windows := []interlock.CollisionWindow{}
	if err := json.Unmarshal(item.CollisionWindowsJSON, &windows); err != nil {
		return RehearsalRunResponse{}, fmt.Errorf("decode run %d collision windows: %w", item.ID, err)
	}
	byKey := make(map[string]model.BlockerDisposition, len(dispositions))
	for _, disposition := range dispositions {
		byKey[disposition.EvidenceKey] = disposition
	}
	blockers := make([]BlockerEvidenceView, 0)
	for _, result := range results {
		if !result.Result.Blocking() {
			continue
		}
		view := BlockerEvidenceView{RuleEvidence: result, EvidenceKey: interlock.EvidenceKey(result)}
		if disposition, ok := byKey[view.EvidenceKey]; ok {
			response := DispositionFromModel(disposition)
			view.Disposition = &response
		}
		blockers = append(blockers, view)
	}
	response := RehearsalRunResponse{ID: item.ID, CueSetVersion: item.CueSetVersion, RunStatus: constants.RehearsalStatus(item.RunStatus), TimelineSnapshot: snapshot, RuleResults: results, CollisionWindows: windows, HighestSeverity: constants.InterlockResult(item.HighestSeverity), Blockers: blockers, BlockerCount: len(blockers), StartedBy: item.StartedBy, ReviewedBy: item.ReviewedBy, ReviewReason: item.ReviewReason, Version: item.Version, FinishedAt: item.FinishedAt, ReviewedAt: item.ReviewedAt, CreatedAt: item.CreatedAt}
	for _, blocker := range blockers {
		switch {
		case blocker.Disposition == nil:
			response.BlockersPendingDisposition++
		case blocker.Disposition.Decision == string(constants.DispositionNeedsRectification):
			response.BlockersNeedsRectification++
		case blocker.Disposition.Decision == string(constants.DispositionAccepted):
			response.BlockersAccepted++
		}
	}
	return response, nil
}

// DispositionFromModel maps one persisted blocker disposition to its API view.
func DispositionFromModel(item model.BlockerDisposition) BlockerDispositionResponse {
	return BlockerDispositionResponse{EvidenceKey: item.EvidenceKey, RuleCode: item.RuleCode, Decision: item.Decision, Reason: item.Reason, ReviewedBy: item.ReviewedBy, ReviewerName: item.ReviewerName, RegisteredAt: item.RegisteredAt}
}

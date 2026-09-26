package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"stage-rigging-cue-interlock/backend/internal/audit"
	"stage-rigging-cue-interlock/backend/internal/constants"
	"stage-rigging-cue-interlock/backend/internal/dto"
	"stage-rigging-cue-interlock/backend/internal/interlock"
	"stage-rigging-cue-interlock/backend/internal/model"
	"stage-rigging-cue-interlock/backend/internal/repository"
	"stage-rigging-cue-interlock/backend/internal/util"

	"gorm.io/datatypes"
)

type RehearsalRunService struct {
	runs           *repository.RehearsalRunRepository
	cues           *repository.CueDefinitionRepository
	devices        *repository.RiggingDeviceRepository
	rules          *repository.InterlockRuleRepository
	timelineStepMS int64
	maxCues        int
}

func NewRehearsalRunService(runs *repository.RehearsalRunRepository, cues *repository.CueDefinitionRepository, devices *repository.RiggingDeviceRepository, rules *repository.InterlockRuleRepository, timelineStepMS int64, maxCues int) *RehearsalRunService {
	return &RehearsalRunService{runs: runs, cues: cues, devices: devices, rules: rules, timelineStepMS: timelineStepMS, maxCues: maxCues}
}

func (s *RehearsalRunService) List(page, pageSize int, status, severity, search string) ([]dto.RehearsalRunResponse, int64, error) {
	items, total, err := s.runs.List(page, pageSize, status, severity, search)
	if err != nil {
		return nil, 0, err
	}
	responses := make([]dto.RehearsalRunResponse, 0, len(items))
	for _, item := range items {
		response, mapErr := dto.RunFromModel(item)
		if mapErr != nil {
			return nil, 0, mapErr
		}
		responses = append(responses, response)
	}
	return responses, total, nil
}

func (s *RehearsalRunService) Get(id uint) (dto.RehearsalRunResponse, error) {
	item, err := s.runs.Get(id)
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	return dto.RunFromModel(item)
}

func (s *RehearsalRunService) Run(request dto.RunRehearsalRequest, actor audit.ActorContext) (dto.RehearsalRunResponse, error) {
	if len(request.CueIDs) > s.maxCues {
		return dto.RehearsalRunResponse{}, util.Unprocessable("CUE_SET_TOO_LARGE", "selected cue count exceeds the configured rehearsal limit", map[string]any{"maximum": s.maxCues, "actual": len(request.CueIDs)})
	}
	if len(uniqueIDSlice(request.CueIDs)) != len(request.CueIDs) {
		return dto.RehearsalRunResponse{}, util.Unprocessable("DUPLICATE_CUE_SELECTION", "cue_ids must not contain duplicates", nil)
	}
	cueModels, err := s.cues.ByIDs(request.CueIDs)
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	cueInputs := make([]interlock.CueInput, 0, len(cueModels))
	for _, cueModel := range cueModels {
		if constants.CueStatus(cueModel.CueStatus) != constants.CueLocked {
			return dto.RehearsalRunResponse{}, util.Unprocessable("CUE_NOT_LOCKED", "only approved and locked cue versions may be rehearsed", map[string]any{"cue_code": cueModel.CueCode, "cue_status": cueModel.CueStatus, "version": cueModel.Version})
		}
		cueResponse, mapErr := dto.CueFromModel(cueModel)
		if mapErr != nil {
			return dto.RehearsalRunResponse{}, mapErr
		}
		actions := make([]interlock.ActionInput, 0, len(cueResponse.Actions))
		for _, action := range cueResponse.Actions {
			actions = append(actions, interlock.ActionInput{DeviceID: action.DeviceID, StartOffsetMS: action.StartOffsetMS, DurationMS: action.DurationMS, FromPositionM: action.FromPositionM, ToPositionM: action.ToPositionM, LoadKG: action.LoadKG})
		}
		cueInputs = append(cueInputs, interlock.CueInput{GraphCue: interlock.GraphCue{ID: cueResponse.ID, CueCode: cueResponse.CueCode, SequenceNo: cueResponse.SequenceNo, StartOffsetMS: cueResponse.StartOffsetMS, DurationMS: cueResponse.DurationMS, DependencyIDs: cueResponse.DependencyIDs}, Version: cueResponse.Version, Actions: actions})
	}
	deviceModels, err := s.devices.All()
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	deviceInputs := make([]interlock.DeviceInput, 0, len(deviceModels))
	for _, device := range deviceModels {
		deviceInputs = append(deviceInputs, interlock.DeviceInput{ID: device.ID, DeviceCode: device.DeviceCode, Name: device.Name, MaxLoadKG: device.MaxLoadKG, MaxSpeedMS: device.MaxSpeedMS, TravelMinM: device.TravelMinM, TravelMaxM: device.TravelMaxM, SafetyZone: device.SafetyZone, DeviceStatus: device.DeviceStatus})
	}
	ruleModels, err := s.rules.Enabled()
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	ruleInputs := make([]interlock.RuleInput, 0, len(ruleModels))
	for _, ruleModel := range ruleModels {
		ruleResponse, mapErr := dto.RuleFromModel(ruleModel)
		if mapErr != nil {
			return dto.RehearsalRunResponse{}, mapErr
		}
		ruleInputs = append(ruleInputs, interlock.RuleInput{ID: ruleResponse.ID, RuleCode: ruleResponse.RuleCode, RuleType: ruleResponse.RuleType, DeviceIDs: ruleResponse.DeviceIDs, Threshold: ruleResponse.Threshold, Severity: ruleResponse.Severity, Enabled: ruleResponse.Enabled, RuleVersion: ruleResponse.RuleVersion, Explanation: ruleResponse.Explanation})
	}
	evaluation, err := interlock.Evaluate(cueInputs, deviceInputs, ruleInputs, s.timelineStepMS)
	if err != nil {
		var graphErr *interlock.GraphError
		if errors.As(err, &graphErr) {
			return dto.RehearsalRunResponse{}, util.Unprocessable(graphErr.Code, graphErr.Message, graphErr)
		}
		return dto.RehearsalRunResponse{}, util.Unprocessable("REHEARSAL_INPUT_INVALID", err.Error(), nil)
	}
	version := cueSetVersion(cueInputs, ruleInputs, deviceModels)
	cueIDs := append([]uint(nil), request.CueIDs...)
	sort.Slice(cueIDs, func(i, j int) bool { return cueIDs[i] < cueIDs[j] })
	ruleVersions := make(map[string]uint, len(ruleInputs))
	for _, rule := range ruleInputs {
		ruleVersions[rule.RuleCode] = rule.RuleVersion
	}
	snapshot := dto.TimelineSnapshot{
		CueSetVersion: version, CueIDs: cueIDs, Cues: evaluation.OrderedCues,
		CueInputs: cueInputs, DeviceInputs: deviceInputs, RuleInputs: ruleInputs,
		Timeline: evaluation.Timeline, RuleVersions: ruleVersions, TimelineStepMS: evaluation.TimelineStepMS, Assumptions: evaluation.Assumptions,
	}
	snapshotJSON, resultsJSON, windowsJSON, err := marshalEvaluation(snapshot, evaluation)
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	status := constants.RunEvaluated
	if evaluation.HighestSeverity == constants.ResultBlocker || evaluation.HighestSeverity == constants.ResultInvalid {
		status = constants.RunBlocked
	}
	now := time.Now().UTC()
	item := model.RehearsalRun{CueSetVersion: version, RunStatus: string(status), TimelineSnapshotJSON: snapshotJSON, RuleResultsJSON: resultsJSON, CollisionWindowsJSON: windowsJSON, HighestSeverity: string(evaluation.HighestSeverity), BlockerDispositionsJSON: datatypes.JSON([]byte("[]")), StartedBy: actor.ID, Version: 1, FinishedAt: now}
	after := util.SummaryJSON(map[string]any{"cue_set_version": version, "cue_ids": cueIDs, "run_status": status, "highest_severity": evaluation.HighestSeverity, "result_count": len(evaluation.RuleResults), "collision_window_count": len(evaluation.CollisionWindows), "rule_versions": ruleVersions})
	if err := s.runs.Create(&item, audit.NewEvent(actor, "rehearsal_run.evaluate", "rehearsal_run", 0, "{}", after)); err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	return dto.RunFromModel(item)
}

func (s *RehearsalRunService) Submit(id uint, request dto.RunTransitionRequest, actor audit.ActorContext) (dto.RehearsalRunResponse, error) {
	current, err := s.runs.Get(id)
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	from := constants.RehearsalStatus(current.RunStatus)
	if !constants.CanTransitionRun(from, constants.RunPendingReview) {
		return dto.RehearsalRunResponse{}, util.Unprocessable("INVALID_RUN_TRANSITION", "only an evaluated or blocker-flagged run can be submitted", map[string]any{"current_status": from})
	}
	before := runStateSummary(current)
	after := util.SummaryJSON(map[string]any{"run_status": constants.RunPendingReview, "version": request.Version + 1, "reason": request.Reason})
	updated, err := s.runs.Transition(id, request.Version, from, constants.RunPendingReview, nil, strings.TrimSpace(request.Reason), audit.NewEvent(actor, "rehearsal_run.submit_review", "rehearsal_run", id, before, after))
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	return dto.RunFromModel(updated)
}

func (s *RehearsalRunService) Review(id uint, request dto.ReviewRunRequest, actor audit.ActorContext) (dto.RehearsalRunResponse, error) {
	current, err := s.runs.Get(id)
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	from := constants.RehearsalStatus(current.RunStatus)
	target := constants.RunRejected
	action := "rehearsal_run.reject"
	if request.Decision == "approve" {
		target = constants.RunApproved
		action = "rehearsal_run.approve"
		if gaps, gapErr := approvalGapsForRun(current); gapErr != nil {
			return dto.RehearsalRunResponse{}, gapErr
		} else if len(gaps.Unregistered) > 0 || len(gaps.NeedsRectification) > 0 {
			return dto.RehearsalRunResponse{}, util.Unprocessable("BLOCKER_DISPOSITION_INCOMPLETE", fmt.Sprintf("approval withheld: %d blocker evidence item(s) still lack a disposition and %d are marked for rectification", len(gaps.Unregistered), len(gaps.NeedsRectification)), map[string]any{"unregistered": gaps.Unregistered, "needs_rectification": gaps.NeedsRectification})
		}
	}
	if !constants.CanTransitionRun(from, target) {
		return dto.RehearsalRunResponse{}, util.Unprocessable("INVALID_RUN_TRANSITION", fmt.Sprintf("cannot transition run from %s to %s", from, target), map[string]any{"from": from, "to": target})
	}
	before := runStateSummary(current)
	after := util.SummaryJSON(map[string]any{"run_status": target, "version": request.Version + 1, "reviewer_id": actor.ID, "reason": request.Reason, "boundary": "offline rehearsal approval only; no machinery command or operational clearance"})
	updated, err := s.runs.Transition(id, request.Version, from, target, &actor.ID, strings.TrimSpace(request.Reason), audit.NewEvent(actor, action, "rehearsal_run", id, before, after))
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	return dto.RunFromModel(updated)
}

// RegisterDisposition records how the reviewing safety reviewer handles one
// blocker or invalid evidence item. Accepted items need an acceptance
// justification; needs_rectification items keep approval blocked. Re-registering
// an item overwrites the previous disposition and bumps the run version.
func (s *RehearsalRunService) RegisterDisposition(id uint, request dto.RegisterDispositionRequest, actor audit.ActorContext) (dto.RehearsalRunResponse, error) {
	current, err := s.runs.Get(id)
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	run, err := dto.RunFromModel(current)
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	if run.RunStatus != constants.RunPendingReview {
		return dto.RehearsalRunResponse{}, util.Unprocessable("RUN_NOT_IN_REVIEW", "blocker dispositions can only be registered while the run is pending review", map[string]any{"run_status": run.RunStatus})
	}
	decision := constants.DispositionDecision(request.Decision)
	if !decision.Valid() {
		return dto.RehearsalRunResponse{}, util.Unprocessable("INVALID_DISPOSITION_DECISION", "decision must be accepted or needs_rectification", map[string]any{"decision": request.Decision})
	}
	targetEvidence := interlock.RuleEvidence{}
	found := false
	for _, evidence := range run.RuleResults {
		if evidence.EvidenceKey == request.EvidenceKey {
			targetEvidence = evidence
			found = true
			break
		}
	}
	if !found {
		return dto.RehearsalRunResponse{}, util.Unprocessable("DISPOSITION_TARGET_NOT_FOUND", "evidence_key does not match any evidence stored on this run", map[string]any{"evidence_key": request.EvidenceKey})
	}
	if !constants.DispositionRequired(targetEvidence.Result) {
		return dto.RehearsalRunResponse{}, util.Unprocessable("DISPOSITION_TARGET_NOT_BLOCKER", "only blocker or invalid evidence requires a disposition", map[string]any{"evidence_key": request.EvidenceKey, "result": targetEvidence.Result})
	}
	reason := strings.TrimSpace(request.Reason)
	dispositions := append([]dto.BlockerDisposition(nil), run.BlockerDispositions...)
	replaced := false
	for index := range dispositions {
		if dispositions[index].EvidenceKey == request.EvidenceKey {
			dispositions[index] = dto.BlockerDisposition{EvidenceKey: request.EvidenceKey, RuleCode: targetEvidence.RuleCode, Decision: string(decision), Reason: reason, ReviewerID: actor.ID, Reviewer: actor.Username, DisposedAt: time.Now().UTC()}
			replaced = true
			break
		}
	}
	if !replaced {
		dispositions = append(dispositions, dto.BlockerDisposition{EvidenceKey: request.EvidenceKey, RuleCode: targetEvidence.RuleCode, Decision: string(decision), Reason: reason, ReviewerID: actor.ID, Reviewer: actor.Username, DisposedAt: time.Now().UTC()})
	}
	sort.SliceStable(dispositions, func(i, j int) bool { return dispositions[i].EvidenceKey < dispositions[j].EvidenceKey })
	encoded, err := json.Marshal(dispositions)
	if err != nil {
		return dto.RehearsalRunResponse{}, fmt.Errorf("encode blocker dispositions: %w", err)
	}
	before := runStateSummary(current)
	after := util.SummaryJSON(map[string]any{"run_status": constants.RunPendingReview, "version": request.Version + 1, "evidence_key": request.EvidenceKey, "rule_code": targetEvidence.RuleCode, "decision": decision, "reviewer_id": actor.ID, "reviewer": actor.Username, "reason": reason})
	updated, err := s.runs.SaveDispositions(id, request.Version, datatypes.JSON(encoded), audit.NewEvent(actor, "rehearsal_run.blocker_disposition", "rehearsal_run", id, before, after))
	if err != nil {
		return dto.RehearsalRunResponse{}, err
	}
	return dto.RunFromModel(updated)
}

func (s *RehearsalRunService) Compare(id, otherID uint) (map[string]any, error) {
	left, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	right, err := s.Get(otherID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"left":     map[string]any{"id": left.ID, "cue_set_version": left.CueSetVersion, "status": left.RunStatus, "highest_severity": left.HighestSeverity, "result_count": len(left.RuleResults), "collision_window_count": len(left.CollisionWindows)},
		"right":    map[string]any{"id": right.ID, "cue_set_version": right.CueSetVersion, "status": right.RunStatus, "highest_severity": right.HighestSeverity, "result_count": len(right.RuleResults), "collision_window_count": len(right.CollisionWindows)},
		"delta":    map[string]any{"result_count": len(left.RuleResults) - len(right.RuleResults), "collision_windows": len(left.CollisionWindows) - len(right.CollisionWindows), "same_snapshot": left.CueSetVersion == right.CueSetVersion},
		"boundary": "Comparison is offline rehearsal evidence and does not indicate machinery readiness or permission to execute.",
	}, nil
}

func cueSetVersion(cues []interlock.CueInput, rules []interlock.RuleInput, devices []model.RiggingDevice) string {
	sortedCues := append([]interlock.CueInput(nil), cues...)
	sort.Slice(sortedCues, func(i, j int) bool { return sortedCues[i].ID < sortedCues[j].ID })
	sortedRules := append([]interlock.RuleInput(nil), rules...)
	sort.Slice(sortedRules, func(i, j int) bool { return sortedRules[i].ID < sortedRules[j].ID })
	sortedDevices := append([]model.RiggingDevice(nil), devices...)
	sort.Slice(sortedDevices, func(i, j int) bool { return sortedDevices[i].ID < sortedDevices[j].ID })
	parts := make([]string, 0, len(cues)+len(rules)+len(devices))
	for _, cue := range sortedCues {
		parts = append(parts, fmt.Sprintf("cue:%d:v%d", cue.ID, cue.Version))
	}
	for _, rule := range sortedRules {
		parts = append(parts, fmt.Sprintf("rule:%d:v%d", rule.ID, rule.RuleVersion))
	}
	for _, device := range sortedDevices {
		parts = append(parts, fmt.Sprintf("device:%d:v%d", device.ID, device.Version))
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "cue-set-" + hex.EncodeToString(digest[:8])
}

func marshalEvaluation(snapshot dto.TimelineSnapshot, evaluation interlock.Evaluation) (datatypes.JSON, datatypes.JSON, datatypes.JSON, error) {
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode timeline snapshot: %w", err)
	}
	resultsJSON, err := json.Marshal(evaluation.RuleResults)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode rule results: %w", err)
	}
	windowsJSON, err := json.Marshal(evaluation.CollisionWindows)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode collision windows: %w", err)
	}
	return datatypes.JSON(snapshotJSON), datatypes.JSON(resultsJSON), datatypes.JSON(windowsJSON), nil
}

func uniqueIDSlice(ids []uint) map[uint]struct{} {
	result := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		result[id] = struct{}{}
	}
	return result
}

func runStateSummary(item model.RehearsalRun) string {
	dispositions := []dto.BlockerDisposition{}
	if len(item.BlockerDispositionsJSON) > 0 {
		_ = json.Unmarshal(item.BlockerDispositionsJSON, &dispositions)
	}
	return util.SummaryJSON(map[string]any{"run_status": item.RunStatus, "version": item.Version, "highest_severity": item.HighestSeverity, "reviewed_by": item.ReviewedBy, "cue_set_version": item.CueSetVersion, "blocker_disposition_count": len(dispositions)})
}

// DispositionGap identifies one blocker evidence item that still prevents
// approval: either no disposition was registered, or the reviewer marked it
// for rectification.
type DispositionGap struct {
	EvidenceKey string `json:"evidence_key"`
	RuleCode    string `json:"rule_code"`
	Result      string `json:"result"`
}

// DispositionGaps reports every item that blocks approval.
type DispositionGaps struct {
	Unregistered       []DispositionGap `json:"unregistered"`
	NeedsRectification []DispositionGap `json:"needs_rectification"`
}

func approvalGapsForRun(item model.RehearsalRun) (DispositionGaps, error) {
	run, err := dto.RunFromModel(item)
	if err != nil {
		return DispositionGaps{}, err
	}
	return computeDispositionGaps(run.RuleResults, run.BlockerDispositions), nil
}

// computeDispositionGaps is the pure approval gate used by the service and its
// tests. Runs without blocker/invalid evidence always have no gaps.
func computeDispositionGaps(results []interlock.RuleEvidence, dispositions []dto.BlockerDisposition) DispositionGaps {
	byKey := make(map[string]dto.BlockerDisposition, len(dispositions))
	for _, disposition := range dispositions {
		byKey[disposition.EvidenceKey] = disposition
	}
	gaps := DispositionGaps{Unregistered: []DispositionGap{}, NeedsRectification: []DispositionGap{}}
	for _, evidence := range results {
		if !constants.DispositionRequired(evidence.Result) {
			continue
		}
		gap := DispositionGap{EvidenceKey: evidence.EvidenceKey, RuleCode: evidence.RuleCode, Result: string(evidence.Result)}
		disposition, registered := byKey[evidence.EvidenceKey]
		if !registered {
			gaps.Unregistered = append(gaps.Unregistered, gap)
			continue
		}
		if constants.DispositionDecision(disposition.Decision) == constants.DispositionNeedsRectification {
			gaps.NeedsRectification = append(gaps.NeedsRectification, gap)
		}
	}
	return gaps
}

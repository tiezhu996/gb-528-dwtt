package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"stage-rigging-cue-interlock/backend/internal/audit"
	"stage-rigging-cue-interlock/backend/internal/constants"
	"stage-rigging-cue-interlock/backend/internal/dto"
	"stage-rigging-cue-interlock/backend/internal/interlock"
	"stage-rigging-cue-interlock/backend/internal/model"
	"stage-rigging-cue-interlock/backend/internal/repository"
	"stage-rigging-cue-interlock/backend/internal/util"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var blockerFlowDBSeq atomic.Int64

type blockerFlowFixture struct {
	db        *gorm.DB
	runs      *RehearsalRunService
	audit     *audit.Repository
	reviewer  audit.ActorContext
	firstKey  string
	secondKey string
}

func setupBlockerFlow(t *testing.T) blockerFlowFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:blocker-flow-%d?mode=memory&cache=shared", blockerFlowDBSeq.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.RehearsalRun{}, &model.BlockerDisposition{}, &audit.Event{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	auditRepo := audit.NewRepository(db)
	runRepo := repository.NewRehearsalRunRepository(db, auditRepo)
	dispositionRepo := repository.NewBlockerDispositionRepository(db, auditRepo)
	runService := NewRehearsalRunService(runRepo, dispositionRepo, nil, nil, nil, 1000, 40)

	first := interlock.RuleEvidence{RuleCode: "LOAD-ALL-01", RuleType: "load_limit", Result: constants.ResultBlocker, CueCodes: []string{"Q-010"}, DeviceCodes: []string{"TRUSS-FOH-01"}, WindowStartMS: 0, WindowEndMS: 10000, ActualValue: 900, ThresholdValue: 800, Unit: "kg", Message: "modeled load exceeds the rule threshold"}
	second := interlock.RuleEvidence{RuleCode: "TRAVEL-ALL-01", RuleType: "travel_limit", Result: constants.ResultBlocker, CueCodes: []string{"Q-020"}, DeviceCodes: []string{"LX-BRIDGE-02"}, WindowStartMS: 11200, WindowEndMS: 21200, ActualValue: 3, ThresholdValue: 4, Unit: "m", Message: "modeled endpoint is below the travel minimum"}
	results, _ := json.Marshal([]interlock.RuleEvidence{first, second, {RuleCode: "SPEED-ALL-01", Result: constants.ResultWarning, Message: "slow"}})
	empty, _ := json.Marshal([]any{})
	snapshot, _ := json.Marshal(dto.TimelineSnapshot{CueSetVersion: "cue-set-test", CueIDs: []uint{1}, Timeline: []interlock.TimelineEvent{}})
	run := model.RehearsalRun{CueSetVersion: "cue-set-test", RunStatus: string(constants.RunBlocked), TimelineSnapshotJSON: datatypes.JSON(snapshot), RuleResultsJSON: datatypes.JSON(results), CollisionWindowsJSON: datatypes.JSON(empty), HighestSeverity: string(constants.ResultBlocker), StartedBy: 1, Version: 1, FinishedAt: time.Now().UTC()}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	return blockerFlowFixture{db: db, runs: runService, audit: auditRepo, reviewer: audit.ActorContext{ID: 2, Username: "reviewer", RequestID: "req-test"}, firstKey: interlock.EvidenceKey(first), secondKey: interlock.EvidenceKey(second)}
}

func appErrorCode(t *testing.T, err error) (*util.AppError, bool) {
	t.Helper()
	var appErr *util.AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

func TestBlockedRunSubmitsButApproveWaitsForDispositions(t *testing.T) {
	fixture := setupBlockerFlow(t)

	submitted, err := fixture.runs.Submit(1, dto.RunTransitionRequest{Version: 1, Reason: "please review each blocker"}, fixture.reviewer)
	if err != nil {
		t.Fatalf("blocked run should be submittable: %v", err)
	}
	if submitted.RunStatus != constants.RunPendingReview || submitted.BlockerCount != 2 || submitted.BlockersPendingDisposition != 2 {
		t.Fatalf("unexpected submitted view: %+v", submitted)
	}

	_, err = fixture.runs.Review(1, dto.ReviewRunRequest{Version: submitted.Version, Decision: "approve", Reason: "all evidence checked"}, fixture.reviewer)
	appErr, ok := appErrorCode(t, err)
	if !ok || appErr.Code != "BLOCKER_DISPOSITIONS_INCOMPLETE" {
		t.Fatalf("expected incomplete-dispositions gate, got %v", err)
	}
	details := appErr.Details.(map[string]any)
	if len(details["pending_disposition"].([]map[string]any)) != 2 {
		t.Fatalf("expected both blockers listed as pending, got %v", details)
	}
}

func TestBlockerDispositionLifecycleGate(t *testing.T) {
	fixture := setupBlockerFlow(t)
	if _, err := fixture.runs.Submit(1, dto.RunTransitionRequest{Version: 1, Reason: "submit blocker run"}, fixture.reviewer); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// mark the first blocker for rectification
	updated, err := fixture.runs.RegisterDisposition(1, dto.RegisterBlockerDispositionRequest{Version: 2, EvidenceKey: fixture.firstKey, Decision: "needs_rectification", Reason: "load must be reduced in the next cue version"}, fixture.reviewer)
	if err != nil {
		t.Fatalf("register disposition: %v", err)
	}
	if updated.Version != 3 || updated.BlockersNeedsRectification != 1 || updated.BlockersPendingDisposition != 1 {
		t.Fatalf("unexpected run counters after disposition: %+v", updated)
	}
	if updated.Blockers[0].Disposition == nil || updated.Blockers[0].Disposition.ReviewerName != "reviewer" {
		t.Fatal("disposition must carry reviewer and time back to the detail view")
	}

	// stale optimistic-lock version must be rejected
	if _, err := fixture.runs.RegisterDisposition(1, dto.RegisterBlockerDispositionRequest{Version: 2, EvidenceKey: fixture.secondKey, Decision: "accepted", Reason: "travel breach accepted for offline planning"}, fixture.reviewer); err == nil {
		t.Fatal("stale run version must conflict")
	}

	// accept the second blocker; approval still blocked by rectification
	if _, err := fixture.runs.RegisterDisposition(1, dto.RegisterBlockerDispositionRequest{Version: 3, EvidenceKey: fixture.secondKey, Decision: "accepted", Reason: "travel breach accepted for offline planning"}, fixture.reviewer); err != nil {
		t.Fatalf("accept second blocker: %v", err)
	}
	_, err = fixture.runs.Review(1, dto.ReviewRunRequest{Version: 4, Decision: "approve", Reason: "review complete"}, fixture.reviewer)
	appErr, ok := appErrorCode(t, err)
	if !ok || appErr.Code != "BLOCKER_DISPOSITIONS_INCOMPLETE" {
		t.Fatalf("rectified blocker must still withhold approval, got %v", err)
	}
	if len(appErr.Details.(map[string]any)["needs_rectification"].([]map[string]any)) != 1 {
		t.Fatalf("expected the rectified blocker to be named, got %v", appErr.Details)
	}

	// reviewer changes the first decision to accepted with a reason
	accepted, err := fixture.runs.RegisterDisposition(1, dto.RegisterBlockerDispositionRequest{Version: 4, EvidenceKey: fixture.firstKey, Decision: "accepted", Reason: "operator control and slack inspection confirm load is tolerable offline"}, fixture.reviewer)
	if err != nil {
		t.Fatalf("revise disposition: %v", err)
	}
	if accepted.BlockersAccepted != 2 {
		t.Fatalf("expected both blockers accepted, got %+v", accepted)
	}

	approved, err := fixture.runs.Review(1, dto.ReviewRunRequest{Version: 5, Decision: "approve", Reason: "all blockers individually accepted"}, fixture.reviewer)
	if err != nil {
		t.Fatalf("approval should pass once every blocker is accepted: %v", err)
	}
	if approved.RunStatus != constants.RunApproved {
		t.Fatalf("expected approved run, got %s", approved.RunStatus)
	}
}

func TestDispositionRequiresPendingRunAndKnownBlocker(t *testing.T) {
	fixture := setupBlockerFlow(t)

	if _, err := fixture.runs.RegisterDisposition(1, dto.RegisterBlockerDispositionRequest{Version: 1, EvidenceKey: fixture.firstKey, Decision: "accepted", Reason: "cannot register before submission"}, fixture.reviewer); err == nil {
		t.Fatal("blocked (not submitted) run must reject dispositions")
	}
	if _, err := fixture.runs.Submit(1, dto.RunTransitionRequest{Version: 1, Reason: "submit blocker run"}, fixture.reviewer); err != nil {
		t.Fatalf("submit: %v", err)
	}
	_, err := fixture.runs.RegisterDisposition(1, dto.RegisterBlockerDispositionRequest{Version: 2, EvidenceKey: "ev-doesnotexist00000000000000000", Decision: "accepted", Reason: "unknown evidence"}, fixture.reviewer)
	appErr, ok := appErrorCode(t, err)
	if !ok || appErr.Code != "BLOCKER_EVIDENCE_NOT_FOUND" {
		t.Fatalf("expected unknown blocker rejection, got %v", err)
	}
}

func TestDispositionsWriteAuditEvents(t *testing.T) {
	fixture := setupBlockerFlow(t)
	if _, err := fixture.runs.Submit(1, dto.RunTransitionRequest{Version: 1, Reason: "submit blocker run"}, fixture.reviewer); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := fixture.runs.RegisterDisposition(1, dto.RegisterBlockerDispositionRequest{Version: 2, EvidenceKey: fixture.firstKey, Decision: "accepted", Reason: "accepted for documented offline reason"}, fixture.reviewer); err != nil {
		t.Fatalf("register: %v", err)
	}
	events, _, err := fixture.audit.List(1, 50, "rehearsal_run", "", "")
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, event := range events {
		if event.Action == "rehearsal_run.blocker_disposition" && event.RequestID == "req-test" && event.ActorUsername == "reviewer" {
			found = true
		}
	}
	if !found {
		t.Fatalf("blocker disposition must be recorded as an audit event, got %+v", events)
	}
}

package service

import (
	"testing"

	"stage-rigging-cue-interlock/backend/internal/constants"
	"stage-rigging-cue-interlock/backend/internal/dto"
	"stage-rigging-cue-interlock/backend/internal/interlock"
)

func evidenceFixture() []interlock.RuleEvidence {
	return []interlock.RuleEvidence{
		{EvidenceKey: "evidence-0", RuleCode: "LOAD-1", Result: constants.ResultBlocker},
		{EvidenceKey: "evidence-1", RuleCode: "SPEED-1", Result: constants.ResultWarning},
		{EvidenceKey: "evidence-2", RuleCode: "TRAVEL-1", Result: constants.ResultInvalid},
		{EvidenceKey: "evidence-3", RuleCode: "ZONE-1", Result: constants.ResultPass},
	}
}

func TestComputeDispositionGapsAllBlockersAccepted(t *testing.T) {
	dispositions := []dto.BlockerDisposition{
		{EvidenceKey: "evidence-0", RuleCode: "LOAD-1", Decision: string(constants.DispositionAccepted)},
		{EvidenceKey: "evidence-2", RuleCode: "TRAVEL-1", Decision: string(constants.DispositionAccepted)},
	}
	gaps := computeDispositionGaps(evidenceFixture(), dispositions)
	if len(gaps.Unregistered) != 0 || len(gaps.NeedsRectification) != 0 {
		t.Fatalf("fully accepted blockers must leave no gaps, got %#v", gaps)
	}
}

func TestComputeDispositionGapsUnregisteredAndRectify(t *testing.T) {
	dispositions := []dto.BlockerDisposition{
		{EvidenceKey: "evidence-0", RuleCode: "LOAD-1", Decision: string(constants.DispositionAccepted)},
		{EvidenceKey: "evidence-2", RuleCode: "TRAVEL-1", Decision: string(constants.DispositionNeedsRectification)},
	}
	gaps := computeDispositionGaps(evidenceFixture(), dispositions)
	if len(gaps.NeedsRectification) != 1 || gaps.NeedsRectification[0].EvidenceKey != "evidence-2" {
		t.Fatalf("evidence-2 must be reported for rectification, got %#v", gaps)
	}
	if len(gaps.Unregistered) != 0 {
		t.Fatalf("every blocker was registered, unregistered must be empty, got %#v", gaps)
	}
}

func TestComputeDispositionGapsWithoutAnyDispositions(t *testing.T) {
	gaps := computeDispositionGaps(evidenceFixture(), nil)
	if len(gaps.Unregistered) != 2 {
		t.Fatalf("both blocker and invalid items must be unregistered, got %#v", gaps.Unregistered)
	}
	keys := map[string]bool{gaps.Unregistered[0].EvidenceKey: true, gaps.Unregistered[1].EvidenceKey: true}
	if !keys["evidence-0"] || !keys["evidence-2"] {
		t.Fatalf("unregistered list must name the exact missing items, got %#v", gaps.Unregistered)
	}
}

func TestComputeDispositionGapsPassOnlyRun(t *testing.T) {
	results := []interlock.RuleEvidence{{EvidenceKey: "evidence-0", RuleCode: "RULESET", Result: constants.ResultPass}}
	gaps := computeDispositionGaps(results, nil)
	if len(gaps.Unregistered) != 0 || len(gaps.NeedsRectification) != 0 {
		t.Fatalf("pass-only runs must be approvable without dispositions, got %#v", gaps)
	}
}

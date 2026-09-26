package interlock

import (
	"testing"

	"stage-rigging-cue-interlock/backend/internal/constants"
)

func TestEvidenceKeyStableAndDistinct(t *testing.T) {
	first := RuleEvidence{RuleCode: "LOAD-ALL-01", RuleType: "load_limit", Result: constants.ResultBlocker, CueCodes: []string{"Q-010"}, DeviceCodes: []string{"TRUSS-FOH-01"}, WindowStartMS: 0, WindowEndMS: 10000, ActualValue: 900, ThresholdValue: 800, Unit: "kg", Message: "overload"}
	second := RuleEvidence{RuleCode: "LOAD-ALL-01", RuleType: "load_limit", Result: constants.ResultBlocker, CueCodes: []string{"Q-010"}, DeviceCodes: []string{"TRUSS-FOH-01"}, WindowStartMS: 0, WindowEndMS: 10000, ActualValue: 900, ThresholdValue: 800, Unit: "kg", Message: "overload"}
	other := RuleEvidence{RuleCode: "LOAD-ALL-01", Result: constants.ResultBlocker, WindowStartMS: 100, WindowEndMS: 200, ActualValue: 900, ThresholdValue: 800, Unit: "kg"}

	if EvidenceKey(first) != EvidenceKey(second) {
		t.Fatal("identical evidence must produce the same stable key")
	}
	if EvidenceKey(first) == EvidenceKey(other) {
		t.Fatal("different blocker windows must produce different keys")
	}
	if len(EvidenceKey(first)) < 12 {
		t.Fatal("evidence key should be a non-trivial digest reference")
	}
}

func TestBlockingEvidenceFiltersPassAndWarning(t *testing.T) {
	evidence := []RuleEvidence{
		{RuleCode: "PASS", Result: constants.ResultPass},
		{RuleCode: "WARN", Result: constants.ResultWarning},
		{RuleCode: "BLOCK", Result: constants.ResultBlocker},
		{RuleCode: "INVALID", Result: constants.ResultInvalid},
	}
	blocking := BlockingEvidence(evidence)
	if len(blocking) != 2 {
		t.Fatalf("expected blocker and invalid to require disposition, got %d", len(blocking))
	}
}

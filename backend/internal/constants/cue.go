package constants

type CueStatus string

const (
	CueDraft         CueStatus = "draft"
	CuePendingReview CueStatus = "pending_review"
	CueApproved      CueStatus = "approved"
	CueLocked        CueStatus = "locked"
	CueArchived      CueStatus = "archived"
)

var cueTransitions = map[CueStatus]map[CueStatus]bool{
	CueDraft:         {CuePendingReview: true},
	CuePendingReview: {CueApproved: true, CueDraft: true},
	CueApproved:      {CueLocked: true, CueDraft: true},
	CueLocked:        {CueArchived: true},
	CueArchived:      {},
}

func (s CueStatus) Valid() bool {
	_, ok := cueTransitions[s]
	return ok
}

func CanTransitionCue(from, to CueStatus) bool {
	return cueTransitions[from][to]
}

type RehearsalStatus string

const (
	RunEvaluated     RehearsalStatus = "evaluated"
	RunBlocked       RehearsalStatus = "blocked"
	RunPendingReview RehearsalStatus = "pending_review"
	RunApproved      RehearsalStatus = "approved_for_rehearsal"
	RunRejected      RehearsalStatus = "rejected"
)

// BlockerDisposition is the reviewer's per-blocker decision registered on a
// submitted rehearsal run. Runs with blocker evidence can only be approved
// once every blocker has been registered as accepted with a written reason.
type BlockerDispositionType string

const (
	DispositionAccepted           BlockerDispositionType = "accepted"
	DispositionNeedsRectification BlockerDispositionType = "needs_rectification"
)

func (d BlockerDispositionType) Valid() bool {
	return d == DispositionAccepted || d == DispositionNeedsRectification
}

func CanTransitionRun(from, to RehearsalStatus) bool {
	switch from {
	case RunEvaluated, RunBlocked:
		// blocker runs can be submitted: the reviewer must register a
		// per-blocker disposition before approval can pass.
		return to == RunPendingReview
	case RunPendingReview:
		return to == RunApproved || to == RunRejected
	default:
		return false
	}
}

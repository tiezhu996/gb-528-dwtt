package constants

type InterlockResult string

const (
	ResultPass    InterlockResult = "pass"
	ResultWarning InterlockResult = "warning"
	ResultBlocker InterlockResult = "blocker"
	ResultInvalid InterlockResult = "invalid"
)

func (r InterlockResult) Valid() bool {
	return r == ResultPass || r == ResultWarning || r == ResultBlocker || r == ResultInvalid
}

func SeverityRank(result InterlockResult) int {
	switch result {
	case ResultInvalid:
		return 4
	case ResultBlocker:
		return 3
	case ResultWarning:
		return 2
	case ResultPass:
		return 1
	default:
		return 0
	}
}

func HighestSeverity(values ...InterlockResult) InterlockResult {
	highest := ResultPass
	for _, value := range values {
		if SeverityRank(value) > SeverityRank(highest) {
			highest = value
		}
	}
	return highest
}

type DispositionDecision string

const (
	DispositionAccepted           DispositionDecision = "accepted"
	DispositionNeedsRectification DispositionDecision = "needs_rectification"
)

func (d DispositionDecision) Valid() bool {
	return d == DispositionAccepted || d == DispositionNeedsRectification
}

// DispositionRequired reports whether an interlock result must receive a
// reviewer disposition before a run can be approved for rehearsal.
func DispositionRequired(result InterlockResult) bool {
	return result == ResultBlocker || result == ResultInvalid
}

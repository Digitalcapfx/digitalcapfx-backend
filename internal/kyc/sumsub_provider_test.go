package kyc

import "testing"

// TestMapSumsubEvent covers the real Sumsub event types (from the production
// webhook payload catalogue), especially the workflow decision events that must
// approve/reject rather than be ignored.
func TestMapSumsubEvent(t *testing.T) {
	cases := []struct{ event, answer, want string }{
		{"applicantReviewed", "GREEN", "approved"},
		{"applicantReviewed", "RED", "rejected"},
		{"applicantWorkflowCompleted", "GREEN", "approved"},
		{"applicantWorkflowCompleted", "RED", "rejected"},
		{"applicantWorkflowFailed", "RED", "rejected"},
		{"applicantPending", "", "under_review"},
		{"applicantCreated", "", "under_review"},
		{"applicantOnHold", "GREEN", "under_review"},
		{"applicantAwaitingUser", "GREEN", "under_review"},
		{"applicantAwaitingService", "GREEN", "under_review"},
		{"applicantActionPending", "", "processing"},
		{"applicantDeleted", "", "pending"},
		{"applicantActivated", "", "pending"},
		{"applicantReset", "GREEN", "pending"},
		{"kytCaseV2Created", "", "pending"},
	}
	for _, c := range cases {
		if got := mapSumsubEvent(c.event, c.answer); got != c.want {
			t.Errorf("mapSumsubEvent(%q, %q) = %q, want %q", c.event, c.answer, got, c.want)
		}
	}
}

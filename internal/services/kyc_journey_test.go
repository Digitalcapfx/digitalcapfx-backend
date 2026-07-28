package services

import "testing"

func TestComputeKYCStage(t *testing.T) {
	cases := []struct {
		name                            string
		kyc, intake, identity, expected string
	}{
		{"nothing", "pending", "not_started", "not_started", "not_started"},
		{"draft", "pending", "draft", "not_started", "draft"},
		{"intake submitted, id not started", "pending", "submitted", "not_started", "submitted"},
		{"identity started", "pending", "submitted", "in_progress", "identity_started"},
		{"in review", "pending", "submitted", "in_review", "in_review"},
		{"identity approved", "pending", "submitted", "approved", "approved"},
		{"identity rejected", "pending", "submitted", "rejected", "rejected"},
		{"identity resubmit", "pending", "submitted", "resubmit", "resubmit"},
		{"final approved (admin/webhook)", "approved", "submitted", "not_started", "approved"},
		{"final rejected", "rejected", "submitted", "rejected", "rejected"},
		{"final rejected but retry", "rejected", "submitted", "resubmit", "resubmit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := computeKYCStage(c.kyc, c.intake, c.identity); got != c.expected {
				t.Errorf("computeKYCStage(%q,%q,%q) = %q, want %q", c.kyc, c.intake, c.identity, got, c.expected)
			}
		})
	}
}

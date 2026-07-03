package ghclient

import (
	"testing"

	"github.com/verygoodsoftwarenotvirus/prez/internal/triage"
)

func TestCheckStatus(t *testing.T) {
	t.Parallel()

	cases := map[string]triage.CheckStatus{
		"SUCCESS":  triage.CheckPassing,
		"FAILURE":  triage.CheckFailing,
		"ERROR":    triage.CheckFailing,
		"PENDING":  triage.CheckPending,
		"EXPECTED": triage.CheckPending,
		"":         triage.CheckNone,
		"WHATEVER": triage.CheckNone,
	}

	for rollup, want := range cases {
		t.Run(rollup, func(t *testing.T) {
			t.Parallel()
			if got := checkStatus(rollup); got != want {
				t.Errorf("checkStatus(%q) = %d, want %d", rollup, got, want)
			}
		})
	}
}

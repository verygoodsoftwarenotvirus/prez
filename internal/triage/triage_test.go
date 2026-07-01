package triage

import (
	"testing"
	"time"
)

func TestEvaluate(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name string
		pr   PullRequest
		want Status
	}{
		{
			name: "never reviewed",
			pr: PullRequest{
				HeadOID: "sha-2",
				Reviews: nil,
			},
			want: StatusNeedsReview,
		},
		{
			name: "reviewed, no new commits since",
			pr: PullRequest{
				HeadOID: "sha-1",
				Reviews: []Review{
					{Author: "me", State: "COMMENTED", CommitOID: "sha-1", SubmittedAt: now},
				},
			},
			want: StatusWaitingOnAuthor,
		},
		{
			name: "reviewed, then author pushed more commits",
			pr: PullRequest{
				HeadOID: "sha-2",
				Reviews: []Review{
					{Author: "me", State: "CHANGES_REQUESTED", CommitOID: "sha-1", SubmittedAt: now},
				},
			},
			want: StatusNeedsReReview,
		},
		{
			name: "reviewed and unchanged, but explicitly re-requested",
			pr: PullRequest{
				HeadOID:            "sha-1",
				RequestedReviewers: []string{"me"},
				Reviews: []Review{
					{Author: "me", State: "APPROVED", CommitOID: "sha-1", SubmittedAt: now},
				},
			},
			want: StatusReReviewRequested,
		},
		{
			name: "uses the latest of multiple reviews by the same person",
			pr: PullRequest{
				HeadOID: "sha-2",
				Reviews: []Review{
					{Author: "me", State: "CHANGES_REQUESTED", CommitOID: "sha-1", SubmittedAt: now.Add(-time.Hour)},
					{Author: "me", State: "APPROVED", CommitOID: "sha-2", SubmittedAt: now},
				},
			},
			want: StatusWaitingOnAuthor,
		},
		{
			name: "ignores other reviewers' reviews",
			pr: PullRequest{
				HeadOID: "sha-2",
				Reviews: []Review{
					{Author: "someone-else", State: "APPROVED", CommitOID: "sha-2", SubmittedAt: now},
				},
			},
			want: StatusNeedsReview,
		},
		{
			name: "login comparison is case-insensitive",
			pr: PullRequest{
				HeadOID: "sha-1",
				Reviews: []Review{
					{Author: "Me", State: "APPROVED", CommitOID: "sha-1", SubmittedAt: now},
				},
			},
			want: StatusWaitingOnAuthor,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultEvaluator{}.Evaluate("me", tc.pr)
			if got.Status != tc.want {
				t.Errorf("Evaluate() = %v (%s), want %v", got.Status, got.Reason, tc.want)
			}
		})
	}
}

func TestFilterAllows(t *testing.T) {
	f := Filter{ExcludeAuthors: []string{"app/dependabot"}, IncludeDrafts: false}

	if f.Allows(PullRequest{IsDraft: true, Author: "someone"}) {
		t.Error("drafts should be excluded by default")
	}
	if f.Allows(PullRequest{Author: "app/dependabot"}) {
		t.Error("excluded authors should be excluded")
	}
	if !f.Allows(PullRequest{Author: "someone"}) {
		t.Error("a normal open PR should be allowed")
	}
}

func TestFilterIncludeAuthors(t *testing.T) {
	f := Filter{IncludeAuthors: []string{"alice", "bob"}}

	if !f.Allows(PullRequest{Author: "alice"}) {
		t.Error("an allowlisted author should be allowed")
	}
	if !f.Allows(PullRequest{Author: "Bob"}) {
		t.Error("allowlist matching should be case-insensitive")
	}
	if f.Allows(PullRequest{Author: "carol"}) {
		t.Error("an author not on the allowlist should be dropped")
	}

	// An empty allowlist disables the restriction entirely.
	if !(Filter{}).Allows(PullRequest{Author: "carol"}) {
		t.Error("an empty IncludeAuthors should allow any author")
	}

	// Exclude wins over Include.
	fx := Filter{IncludeAuthors: []string{"alice"}, ExcludeAuthors: []string{"alice"}}
	if fx.Allows(PullRequest{Author: "alice"}) {
		t.Error("ExcludeAuthors should override IncludeAuthors")
	}
}

func TestFilterHideFailing(t *testing.T) {
	f := Filter{HideFailing: true}

	if f.Allows(PullRequest{Author: "a", CheckStatus: "FAILURE"}) {
		t.Error("FAILURE checks should be hidden when HideFailing is set")
	}
	if f.Allows(PullRequest{Author: "a", CheckStatus: "ERROR"}) {
		t.Error("ERROR checks should be hidden when HideFailing is set")
	}
	if !f.Allows(PullRequest{Author: "a", CheckStatus: "PENDING"}) {
		t.Error("PENDING checks should still be shown")
	}
	if !f.Allows(PullRequest{Author: "a", CheckStatus: "SUCCESS"}) {
		t.Error("SUCCESS checks should be shown")
	}
	if !f.Allows(PullRequest{Author: "a", CheckStatus: ""}) {
		t.Error("PRs with no checks should be shown")
	}

	// Disabled by default: a failing PR passes when HideFailing is false.
	if !(Filter{}).Allows(PullRequest{Author: "a", CheckStatus: "FAILURE"}) {
		t.Error("failing checks should pass when HideFailing is off")
	}
}

func TestEvaluateAllSortsByUrgencyThenRecency(t *testing.T) {
	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now()

	prs := []PullRequest{
		{Number: 1, HeadOID: "a", UpdatedAt: older, Reviews: []Review{{Author: "me", CommitOID: "a", SubmittedAt: older}}}, // waiting
		{Number: 2, HeadOID: "b", UpdatedAt: newer}, // needs review
		{Number: 3, HeadOID: "d", UpdatedAt: older, Reviews: []Review{{Author: "me", CommitOID: "c", SubmittedAt: older}}}, // needs re-review
	}

	got := EvaluateAll("me", prs, Filter{}, DefaultEvaluator{})

	if len(got) != 3 {
		t.Fatalf("expected 3 evaluations, got %d", len(got))
	}
	if got[0].PR.Number != 2 || got[0].Status != StatusNeedsReview {
		t.Errorf("expected PR #2 (needs review) first, got #%d (%v)", got[0].PR.Number, got[0].Status)
	}
	if got[1].PR.Number != 3 || got[1].Status != StatusNeedsReReview {
		t.Errorf("expected PR #3 (needs re-review) second, got #%d (%v)", got[1].PR.Number, got[1].Status)
	}
	if got[2].PR.Number != 1 || got[2].Status != StatusWaitingOnAuthor {
		t.Errorf("expected PR #1 (waiting) last, got #%d (%v)", got[2].PR.Number, got[2].Status)
	}
}

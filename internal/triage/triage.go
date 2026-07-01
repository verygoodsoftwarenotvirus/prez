// Package triage contains the domain model for a pull request and the pure
// logic that decides whether it currently needs the viewer's attention.
//
// Nothing in this package talks to GitHub or a terminal. It exists so the
// "what counts as actionable" decision is a plain, testable function rather
// than something buried in API-response handling or rendering code.
package triage

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Review is a single review event on a PR, trimmed to the fields that matter
// for staleness detection.
type Review struct {
	SubmittedAt time.Time
	Author      string
	State       string
	CommitOID   string
}

// PullRequest is a provider-agnostic snapshot of an open PR at fetch time.
type PullRequest struct {
	UpdatedAt time.Time
	Repo      string
	Title     string
	URL       string
	Author    string
	HeadOID   string
	// CheckStatus is GitHub's overall status-check rollup state for the head
	// commit (SUCCESS, FAILURE, ERROR, PENDING, EXPECTED), or "" when the PR
	// has no checks at all.
	CheckStatus        string
	Reviews            []Review
	RequestedReviewers []string
	Number             int
	IsDraft            bool
}

// ChecksFailing reports whether the PR's overall check rollup is in a failed
// state, as opposed to passing, pending, or having no checks configured.
func (pr PullRequest) ChecksFailing() bool {
	switch pr.CheckStatus {
	case "FAILURE", "ERROR":
		return true
	default:
		return false
	}
}

// Status is the triage outcome for a PR from a specific viewer's perspective.
type Status int

const (
	// StatusNeedsReview: the viewer has never reviewed this PR.
	StatusNeedsReview Status = iota
	// StatusReReviewRequested: the viewer was explicitly re-requested,
	// regardless of whether the head commit moved.
	StatusReReviewRequested
	// StatusNeedsReReview: the viewer reviewed before, but new commits have
	// landed since (head SHA no longer matches the reviewed SHA).
	StatusNeedsReReview
	// StatusWaitingOnAuthor: the viewer's review still applies to the
	// current head commit. Nothing new for them to look at.
	StatusWaitingOnAuthor
)

func (s Status) String() string {
	switch s {
	case StatusNeedsReview:
		return "needs review"
	case StatusReReviewRequested:
		return "re-review requested"
	case StatusNeedsReReview:
		return "needs re-review"
	case StatusWaitingOnAuthor:
		return "waiting on author"
	default:
		return "unknown"
	}
}

// Actionable reports whether this status represents something the viewer
// should actually look at right now.
func (s Status) Actionable() bool {
	return s != StatusWaitingOnAuthor
}

// Evaluation is the result of running an Evaluator over a single PR.
type Evaluation struct {
	Reason string
	PR     PullRequest
	Status Status
}

// Evaluator decides the Status of a PR from a given viewer's perspective.
// It's an interface so the policy (e.g. what counts as "stale") can be
// swapped or unit tested independently of the GitHub client and the TUI.
type Evaluator interface {
	Evaluate(viewer string, pr PullRequest) Evaluation
}

// DefaultEvaluator implements the SHA-comparison policy described above.
type DefaultEvaluator struct{}

func (DefaultEvaluator) Evaluate(viewer string, pr PullRequest) Evaluation {
	last := latestReviewBy(pr.Reviews, viewer)

	if last == nil {
		return Evaluation{PR: pr, Status: StatusNeedsReview, Reason: "you haven't reviewed this yet"}
	}

	if contains(pr.RequestedReviewers, viewer) {
		return Evaluation{PR: pr, Status: StatusReReviewRequested, Reason: "you were explicitly re-requested"}
	}

	if last.CommitOID != pr.HeadOID {
		return Evaluation{
			PR:     pr,
			Status: StatusNeedsReReview,
			Reason: fmt.Sprintf("new commits since your %s review", strings.ToLower(last.State)),
		}
	}

	return Evaluation{
		PR:     pr,
		Status: StatusWaitingOnAuthor,
		Reason: fmt.Sprintf("already %s, no new commits", strings.ToLower(last.State)),
	}
}

func latestReviewBy(reviews []Review, login string) *Review {
	var latest *Review
	for i := range reviews {
		r := &reviews[i]
		if !strings.EqualFold(r.Author, login) {
			continue
		}
		if latest == nil || r.SubmittedAt.After(latest.SubmittedAt) {
			latest = r
		}
	}
	return latest
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.EqualFold(s, needle) {
			return true
		}
	}
	return false
}

// Filter describes which PRs to drop before evaluation, independent of
// per-repo GraphQL query shape.
type Filter struct {
	// IncludeAuthors is an allowlist of logins. When empty, every author is
	// allowed; when non-empty, only PRs by these authors pass.
	IncludeAuthors []string
	ExcludeAuthors []string
	IncludeDrafts  bool
	// HideFailing drops PRs whose check rollup is in a failed state.
	HideFailing bool
}

func (f Filter) Allows(pr PullRequest) bool {
	if pr.IsDraft && !f.IncludeDrafts {
		return false
	}
	if f.HideFailing && pr.ChecksFailing() {
		return false
	}
	if len(f.IncludeAuthors) > 0 && !contains(f.IncludeAuthors, pr.Author) {
		return false
	}
	for _, a := range f.ExcludeAuthors {
		if strings.EqualFold(a, pr.Author) {
			return false
		}
	}
	return true
}

// EvaluateAll applies a Filter then an Evaluator to a batch of PRs, and
// returns evaluations sorted by urgency (most actionable first), with ties
// broken by most-recently-updated first.
func EvaluateAll(viewer string, prs []PullRequest, f Filter, ev Evaluator) []Evaluation {
	out := make([]Evaluation, 0, len(prs))
	for i := range prs {
		if !f.Allows(prs[i]) {
			continue
		}
		out = append(out, ev.Evaluate(viewer, prs[i]))
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return out[i].Status < out[j].Status
		}
		return out[i].PR.UpdatedAt.After(out[j].PR.UpdatedAt)
	})

	return out
}

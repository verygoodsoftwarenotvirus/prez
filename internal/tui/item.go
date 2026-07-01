package tui

import (
	"fmt"
	"time"

	"github.com/verygoodsoftwarenotvirus/prez/internal/triage"
)

type item struct {
	eval triage.Evaluation
}

func (i item) Title() string {
	badge := badgeStyle(i.eval.Status).Render(statusLabel(i.eval.Status))
	return fmt.Sprintf("%s  %s#%d  %s", badge, i.eval.PR.Repo, i.eval.PR.Number, i.eval.PR.Title)
}

func (i item) Description() string {
	desc := fmt.Sprintf("by %s · %s · updated %s", i.eval.PR.Author, i.eval.Reason, relTime(i.eval.PR.UpdatedAt))
	if c := checkLabel(i.eval.PR.CheckStatus); c != "" {
		desc += " · " + c
	}
	return desc
}

// checkLabel renders a compact indicator for a PR's overall check rollup, or
// "" when the PR has no checks.
func checkLabel(state string) string {
	switch state {
	case "SUCCESS":
		return "checks ✓"
	case "FAILURE", "ERROR":
		return "checks ✗"
	case "PENDING", "EXPECTED":
		return "checks …"
	default:
		return ""
	}
}

func (i item) FilterValue() string {
	return i.eval.PR.Title + " " + i.eval.PR.Repo
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

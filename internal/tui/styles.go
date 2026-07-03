package tui

import (
	"github.com/verygoodsoftwarenotvirus/prez/internal/triage"

	"github.com/charmbracelet/lipgloss"
)

var (
	docStyle   = lipgloss.NewStyle().Margin(1, 2)
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

	tabActive   = lipgloss.NewStyle().Padding(0, 2).Bold(true).Reverse(true)
	tabInactive = lipgloss.NewStyle().Padding(0, 2).Foreground(lipgloss.Color("241"))

	badgeBase = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Foreground(lipgloss.Color("0"))

	badgeNeedsReview       = badgeBase.Background(lipgloss.Color("9"))                                  // red — never reviewed
	badgeReReviewRequested = badgeBase.Background(lipgloss.Color("13"))                                 // magenta — explicitly re-requested
	badgeNeedsReReview     = badgeBase.Background(lipgloss.Color("11"))                                 // yellow — new commits since your review
	badgeWaitingOnAuthor   = badgeBase.Background(lipgloss.Color("8")).Foreground(lipgloss.Color("15")) // grey — nothing new
)

func badgeStyle(s triage.Status) lipgloss.Style {
	switch s {
	case triage.StatusNeedsReview:
		return badgeNeedsReview
	case triage.StatusReReviewRequested:
		return badgeReReviewRequested
	case triage.StatusNeedsReReview:
		return badgeNeedsReReview
	default:
		return badgeWaitingOnAuthor
	}
}

func statusLabel(s triage.Status) string {
	switch s {
	case triage.StatusNeedsReview:
		return "NEW"
	case triage.StatusReReviewRequested:
		return "RE-REQUESTED"
	case triage.StatusNeedsReReview:
		return "STALE"
	default:
		return "WAITING"
	}
}

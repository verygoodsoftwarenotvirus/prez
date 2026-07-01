// Package tui is the interactive terminal UI: a single list of evaluated
// PRs, sorted by urgency, that refreshes itself on an interval and lets the
// viewer jump to a PR in the browser.
package tui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/verygoodsoftwarenotvirus/prez/internal/config"
	"github.com/verygoodsoftwarenotvirus/prez/internal/triage"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sync/errgroup"
)

// Source is the subset of ghclient.Client the TUI depends on. Defined here,
// at the consumer, so the TUI can be driven by a fake in tests without
// pulling in any GitHub-specific types.
type Source interface {
	Viewer(ctx context.Context) (string, error)
	OpenPullRequests(ctx context.Context, owner, name string) ([]triage.PullRequest, error)
	TeamMembers(ctx context.Context, org, slug string) ([]string, error)
}

type prsFetchedMsg struct {
	err            error
	viewer         string
	includeAuthors []string
	evals          []triage.Evaluation
}

type tickMsg time.Time

type Model struct {
	list            list.Model
	lastRefresh     time.Time
	src             Source
	evaluator       triage.Evaluator
	err             error
	viewer          string
	includeAuthors  []string
	evals           []triage.Evaluation
	cfg             config.Config
	authorsResolved bool
	showWaiting     bool
	loading         bool
}

// New builds the initial Model. Call tea.NewProgram(m) to run it.
func New(cfg config.Config, src Source) Model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "PRs"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)

	return Model{
		list:      l,
		cfg:       cfg,
		src:       src,
		evaluator: triage.DefaultEvaluator{},
		loading:   true,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd(m.cfg.PollInterval))
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) refreshCmd() tea.Cmd {
	cachedViewer := m.viewer
	cachedAuthors := m.includeAuthors
	authorsResolved := m.authorsResolved
	src := m.src
	cfg := m.cfg
	evaluator := m.evaluator

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		viewer := cachedViewer
		if viewer == "" {
			v, err := src.Viewer(ctx)
			if err != nil {
				return prsFetchedMsg{err: fmt.Errorf("looking up your GitHub login: %w", err)}
			}
			viewer = v
		}

		// Team membership rarely changes, so resolve it once and cache the
		// combined allowlist; a restart picks up team roster changes.
		includeAuthors := cachedAuthors
		if !authorsResolved {
			includeAuthors = append(includeAuthors, cfg.Authors.Include...)
			for i := range cfg.Authors.Teams {
				t := &cfg.Authors.Teams[i]
				members, err := src.TeamMembers(ctx, t.Org, t.Slug)
				if err != nil {
					return prsFetchedMsg{err: fmt.Errorf("resolving team %s: %w", t, err)}
				}
				includeAuthors = append(includeAuthors, members...)
			}
		}

		filter := triage.Filter{
			IncludeAuthors: includeAuthors,
			ExcludeAuthors: cfg.Authors.Exclude,
			IncludeDrafts:  cfg.IncludeDrafts,
			HideFailing:    cfg.Checks.HideFailing,
		}

		var (
			mu  sync.Mutex
			all []triage.PullRequest
		)
		g, gctx := errgroup.WithContext(ctx)
		for i := range cfg.Repos {
			r := &cfg.Repos[i]
			g.Go(func() error {
				prs, err := src.OpenPullRequests(gctx, r.Owner, r.Name)
				if err != nil {
					return fmt.Errorf("fetching %s: %w", r, err)
				}
				mu.Lock()
				all = append(all, prs...)
				mu.Unlock()
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return prsFetchedMsg{err: err}
		}

		evals := triage.EvaluateAll(viewer, all, filter, evaluator)
		return prsFetchedMsg{evals: evals, viewer: viewer, includeAuthors: includeAuthors}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v-2)
		return m, nil

	case prsFetchedMsg:
		m.loading = false
		m.lastRefresh = time.Now()
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.viewer = msg.viewer
		m.includeAuthors = msg.includeAuthors
		m.authorsResolved = true
		m.evals = msg.evals
		m.applyVisibility()
		return m, nil

	case tickMsg:
		var cmd tea.Cmd
		if !m.loading {
			m.loading = true
			cmd = m.refreshCmd()
		}
		return m, tea.Batch(cmd, tickCmd(m.cfg.PollInterval))

	case tea.KeyMsg:
		// Don't intercept keys while the user is typing into the filter box.
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				return m, m.refreshCmd()
			}
			return m, nil
		case "w":
			m.showWaiting = !m.showWaiting
			m.applyVisibility()
			return m, nil
		case "enter":
			if it, ok := m.list.SelectedItem().(item); ok {
				if err := openBrowser(context.Background(), it.eval.PR.URL); err != nil {
					m.err = fmt.Errorf("opening browser: %w", err)
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *Model) applyVisibility() {
	// With review filtering off, every PR that passed the fetch-time filters
	// is shown regardless of review state.
	showAll := !m.cfg.ReviewFilter.Enabled

	items := make([]list.Item, 0, len(m.evals))
	actionable, waiting := 0, 0
	for i := range m.evals {
		e := &m.evals[i]
		if e.Status == triage.StatusWaitingOnAuthor {
			waiting++
			if !m.showWaiting && !showAll {
				continue
			}
		} else {
			actionable++
		}
		items = append(items, item{eval: *e})
	}
	m.list.SetItems(items)

	if showAll {
		m.list.Title = fmt.Sprintf("PRs for %s  (%d shown, review filter off)", m.viewer, len(items))
		return
	}
	m.list.Title = fmt.Sprintf("PRs for %s  (%d actionable, %d waiting%s)",
		m.viewer, actionable, waiting, map[bool]string{true: ", shown", false: ", hidden"}[m.showWaiting])
}

func (m Model) View() string {
	var status string
	switch {
	case m.loading:
		status = "refreshing…"
	case m.err != nil:
		status = errorStyle.Render("error: " + m.err.Error())
	default:
		status = fmt.Sprintf("last refreshed %s", relTime(m.lastRefresh))
	}

	help := helpStyle.Render("↑/↓ navigate · enter open in browser · r refresh · w toggle waiting-on-author · / filter · q quit")

	return docStyle.Render(m.list.View() + "\n" + status + "\n" + help)
}

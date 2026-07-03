// Package tui is the interactive terminal UI: one tab per config profile, each
// a self-refreshing list of evaluated PRs sorted by urgency, letting the viewer
// switch contexts and jump to a PR in the browser.
package tui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/verygoodsoftwarenotvirus/prez/internal/config"
	"github.com/verygoodsoftwarenotvirus/prez/internal/provider"
	"github.com/verygoodsoftwarenotvirus/prez/internal/triage"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/sync/errgroup"
)

// Source is the forge contract each tab fetches through. It aliases
// provider.Provider so the TUI can be driven by a hand-written fake in tests
// without pulling in any forge-specific types.
type Source = provider.Provider

type prsFetchedMsg struct {
	err            error
	viewer         string
	includeAuthors []string
	evals          []triage.Evaluation
	idx            int
}

type tickMsg struct {
	idx int
}

// tab is the per-profile state: its own list, config, and fetch bookkeeping.
// One prez run holds several, one per config profile, switched between as tabs.
type tab struct {
	name            string
	list            list.Model
	lastRefresh     time.Time
	src             Source
	evaluator       triage.Evaluator
	err             error
	viewer          string
	includeAuthors  []string
	evals           []triage.Evaluation
	cfg             config.Config
	actionable      int
	authorsResolved bool
	showWaiting     bool
	loading         bool
}

type Model struct {
	tabs          []tab
	active        int
	width, height int
}

func newList() list.Model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "PRs"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	return l
}

// New builds the initial Model, one tab per profile, each fetching through the
// provider at the same index (see provider.Resolve). Call tea.NewProgram(m) to
// run it.
func New(profiles []config.Profile, providers []provider.Provider) Model {
	tabs := make([]tab, 0, len(profiles))
	for i := range profiles {
		tabs = append(tabs, tab{
			name:      profiles[i].Name,
			list:      newList(),
			cfg:       profiles[i].Config,
			src:       providers[i],
			evaluator: triage.DefaultEvaluator{},
			loading:   true,
		})
	}
	return Model{tabs: tabs}
}

func (m Model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.tabs)*2)
	for i := range m.tabs {
		cmds = append(cmds, m.tabs[i].refreshCmd(i), tickCmd(i, m.tabs[i].cfg.PollInterval))
	}
	return tea.Batch(cmds...)
}

func tickCmd(idx int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{idx: idx} })
}

func (t tab) refreshCmd(idx int) tea.Cmd {
	cachedViewer := t.viewer
	cachedAuthors := t.includeAuthors
	authorsResolved := t.authorsResolved
	src := t.src
	cfg := t.cfg
	evaluator := t.evaluator

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		viewer := cachedViewer
		if viewer == "" {
			v, err := src.Viewer(ctx)
			if err != nil {
				return prsFetchedMsg{idx: idx, err: fmt.Errorf("looking up your GitHub login: %w", err)}
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
					return prsFetchedMsg{idx: idx, err: fmt.Errorf("resolving team %s: %w", t, err)}
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
			return prsFetchedMsg{idx: idx, err: err}
		}

		evals := triage.EvaluateAll(viewer, all, filter, evaluator)
		return prsFetchedMsg{idx: idx, evals: evals, viewer: viewer, includeAuthors: includeAuthors}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeLists()
		return m, nil

	case prsFetchedMsg:
		t := &m.tabs[msg.idx]
		t.loading = false
		t.lastRefresh = time.Now()
		if msg.err != nil {
			t.err = msg.err
			return m, nil
		}
		t.err = nil
		t.viewer = msg.viewer
		t.includeAuthors = msg.includeAuthors
		t.authorsResolved = true
		t.evals = msg.evals
		t.applyVisibility()
		return m, nil

	case tickMsg:
		t := &m.tabs[msg.idx]
		var cmd tea.Cmd
		if !t.loading {
			t.loading = true
			cmd = t.refreshCmd(msg.idx)
		}
		return m, tea.Batch(cmd, tickCmd(msg.idx, t.cfg.PollInterval))

	case tea.KeyMsg:
		// Don't intercept keys while the user is typing into the filter box.
		if m.tabs[m.active].list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			m.active = (m.active + 1) % len(m.tabs)
			return m, nil
		case "shift+tab":
			m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if i := int(msg.String()[0] - '1'); i < len(m.tabs) {
				m.active = i
			}
			return m, nil
		case "r":
			t := &m.tabs[m.active]
			if !t.loading {
				t.loading = true
				return m, t.refreshCmd(m.active)
			}
			return m, nil
		case "w":
			t := &m.tabs[m.active]
			t.showWaiting = !t.showWaiting
			t.applyVisibility()
			return m, nil
		case "enter":
			if it, ok := m.tabs[m.active].list.SelectedItem().(item); ok {
				if err := openBrowser(context.Background(), it.eval.PR.URL); err != nil {
					m.tabs[m.active].err = fmt.Errorf("opening browser: %w", err)
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.tabs[m.active].list, cmd = m.tabs[m.active].list.Update(msg)
	return m, cmd
}

// resizeLists sizes every tab's list to the current terminal, reserving rows
// for the status line, help line, and (when there's more than one) tab strip.
func (m *Model) resizeLists() {
	h, v := docStyle.GetFrameSize()
	reserved := v + 2
	if len(m.tabs) > 1 {
		reserved++
	}
	for i := range m.tabs {
		m.tabs[i].list.SetSize(m.width-h, m.height-reserved)
	}
}

func (t *tab) applyVisibility() {
	// With review filtering off, every PR that passed the fetch-time filters
	// is shown regardless of review state.
	showAll := !t.cfg.ReviewFilter.Enabled

	items := make([]list.Item, 0, len(t.evals))
	actionable, waiting := 0, 0
	for i := range t.evals {
		e := &t.evals[i]
		if e.Status == triage.StatusWaitingOnAuthor {
			waiting++
			if !t.showWaiting && !showAll {
				continue
			}
		} else {
			actionable++
		}
		items = append(items, item{eval: *e})
	}
	t.list.SetItems(items)
	t.actionable = actionable

	if showAll {
		t.list.Title = fmt.Sprintf("PRs for %s  (%d shown, review filter off)", t.viewer, len(items))
		return
	}
	t.list.Title = fmt.Sprintf("PRs for %s  (%d actionable, %d waiting%s)",
		t.viewer, actionable, waiting, map[bool]string{true: ", shown", false: ", hidden"}[t.showWaiting])
}

func (m Model) tabStrip() string {
	pills := make([]string, 0, len(m.tabs))
	for i := range m.tabs {
		label := m.tabs[i].name
		if !m.tabs[i].loading && m.tabs[i].actionable > 0 {
			label = fmt.Sprintf("%s (%d)", label, m.tabs[i].actionable)
		}
		if i == m.active {
			pills = append(pills, tabActive.Render(label))
		} else {
			pills = append(pills, tabInactive.Render(label))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, pills...)
}

func (m Model) View() string {
	t := &m.tabs[m.active]

	var status string
	switch {
	case t.loading:
		status = "refreshing…"
	case t.err != nil:
		status = errorStyle.Render("error: " + t.err.Error())
	default:
		status = fmt.Sprintf("last refreshed %s", relTime(t.lastRefresh))
	}

	help := "↑/↓ navigate · enter open in browser · r refresh · w toggle waiting-on-author · / filter · q quit"
	if len(m.tabs) > 1 {
		help = "tab/⇧tab or 1-9 switch profile · " + help
	}

	var b string
	if len(m.tabs) > 1 {
		b = m.tabStrip() + "\n"
	}
	b += t.list.View() + "\n" + status + "\n" + helpStyle.Render(help)

	return docStyle.Render(b)
}

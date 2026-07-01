// Package config loads prez's YAML configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
)

type Repo struct {
	Owner string `yaml:"owner"`
	Name  string `yaml:"name"`
}

func (r Repo) String() string {
	return r.Owner + "/" + r.Name
}

// Team identifies a GitHub team whose current members' PRs should be shown.
type Team struct {
	Org  string `yaml:"org"`
	Slug string `yaml:"slug"`
}

func (t Team) String() string {
	return t.Org + "/" + t.Slug
}

// AuthorsConfig restricts which PR authors appear in the list. When both
// Teams and Include are empty the author allowlist is disabled and every
// author is shown; Exclude always applies regardless.
type AuthorsConfig struct {
	// Teams whose members (resolved from the GitHub API at startup) are
	// shown. Requires the gh token to have read:org scope.
	Teams []Team `yaml:"teams"`
	// Include adds individual logins on top of any team members.
	Include []string `yaml:"include"`
	// Exclude drops these logins even if a team or Include would allow them.
	Exclude []string `yaml:"exclude"`
}

// ChecksConfig controls filtering by CI status.
type ChecksConfig struct {
	// HideFailing drops PRs whose overall check rollup is FAILURE or ERROR.
	// Successful, pending, and check-less PRs are always shown.
	HideFailing bool `yaml:"hide_failing"`
}

// ReviewFilterConfig toggles prez's defining behavior: filtering PRs by
// whether they need the viewer's review.
type ReviewFilterConfig struct {
	// Enabled, when false, shows every PR that passes the other filters
	// regardless of review state. Defaults to true.
	Enabled bool `yaml:"enabled"`
}

type Config struct {
	Repos         []Repo             `yaml:"repos"`
	Authors       AuthorsConfig      `yaml:"authors"`
	Checks        ChecksConfig       `yaml:"checks"`
	ReviewFilter  ReviewFilterConfig `yaml:"review_filter"`
	IncludeDrafts bool               `yaml:"include_drafts"`
	PollInterval  time.Duration      `yaml:"poll_interval"`
}

func defaults() Config {
	return Config{
		Authors:      AuthorsConfig{Exclude: []string{"app/dependabot"}},
		ReviewFilter: ReviewFilterConfig{Enabled: true},
		PollInterval: 5 * time.Minute,
	}
}

// Load reads and validates a config file at path.
func Load(path string) (Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading config %s: %w", path, err)
	}

	if err = yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if len(cfg.Repos) == 0 {
		return Config{}, fmt.Errorf("config %s: at least one entry under 'repos' is required", path)
	}
	for i := range cfg.Repos {
		if cfg.Repos[i].Owner == "" || cfg.Repos[i].Name == "" {
			return Config{}, fmt.Errorf("config %s: repos[%d] needs both 'owner' and 'name'", path, i)
		}
	}
	for i := range cfg.Authors.Teams {
		if cfg.Authors.Teams[i].Org == "" || cfg.Authors.Teams[i].Slug == "" {
			return Config{}, fmt.Errorf("config %s: authors.teams[%d] needs both 'org' and 'slug'", path, i)
		}
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaults().PollInterval
	}

	return cfg, nil
}

// DefaultPath returns ~/.config/prez/config.yaml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "prez", "config.yaml"), nil
}

// annotatedTemplate is a commented, ready-to-edit config with every option
// documented and set to its default. The single required field (repos) is
// present but empty so a fresh file fails Load with a clear message until the
// user fills it in.
const annotatedTemplate = `# prez configuration.
# Docs live alongside each field below. Delete what you don't need.

# repos: the PRs to triage are pulled from these repositories. Required;
# at least one entry with both owner and name.
repos:
  - owner: ""
    name: ""

# authors: restrict which PR authors appear. When both teams and include are
# empty the allowlist is off and every author is shown; exclude always applies.
authors:
  # teams whose current members are shown (resolved from the GitHub API at
  # startup). Requires the gh token to have the read:org scope.
  teams: []
  #  - org: ""
  #    slug: ""
  # include: individual logins shown on top of any team members.
  include: []
  # exclude: logins dropped even if a team or include would allow them.
  exclude:
    - app/dependabot

# checks: filter by CI status.
checks:
  # hide_failing drops PRs whose overall check rollup is FAILURE or ERROR.
  # Successful, pending, and check-less PRs are always shown.
  hide_failing: false

# review_filter toggles prez's defining behavior: filtering PRs by whether
# they need your review.
review_filter:
  # enabled: when false, shows every PR that passes the other filters
  # regardless of review state.
  enabled: true

# include_drafts: when true, draft PRs are shown too.
include_drafts: false

# poll_interval: how often to refresh, as a Go duration (e.g. 30s, 5m, 1h).
poll_interval: 5m
`

// Init writes the annotated config template to path, creating parent
// directories as needed. It refuses to overwrite an existing file.
func Init(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config already exists at %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking config path %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(annotatedTemplate), 0o644); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}

	return nil
}

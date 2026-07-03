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

// ProviderGitHub is the only forge prez currently talks to. The provider field
// exists so a config can later opt into gitlab, codeberg, and friends without a
// schema break; for now any other value is rejected at load time.
const ProviderGitHub = "github"

type Config struct {
	Provider      string             `yaml:"provider"`
	Repos         []Repo             `yaml:"repos"`
	Authors       AuthorsConfig      `yaml:"authors"`
	Checks        ChecksConfig       `yaml:"checks"`
	ReviewFilter  ReviewFilterConfig `yaml:"review_filter"`
	IncludeDrafts bool               `yaml:"include_drafts"`
	PollInterval  time.Duration      `yaml:"poll_interval"`
}

// defaultProfileName is the name given to the lone profile a legacy
// single-config file is migrated into.
const defaultProfileName = "default"

// Profile is a named Config. Profiles are shown as tabs in the TUI, letting
// one prez instance triage several disjoint contexts (work, personal, a side
// project) without juggling files or restarting.
type Profile struct {
	Name   string `yaml:"name"`
	Config `yaml:",inline"`
}

// file is the on-disk shape: an ordered list of named profiles.
type file struct {
	Profiles []Profile `yaml:"profiles"`
}

// profileEnvelope captures a profile's name and defers its Config body to a
// second decode pass, so each profile can be seeded with defaults() before its
// keys are applied — preserving the omitted-vs-explicit-false semantics for
// fields like review_filter.enabled.
type profileEnvelope struct {
	Rest map[string]any `yaml:",inline"`
	Name string         `yaml:"name"`
}

type fileEnvelope struct {
	Profiles []profileEnvelope `yaml:"profiles"`
}

func defaults() Config {
	return Config{
		Provider:     ProviderGitHub,
		Authors:      AuthorsConfig{Exclude: []string{"app/dependabot"}},
		ReviewFilter: ReviewFilterConfig{Enabled: true},
		PollInterval: 5 * time.Minute,
	}
}

// Load reads and validates the config file at path, returning its profiles in
// on-disk order. A legacy single-config file (no 'profiles' key) is parsed as
// one Config, wrapped in a lone profile named "default", and rewritten in the
// profiles shape so the next load takes the fast path.
func Load(path string) ([]Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	// A legacy single-config file has no 'profiles' key, so it decodes here as
	// zero profiles (goccy ignores unknown top-level keys); a file that isn't
	// valid as the profiles shape at all lands here too. Either way, fall back
	// to parsing it as one Config and migrate it in place.
	var env fileEnvelope
	if perr := yaml.Unmarshal(data, &env); perr != nil || len(env.Profiles) == 0 {
		cfg, cerr := decodeConfig(path, data)
		if cerr != nil {
			return nil, cerr
		}
		profiles := []Profile{{Name: defaultProfileName, Config: cfg}}
		if serr := Save(path, profiles); serr != nil {
			return nil, fmt.Errorf("migrating config %s to profiles: %w", path, serr)
		}
		return profiles, nil
	}

	profiles := make([]Profile, 0, len(env.Profiles))
	seen := make(map[string]struct{}, len(env.Profiles))
	// rewrite becomes true when a profile omits the provider key, so we can
	// persist the defaulted-in value and take the fast path next load.
	rewrite := false
	for i := range env.Profiles {
		pe := &env.Profiles[i]
		if pe.Name == "" {
			return nil, fmt.Errorf("config %s: profiles[%d] needs a 'name'", path, i)
		}
		if _, dup := seen[pe.Name]; dup {
			return nil, fmt.Errorf("config %s: duplicate profile name %q", path, pe.Name)
		}
		seen[pe.Name] = struct{}{}

		if _, ok := pe.Rest["provider"]; !ok {
			rewrite = true
		}

		body, merr := yaml.Marshal(pe.Rest)
		if merr != nil {
			return nil, fmt.Errorf("config %s: profile %q: %w", path, pe.Name, merr)
		}
		cfg, cerr := decodeConfig(path, body)
		if cerr != nil {
			return nil, fmt.Errorf("config %s: profile %q: %w", path, pe.Name, cerr)
		}
		profiles = append(profiles, Profile{Name: pe.Name, Config: cfg})
	}

	if rewrite {
		if serr := Save(path, profiles); serr != nil {
			return nil, fmt.Errorf("backfilling provider in config %s: %w", path, serr)
		}
	}

	return profiles, nil
}

// decodeConfig parses a single Config body over freshly-seeded defaults and
// validates it. Seeding before the unmarshal is what lets an omitted key keep
// its default while an explicit value (including false) overrides it.
func decodeConfig(path string, data []byte) (Config, error) {
	cfg := defaults()

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if cfg.Provider == "" {
		cfg.Provider = ProviderGitHub
	}
	if cfg.Provider != ProviderGitHub {
		return Config{}, fmt.Errorf("config %s: unsupported provider %q (only %q is supported)", path, cfg.Provider, ProviderGitHub)
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

// Save writes profiles to path as YAML, creating parent directories as needed.
// It backs the legacy migration and any future profile edits. Note that this
// emits plain YAML — the annotated comments in a hand-written or Init-generated
// file are not preserved across a rewrite.
func Save(path string, profiles []Profile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(file{Profiles: profiles})
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err = os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}

	return nil
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

# profiles: one or more named contexts to triage, each shown as its own tab in
# the TUI. Add more entries to track work, personal, and side-project PRs side
# by side; every field below repeats per profile.
profiles:
  - name: default

    # provider: the forge these repos live on. Only "github" is supported today;
    # the field is here so other forges can be added later without a schema
    # break. Omit it and prez fills in "github".
    provider: github

    # repos: the PRs to triage are pulled from these repositories. Required;
    # at least one entry with both owner and name.
    repos:
      - owner: ""
        name: ""

    # authors: restrict which PR authors appear. When both teams and include
    # are empty the allowlist is off and every author is shown; exclude always
    # applies.
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

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(annotatedTemplate), 0o600); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}

	return nil
}

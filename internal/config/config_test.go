package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTemp(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// loadOne loads a legacy single-config fixture and returns the single migrated
// profile's Config, asserting the file was wrapped as a lone "default" profile.
func loadOne(t *testing.T, contents string) Config {
	t.Helper()
	path := writeTemp(t, contents)
	profiles, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}
	if profiles[0].Name != defaultProfileName {
		t.Fatalf("profiles[0].Name = %q, want %q", profiles[0].Name, defaultProfileName)
	}
	return profiles[0].Config
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg := loadOne(t, "repos:\n  - owner: acme\n    name: some-service\n")

	if cfg.PollInterval != 5*time.Minute {
		t.Errorf("PollInterval = %v, want 5m default", cfg.PollInterval)
	}
	if len(cfg.Authors.Exclude) != 1 || cfg.Authors.Exclude[0] != "app/dependabot" {
		t.Errorf("Authors.Exclude = %v, want default dependabot exclusion", cfg.Authors.Exclude)
	}
	if !cfg.ReviewFilter.Enabled {
		t.Error("ReviewFilter.Enabled should default to true")
	}
	if cfg.Checks.HideFailing {
		t.Error("Checks.HideFailing should default to false")
	}
	if cfg.IncludeDrafts {
		t.Error("IncludeDrafts should default to false")
	}
	if cfg.Provider != ProviderGitHub {
		t.Errorf("Provider = %q, want %q default", cfg.Provider, ProviderGitHub)
	}
}

func TestLoadRejectsUnsupportedProvider(t *testing.T) {
	path := writeTemp(t, "provider: gitlab\nrepos:\n  - owner: acme\n    name: some-service\n")
	if _, err := Load(path); err == nil {
		t.Error("expected an error for an unsupported provider")
	}
}

func TestLoadAcceptsExplicitGitHubProvider(t *testing.T) {
	cfg := loadOne(t, "provider: github\nrepos:\n  - owner: acme\n    name: some-service\n")
	if cfg.Provider != ProviderGitHub {
		t.Errorf("Provider = %q, want %q", cfg.Provider, ProviderGitHub)
	}
}

// A profiles file that predates the provider field must be backfilled with the
// github default and rewritten on disk so the next load takes the fast path.
func TestLoadBackfillsMissingProviderOnDisk(t *testing.T) {
	path := writeTemp(t, `
profiles:
  - name: work
    repos:
      - owner: acme
        name: some-service
`)

	profiles, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(profiles) != 1 || profiles[0].Provider != ProviderGitHub {
		t.Fatalf("profiles = %+v, want one github-provider profile", profiles)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "provider: github") {
		t.Errorf("rewritten file does not contain the backfilled provider:\n%s", data)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	cfg := loadOne(t, `
repos:
  - owner: acme
    name: some-service
authors:
  teams:
    - org: acme
      slug: platform
  include: [contractor]
  exclude: [someone]
checks:
  hide_failing: true
review_filter:
  enabled: false
include_drafts: true
poll_interval: 90s
`)

	if cfg.PollInterval != 90*time.Second {
		t.Errorf("PollInterval = %v, want 90s", cfg.PollInterval)
	}
	if !cfg.IncludeDrafts {
		t.Error("IncludeDrafts should be true")
	}
	if len(cfg.Authors.Exclude) != 1 || cfg.Authors.Exclude[0] != "someone" {
		t.Errorf("Authors.Exclude = %v, want [someone]", cfg.Authors.Exclude)
	}
	if len(cfg.Authors.Include) != 1 || cfg.Authors.Include[0] != "contractor" {
		t.Errorf("Authors.Include = %v, want [contractor]", cfg.Authors.Include)
	}
	if len(cfg.Authors.Teams) != 1 || cfg.Authors.Teams[0] != (Team{Org: "acme", Slug: "platform"}) {
		t.Errorf("Authors.Teams = %v, want [acme/platform]", cfg.Authors.Teams)
	}
	if !cfg.Checks.HideFailing {
		t.Error("Checks.HideFailing should be true")
	}
	if cfg.ReviewFilter.Enabled {
		t.Error("ReviewFilter.Enabled should be false when explicitly disabled")
	}
}

// A partial authors block must not wipe out the default exclude list.
func TestLoadPreservesDefaultExcludeWhenOmitted(t *testing.T) {
	cfg := loadOne(t, `
repos:
  - owner: acme
    name: some-service
authors:
  include: [someone]
`)
	if len(cfg.Authors.Exclude) != 1 || cfg.Authors.Exclude[0] != "app/dependabot" {
		t.Errorf("Authors.Exclude = %v, want default dependabot exclusion preserved", cfg.Authors.Exclude)
	}
}

func TestLoadRequiresTeamOrgAndSlug(t *testing.T) {
	path := writeTemp(t, `
repos:
  - owner: acme
    name: some-service
authors:
  teams:
    - org: acme
`)
	if _, err := Load(path); err == nil {
		t.Error("expected an error for a team missing 'slug'")
	}
}

func TestLoadRequiresAtLeastOneRepo(t *testing.T) {
	path := writeTemp(t, "include_drafts: true\n")
	if _, err := Load(path); err == nil {
		t.Error("expected an error for a config with no repos")
	}
}

func TestLoadRequiresOwnerAndName(t *testing.T) {
	path := writeTemp(t, "repos:\n  - owner: acme\n")
	if _, err := Load(path); err == nil {
		t.Error("expected an error for a repo missing 'name'")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("/nonexistent/path/config.yaml"); err == nil {
		t.Error("expected an error for a missing config file")
	}
}

func TestLoadMultipleProfiles(t *testing.T) {
	path := writeTemp(t, `
profiles:
  - name: work
    repos:
      - owner: acme
        name: some-service
    review_filter:
      enabled: false
  - name: personal
    repos:
      - owner: verygoodsoftwarenotvirus
        name: prez
    poll_interval: 30s
`)

	profiles, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("len(profiles) = %d, want 2", len(profiles))
	}

	// Order is preserved.
	if profiles[0].Name != "work" || profiles[1].Name != "personal" {
		t.Fatalf("profile names = %q, %q; want work, personal", profiles[0].Name, profiles[1].Name)
	}

	// Per-profile defaults apply independently: work omits poll_interval so it
	// gets the 5m default; personal omits authors so it keeps the dependabot
	// exclude; work's explicit enabled:false must stick.
	work, personal := profiles[0].Config, profiles[1].Config
	if work.PollInterval != 5*time.Minute {
		t.Errorf("work.PollInterval = %v, want 5m default", work.PollInterval)
	}
	if work.ReviewFilter.Enabled {
		t.Error("work.ReviewFilter.Enabled should stay false")
	}
	if personal.PollInterval != 30*time.Second {
		t.Errorf("personal.PollInterval = %v, want 30s", personal.PollInterval)
	}
	if len(personal.Authors.Exclude) != 1 || personal.Authors.Exclude[0] != "app/dependabot" {
		t.Errorf("personal.Authors.Exclude = %v, want default dependabot exclusion", personal.Authors.Exclude)
	}
}

func TestLoadMigratesLegacyConfigOnDisk(t *testing.T) {
	path := writeTemp(t, "repos:\n  - owner: acme\n    name: some-service\npoll_interval: 90s\n")

	profiles, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(profiles) != 1 || profiles[0].Name != defaultProfileName {
		t.Fatalf("profiles = %+v, want one %q profile", profiles, defaultProfileName)
	}

	// The file on disk was rewritten in the profiles shape.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "profiles:") || !strings.Contains(string(data), "name: default") {
		t.Errorf("migrated file does not contain a profiles/default block:\n%s", data)
	}

	// Reloading the rewritten file yields the same profile.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if len(reloaded) != 1 || reloaded[0].Name != defaultProfileName {
		t.Fatalf("reloaded = %+v, want one %q profile", reloaded, defaultProfileName)
	}
	if reloaded[0].PollInterval != 90*time.Second {
		t.Errorf("reloaded PollInterval = %v, want 90s preserved across migration", reloaded[0].PollInterval)
	}
	if reloaded[0].Repos[0] != (Repo{Owner: "acme", Name: "some-service"}) {
		t.Errorf("reloaded repo = %v, want acme/some-service", reloaded[0].Repos[0])
	}
}

func TestLoadRequiresProfileName(t *testing.T) {
	path := writeTemp(t, `
profiles:
  - repos:
      - owner: acme
        name: some-service
`)
	if _, err := Load(path); err == nil {
		t.Error("expected an error for a profile missing 'name'")
	}
}

func TestLoadRejectsDuplicateProfileNames(t *testing.T) {
	path := writeTemp(t, `
profiles:
  - name: work
    repos:
      - owner: acme
        name: some-service
  - name: work
    repos:
      - owner: verygoodsoftwarenotvirus
        name: prez
`)
	if _, err := Load(path); err == nil {
		t.Error("expected an error for duplicate profile names")
	}
}

func TestLoadValidatesEachProfilesRepos(t *testing.T) {
	path := writeTemp(t, `
profiles:
  - name: work
    repos:
      - owner: acme
        name: some-service
  - name: personal
    review_filter:
      enabled: true
`)
	if _, err := Load(path); err == nil {
		t.Error("expected an error for a profile with no repos")
	}
}

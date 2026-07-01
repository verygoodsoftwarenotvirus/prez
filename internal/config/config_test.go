package config

import (
	"os"
	"path/filepath"
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

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeTemp(t, "repos:\n  - owner: acme\n    name: some-service\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
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
}

func TestLoadOverridesDefaults(t *testing.T) {
	path := writeTemp(t, `
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

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
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
	path := writeTemp(t, `
repos:
  - owner: acme
    name: some-service
authors:
  include: [someone]
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
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

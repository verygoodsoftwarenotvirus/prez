// Package provider abstracts the forge prez triages PRs from. GitHub is the
// only implementation today; the interface and factory exist so a second forge
// (GitLab, Codeberg) is a new implementation of one contract rather than a
// scattered rewrite.
package provider

import (
	"context"
	"fmt"

	"github.com/verygoodsoftwarenotvirus/prez/internal/config"
	"github.com/verygoodsoftwarenotvirus/prez/internal/ghauth"
	"github.com/verygoodsoftwarenotvirus/prez/internal/ghclient"
	"github.com/verygoodsoftwarenotvirus/prez/internal/triage"
)

// Provider is the forge contract the TUI drives: resolve the viewer, list open
// PRs for a repo, and resolve a team's members. Every method returns
// provider-neutral triage types, so nothing above this layer knows which forge
// the data came from.
type Provider interface {
	Viewer(ctx context.Context) (string, error)
	OpenPullRequests(ctx context.Context, owner, name string) ([]triage.PullRequest, error)
	TeamMembers(ctx context.Context, org, slug string) ([]string, error)
}

// New builds the provider named by a profile's config.Provider, owning that
// forge's authentication. GitHub authenticates via the local gh CLI. An
// unknown name is an error, mirroring the validation in config.Load.
func New(ctx context.Context, name string) (Provider, error) {
	switch name {
	case config.ProviderGitHub:
		token, err := ghauth.Token(ctx)
		if err != nil {
			return nil, err
		}
		return ghclient.New(token), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", name)
	}
}

// Resolve returns one Provider per profile, aligned by index, constructing each
// distinct provider only once so profiles sharing a forge share its client (and
// its single authentication).
func Resolve(ctx context.Context, profiles []config.Profile) ([]Provider, error) {
	byName := make(map[string]Provider, len(profiles))
	out := make([]Provider, len(profiles))
	for i := range profiles {
		name := profiles[i].Provider
		p, ok := byName[name]
		if !ok {
			var err error
			p, err = New(ctx, name)
			if err != nil {
				return nil, err
			}
			byName[name] = p
		}
		out[i] = p
	}
	return out, nil
}

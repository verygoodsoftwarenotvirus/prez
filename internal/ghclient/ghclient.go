// Package ghclient fetches open pull requests (with their reviews and
// review requests) via the GitHub GraphQL API and converts them into the
// provider-agnostic triage.PullRequest type.
package ghclient

import (
	"context"
	"net/http"
	"time"

	"github.com/verygoodsoftwarenotvirus/prez/internal/triage"

	"github.com/shurcooL/githubv4"
)

// Client fetches PR data from GitHub. It satisfies a small interface so the
// TUI and evaluator never depend on githubv4 directly.
type Client struct {
	v4 *githubv4.Client
}

// New builds a Client authenticated with the given token. The token is sent
// as a bearer token on every request via a custom RoundTripper, so callers
// don't need golang.org/x/oauth2.
func New(token string) *Client {
	hc := &http.Client{
		Transport: bearerTransport{token: token, base: http.DefaultTransport},
		Timeout:   30 * time.Second,
	}
	return &Client{v4: githubv4.NewClient(hc)}
}

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "bearer "+t.token)
	return t.base.RoundTrip(req)
}

// Viewer returns the login of the authenticated user.
func (c *Client) Viewer(ctx context.Context) (string, error) {
	var q struct {
		Viewer struct {
			Login githubv4.String
		}
	}
	if err := c.v4.Query(ctx, &q, nil); err != nil {
		return "", err
	}
	return string(q.Viewer.Login), nil
}

type reviewerNode struct {
	RequestedReviewer struct {
		User struct {
			Login githubv4.String
		} `graphql:"... on User"`
	}
}

type reviewNode struct {
	Author struct {
		Login githubv4.String
	}
	State       githubv4.String
	SubmittedAt githubv4.DateTime
	Commit      struct {
		Oid githubv4.String
	}
}

type prNode struct {
	UpdatedAt  githubv4.DateTime
	Title      githubv4.String
	URL        githubv4.String
	HeadRefOid githubv4.String
	Author     struct{ Login githubv4.String }
	// Commits pulls the head commit's overall check rollup. The rollup object
	// is null when the PR has no checks, in which case State decodes to "".
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup struct {
					State githubv4.String
				}
			}
		}
	} `graphql:"commits(last: 1)"`
	Reviews        struct{ Nodes []reviewNode }   `graphql:"reviews(last: 30)"`
	ReviewRequests struct{ Nodes []reviewerNode } `graphql:"reviewRequests(first: 15)"`
	Number         githubv4.Int
	IsDraft        githubv4.Boolean
}

type openPRsQuery struct {
	Repository struct {
		PullRequests struct {
			PageInfo struct {
				EndCursor   githubv4.String
				HasNextPage githubv4.Boolean
			}
			Nodes []prNode
		} `graphql:"pullRequests(states: OPEN, first: 50, after: $cursor, orderBy: {field: UPDATED_AT, direction: DESC})"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// OpenPullRequests fetches every open PR in owner/name, paginating as
// needed, and converts each into a triage.PullRequest.
func (c *Client) OpenPullRequests(ctx context.Context, owner, name string) ([]triage.PullRequest, error) {
	var (
		out    []triage.PullRequest
		cursor *githubv4.String
	)

	for {
		var q openPRsQuery
		vars := map[string]any{
			"owner":  githubv4.String(owner),
			"name":   githubv4.String(name),
			"cursor": cursor,
		}
		if err := c.v4.Query(ctx, &q, vars); err != nil {
			return nil, err
		}

		for i := range q.Repository.PullRequests.Nodes {
			out = append(out, convert(owner, name, q.Repository.PullRequests.Nodes[i]))
		}

		if !bool(q.Repository.PullRequests.PageInfo.HasNextPage) {
			break
		}
		c := q.Repository.PullRequests.PageInfo.EndCursor
		cursor = &c
	}

	return out, nil
}

func convert(owner, name string, n prNode) triage.PullRequest {
	pr := triage.PullRequest{
		Repo:      owner + "/" + name,
		Number:    int(n.Number),
		Title:     string(n.Title),
		URL:       string(n.URL),
		Author:    string(n.Author.Login),
		IsDraft:   bool(n.IsDraft),
		HeadOID:   string(n.HeadRefOid),
		UpdatedAt: n.UpdatedAt.Time,
	}

	if commits := n.Commits.Nodes; len(commits) > 0 {
		pr.CheckStatus = checkStatus(string(commits[0].Commit.StatusCheckRollup.State))
	}

	for i := range n.Reviews.Nodes {
		r := &n.Reviews.Nodes[i]
		pr.Reviews = append(pr.Reviews, triage.Review{
			Author:      string(r.Author.Login),
			State:       string(r.State),
			CommitOID:   string(r.Commit.Oid),
			SubmittedAt: r.SubmittedAt.Time,
		})
	}

	for _, rr := range n.ReviewRequests.Nodes {
		if login := string(rr.RequestedReviewer.User.Login); login != "" {
			pr.RequestedReviewers = append(pr.RequestedReviewers, login)
		}
	}

	return pr
}

// checkStatus launders GitHub's status-check rollup state into the
// provider-neutral triage.CheckStatus. An empty or unknown state (a PR with no
// checks) maps to triage.CheckNone.
func checkStatus(rollup string) triage.CheckStatus {
	switch rollup {
	case "SUCCESS":
		return triage.CheckPassing
	case "FAILURE", "ERROR":
		return triage.CheckFailing
	case "PENDING", "EXPECTED":
		return triage.CheckPending
	default:
		return triage.CheckNone
	}
}

type teamMembersQuery struct {
	Organization struct {
		Team struct {
			Members struct {
				PageInfo struct {
					EndCursor   githubv4.String
					HasNextPage githubv4.Boolean
				}
				Nodes []struct {
					Login githubv4.String
				}
			} `graphql:"members(first: 100, after: $cursor)"`
		} `graphql:"team(slug: $slug)"`
	} `graphql:"organization(login: $org)"`
}

// TeamMembers returns the logins of every member of org/slug, paginating as
// needed. The gh token must have read:org scope for this to succeed.
func (c *Client) TeamMembers(ctx context.Context, org, slug string) ([]string, error) {
	var (
		out    []string
		cursor *githubv4.String
	)

	for {
		var q teamMembersQuery
		vars := map[string]any{
			"org":    githubv4.String(org),
			"slug":   githubv4.String(slug),
			"cursor": cursor,
		}
		if err := c.v4.Query(ctx, &q, vars); err != nil {
			return nil, err
		}

		for _, n := range q.Organization.Team.Members.Nodes {
			out = append(out, string(n.Login))
		}

		if !bool(q.Organization.Team.Members.PageInfo.HasNextPage) {
			break
		}
		cur := q.Organization.Team.Members.PageInfo.EndCursor
		cursor = &cur
	}

	return out, nil
}

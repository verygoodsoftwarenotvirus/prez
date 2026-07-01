# prez

A terminal UI for triaging GitHub pull requests across one or more repos,
specifically built to fix a gap in GitHub's own search filters: a PR you've
already reviewed, where nothing has changed since, still shows up in
`-review:approved` searches if your review wasn't an approval.

## The core idea

GitHub records the commit SHA each review was submitted against
(`commit.oid` in GraphQL). prez compares that SHA to the PR's current
head SHA:

- **never reviewed by you** → `NEW`
- **your last review's SHA == current head SHA** → `WAITING` (nothing new,
  it's on the author — hidden from the list by default)
- **your last review's SHA != current head SHA** → `STALE` (new commits
  landed since you looked — actionable again)
- **you're currently in the PR's requested reviewers** → `RE-REQUESTED`
  (shown regardless of SHA, since an explicit re-request is a strong signal
  even without new commits)

All of that logic lives in `internal/triage`, with no dependency on the
GitHub client or the TUI — see `triage_test.go` for the behavior spelled
out as test cases.

## Setup

1. Install and authenticate the GitHub CLI if you haven't already:
   `gh auth login`. prez shells out to `gh auth token` rather than
   managing its own credentials.
2. Copy `config.example.yaml` to `~/.config/prez/config.yaml` (or
   anywhere, and pass `--config path/to/file.yaml`) and list the repos you
   want to watch:

   ```yaml
   repos:
     - owner: acme
       name: some-service

   include_drafts: false
   poll_interval: 5m
   ```

   On top of the repo list, three optional filters narrow the list further —
   each is off unless you configure it, so you can enable any combination:

   - **`authors`** — show only PRs by members of one or more GitHub `teams`
     (resolved from the API at startup; needs `read:org` on your gh token),
     plus any extra `include` logins, minus any `exclude` logins.
   - **`checks`** — with `hide_failing: true`, drop PRs whose CI rollup is
     `FAILURE`/`ERROR`. Passing, pending, and check-less PRs still show, and
     each row shows a `checks ✓/✗/…` indicator.
   - **`review_filter`** — set `enabled: false` to turn off the review-state
     filtering below and just see every PR matching the other filters.

   See `config.example.yaml` for the full annotated schema.

3. Build and run:

   ```sh
   go build -o prez ./cmd/prez
   ./prez
   ```

## Keys

- `↑`/`↓` — navigate
- `enter` — open the selected PR in your browser
- `r` — refresh now
- `w` — toggle visibility of `WAITING` PRs
- `/` — filter the list
- `q` / `ctrl+c` — quit

It also auto-refreshes on `poll_interval` in the background.

## A note on this sandbox's go.mod

This was built and tested in an environment without access to
`proxy.golang.org`, so `go.mod` has `replace` directives pointing a few
`golang.org/x/*` transitive dependencies at their GitHub mirrors
(`github.com/golang/sys`, etc.) as a workaround. On a machine with normal
internet access these aren't necessary — feel free to delete the `replace`
block at the bottom of `go.mod` and run `go mod tidy`.

## Extending it

The places designed to be swapped out:

- `internal/triage.Evaluator` — the staleness policy itself. If you want
  different rules (e.g. treat `COMMENTED` differently from
  `CHANGES_REQUESTED`, or factor in PR age), implement the interface and
  pass it into `tui.New`.
- `tui.Source` — defined at the TUI's call site, satisfied today by
  `internal/ghclient.Client`. A caching layer or a fake for tests just needs
  to implement `Viewer`, `OpenPullRequests`, and `TeamMembers`.

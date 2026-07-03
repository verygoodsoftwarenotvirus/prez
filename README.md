# prez

A terminal UI for triaging GitHub pull requests across one or more repos.

Its whole reason for existing is to fix a gap in GitHub's own search filters: a
PR you've **already reviewed**, where **nothing has changed since**, still shows
up in `-review:approved` searches if your review wasn't an approval. So your
review queue never really empties — it stays cluttered with PRs that are
genuinely waiting on the author, not on you. prez sorts those out and hides them
by default, leaving you a short list of what actually needs your eyes right now.

The name is exactly what it sounds like: prez makes **PR**s **EZ**.

If you've never set up a command-line tool from source before, that's fine —
this README walks through every step, and explains *why* each configuration
option exists so you can decide what you actually need.

---

## Table of contents

- [What you'll need](#what-youll-need)
- [Installation](#installation)
- [Quick start](#quick-start)
- [How triage works (the core idea)](#how-triage-works-the-core-idea)
- [Configuration reference](#configuration-reference)
  - [Profiles](#profiles)
  - [Every field, and the *why* behind it](#every-field-and-the-why-behind-it)
- [Using the app](#using-the-app)
- [Troubleshooting](#troubleshooting)
- [Extending it](#extending-it)

---

## What you'll need

Three things, all free:

1. **A GitHub account** — the one you review PRs with.
2. **The GitHub CLI (`gh`)** — prez doesn't manage its own login or store any
   tokens. Instead it borrows the credentials `gh` already has by shelling out
   to `gh auth token`. Install it from <https://cli.github.com>, then run:

   ```sh
   gh auth login
   ```

   Follow the prompts (choose GitHub.com, HTTPS, and log in via browser — the
   defaults are fine). If you want to filter PRs by GitHub **team membership**
   later, add the `read:org` scope when you authenticate (see
   [`authors`](#authors) below for why).

3. **Go 1.26 or newer** — needed only to build prez from source. Get it from
   <https://go.dev/dl>. Check your version with `go version`.

You do **not** need to create a personal access token, set environment
variables, or paste any secrets. `gh` handles all of that.

---

## Installation

The quickest way is `go install`, which downloads, builds, and drops the binary
into Go's bin directory (`$(go env GOPATH)/bin`, usually `~/go/bin`):

```sh
go install github.com/verygoodsoftwarenotvirus/prez/cmd/prez@latest
```

Make sure that directory is on your `PATH` and you can then run `prez` from
anywhere.

Prefer to build from a clone (e.g. to hack on it)?

```sh
git clone https://github.com/verygoodsoftwarenotvirus/prez
cd prez
go build -o prez ./cmd/prez
```

That produces a `prez` executable in the current directory; run it with `./prez`
or move it onto your `PATH`. If you have `make`, `make build` compiles all
packages as a sanity check.

---

## Quick start

**1. Generate a config file.** prez can write you a fully-commented starter
config with every option set to its default:

```sh
prez init
```

This creates `~/.config/prez/config.yaml`. (Want it elsewhere? Use
`prez init --config ./my-config.yaml`.) `init` refuses to overwrite an
existing file, so it's safe to run.

**2. Tell it which repos to watch.** Open the file it just made and fill in the
`repos:` section — this is the one field you *must* set. For example, to triage
the GitHub CLI's own repository:

```yaml
    repos:
      - owner: cli
        name: cli
```

`owner` is the user or org, `name` is the repository — together they're the
`owner/name` you see in any GitHub URL.

**3. Run it.**

```sh
prez
```

prez looks up your GitHub login (via `gh`), fetches the open PRs, decides which
ones need your attention, and drops you into the list. If you put your config
somewhere other than the default path, point at it with
`prez --config ./my-config.yaml`.

That's the whole loop. Everything below is detail you can reach for as you want
to narrow the list down or watch several contexts at once.

---

## How triage works (the core idea)

GitHub records the exact commit SHA each review was submitted against
(`commit.oid` in its GraphQL API). prez compares that SHA against the PR's
**current** head SHA to decide, for *you specifically*, whether a PR is still
your problem. Each PR lands in one of four buckets, shown as a colored badge on
its row:

| Badge | Meaning | Shown by default? |
|-------|---------|-------------------|
| 🔴 **NEW** | You've never reviewed this PR. | Yes |
| 🟣 **RE-REQUESTED** | You're currently in the PR's requested reviewers — an explicit re-request. Shown regardless of SHA, because someone asking for you again is a strong signal even without new commits. | Yes |
| 🟡 **STALE** | You reviewed before, but new commits have landed since (the head SHA no longer matches the one you reviewed). Actionable again. | Yes |
| ⚪ **WAITING** | Your last review still applies to the current head commit — nothing new for you to look at. It's on the author now. | **No** (press `w` to reveal) |

The list is sorted most-urgent first (NEW → RE-REQUESTED → STALE → WAITING),
with ties broken by most-recently-updated. The tab title tells you the count,
e.g. `(3 actionable, 12 waiting, hidden)`.

All of this decision logic lives in `internal/triage`, deliberately isolated
from the GitHub client and the terminal UI — it's a plain, testable function.
`internal/triage/triage_test.go` spells the behavior out case by case if you
want to see the edge cases.

---

## Configuration reference

Your config is a YAML file (default location `~/.config/prez/config.yaml`). The
fastest way to a valid one is `prez init`, which writes an annotated template.
This section is the long-form version of those comments.

### Profiles

The file is a list of **profiles**. A profile is one self-contained triage
context — its own set of repos and its own filters. Each profile becomes a
**tab** in the UI, so a single prez window can keep, say, your work repos, your
personal projects, and an open-source project you help maintain side by side,
each refreshing on its own schedule, without juggling multiple config files or
restarting.

```yaml
profiles:
  - name: work
    repos:
      - owner: acme
        name: some-service
      - owner: acme
        name: another-service
    poll_interval: 2m

  - name: personal
    repos:
      - owner: your-username
        name: your-side-project
    include_drafts: true
```

If you only ever want one context, just keep a single profile — you'll get a
clean, tab-less list.

> **Migration:** older single-context config files (a flat file with no
> `profiles:` key) are still accepted. On first load prez wraps such a file into
> one profile named `default` and rewrites it into the profiles shape. One
> caveat: when prez rewrites your file (this migration, or backfilling an
> omitted `provider`), it emits plain YAML and **your hand-written comments are
> not preserved**. The `init` template avoids triggering a rewrite, so its
> comments stick around.

### Every field, and the *why* behind it

Every field below lives *inside* a profile and repeats per profile. Only `repos`
is required; everything else has a sensible default and can be omitted.

---

#### `name`

```yaml
name: work
```

The label shown on this profile's tab. Required when you have more than one
profile, and must be unique across the file. Purely cosmetic — pick whatever
helps you tell your contexts apart at a glance.

---

#### `provider`

```yaml
provider: github
```

Which forge (code host) these repos live on. **`github` is the only supported
value today** — the field exists so support for GitLab, Codeberg, and friends
can be added later without breaking existing configs. Omit it and prez fills in
`github` for you. Any other value is rejected at startup with a clear message.

---

#### `repos` — *required*

```yaml
repos:
  - owner: cli
    name: cli
  - owner: charmbracelet
    name: bubbletea
```

The repositories whose open PRs you want to triage. This is the **one field you
must fill in** — a profile with an empty `repos` list fails to load with an
explanatory error, on purpose, so a freshly-generated config nudges you to fill
it in rather than silently showing nothing.

Each entry needs both `owner` (the user or org) and `name` (the repository).
You need read access to each repo; private repos work fine as long as the `gh`
account you logged in with can see them. All the repos in a profile are fetched
in parallel and merged into that profile's single list.

---

#### `authors`

*Why it exists:* on a busy repo, most open PRs aren't yours to review. This
filter narrows the list to just the people whose work you care about.

```yaml
authors:
  teams:
    - org: acme
      slug: backend
  include:
    - a-specific-collaborator
  exclude:
    - app/dependabot
```

The allowlist is built from **`teams` + `include`**, then **`exclude`** is
subtracted from the result:

- **`teams`** — GitHub teams whose *current members* should be shown. prez
  resolves each team's roster from the GitHub API once at startup and caches it
  (a restart picks up roster changes). Reading team membership requires your
  `gh` token to carry the **`read:org`** scope — if you didn't grant it during
  `gh auth login`, add it with `gh auth refresh -s read:org`. Each team needs
  both an `org` and a `slug` (the slug is the team's URL name, e.g. the
  `backend` in `github.com/orgs/acme/teams/backend`).
- **`include`** — extra individual logins to show on top of any team members.
  Handy for that one external contributor who isn't on a team.
- **`exclude`** — logins to drop *even if* a team or `include` would have
  allowed them. This one always applies. The default config excludes
  `app/dependabot` so bot PRs don't drown out human ones — remove it if you do
  want to review dependency bumps here.

**Key behavior:** if both `teams` and `include` are empty, the allowlist is
switched **off** and *every* author is shown. `exclude` still applies. So the
default (no teams, no includes, dependabot excluded) means "show everyone's PRs
except the bot."

---

#### `checks`

*Why it exists:* a PR whose CI is red usually isn't ready for your review yet —
the author still has work to do. This lets you hide those.

```yaml
checks:
  hide_failing: false
```

- **`hide_failing`** — when `true`, drops PRs whose overall check rollup is
  `FAILURE` or `ERROR`. Passing, still-pending, and check-less PRs are **always
  shown** — the assumption is "red means not-ready," while pending or absent
  checks aren't a reason to hide something. Defaults to `false` (show
  everything).

Regardless of this setting, each PR row shows a compact indicator of its check
state: `checks ✓` (passing), `checks ✗` (failing), or `checks …` (pending). PRs
with no checks configured show nothing.

---

#### `review_filter`

*Why it exists:* this is prez's whole reason for being — the SHA-comparison
triage described [above](#how-triage-works-the-core-idea). But sometimes you
just want to see *every* open PR in a repo, triage aside. This toggle lets you.

```yaml
review_filter:
  enabled: true
```

- **`enabled`** — defaults to `true`, giving you the NEW / RE-REQUESTED / STALE /
  WAITING triage. Set it to `false` and prez stops filtering by review state
  entirely: you get every PR that passes the *other* filters (authors, checks,
  drafts), review status ignored. The tab title switches to
  `(N shown, review filter off)` so it's obvious you're in this mode.

Turn it off for a profile you use as a plain "what's open in these repos" board,
leave it on for your actual review queue.

---

#### `include_drafts`

```yaml
include_drafts: false
```

Whether draft PRs appear in the list. Defaults to `false`, since a draft is by
definition not ready for review. Flip it to `true` on a profile where you want
early eyes on work-in-progress (e.g. your own team's in-flight branches).

---

#### `poll_interval`

```yaml
poll_interval: 5m
```

How often this profile refreshes itself in the background, written as a
[Go duration](https://pkg.go.dev/time#ParseDuration): `30s`, `5m`, `1h`, or
combinations like `1m30s`. Defaults to `5m`. Each profile polls on its own
timer, so a fast-moving work repo can refresh every couple of minutes while a
quiet side project sits at `30m`. You can always force an immediate refresh with
`r`.

Be a considerate API citizen: very short intervals across many repos mean a lot
of GraphQL calls. A few minutes is plenty for a review queue.

---

## Using the app

Once prez is running:

**Navigation & actions**

- `↑` / `↓` — move through the list
- `enter` — open the selected PR in your default browser
- `r` — refresh the current profile now
- `w` — toggle visibility of `WAITING` PRs (the ones hidden by default)
- `/` — filter the list by typing (matches PR title and repo); `esc` clears it
- `q` or `ctrl+c` — quit

**Switching profiles** (only when you have more than one)

- `tab` / `shift+tab` — cycle to the next / previous profile
- `1`–`9` — jump straight to a profile by position

Each tab shows an actionable count in parentheses when it has PRs waiting on you,
so you can see at a glance which context needs attention without switching to it.
Profiles also refresh on their own `poll_interval` in the background regardless
of which tab you're looking at.

---

## Troubleshooting

**`gh CLI not found on PATH`** — prez can't find the GitHub CLI. Install it from
<https://cli.github.com> and make sure `gh --version` works in the same shell.

**`gh auth token failed (is gh auth login done?)`** — `gh` is installed but not
logged in (or the login expired). Run `gh auth login`.

**`unsupported provider "…"`** — a profile's `provider` is set to something other
than `github`. Remove the field or set it to `github`.

**`at least one entry under 'repos' is required`** — you ran prez against a
config whose `repos` list is empty. This is the expected message for a
freshly-`init`ed file; fill in `owner`/`name` and try again.

**`resolving team …` errors, or teams show no members** — your `gh` token is
missing the `read:org` scope needed to read team membership. Add it with
`gh auth refresh -s read:org`.

**Nothing shows up** — check whether everything is `WAITING` (press `w` to
reveal), whether an `authors` allowlist is filtering out the PRs you expected, or
whether `hide_failing`/`include_drafts` are excluding them.

---

## Extending it

The seams designed to be swapped out:

- **`internal/triage.Evaluator`** — the staleness policy itself. Want different
  rules (treat `COMMENTED` differently from `CHANGES_REQUESTED`, factor in PR
  age, whatever)? Implement the one-method interface and pass it in where the
  TUI is built. The rest of the app doesn't care how the decision is made.
- **`provider.Provider`** — the forge contract (`Viewer`, `OpenPullRequests`,
  `TeamMembers`), satisfied today by `internal/ghclient` for GitHub. A second
  forge, a caching layer, or a fake for tests is a new implementation of that
  one interface, not a rewrite. Everything above the provider layer speaks in
  provider-neutral triage types and never learns which forge the data came from.
</content>
</invoke>

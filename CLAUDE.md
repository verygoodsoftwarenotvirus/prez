# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go project (`github.com/verygoodsoftwarenotvirus/prez`), a terminal UI for triaging GitHub pull requests. Go 1.26.

## Common Commands

```bash
make format         # Format all Go code (imports, field alignment, tag alignment, gofmt)
make lint           # Run golangci-lint (Docker) + shellcheck
make format lint    # Typical workflow: format then lint
make test           # Run tests (race detector, shuffle, failfast)
make build          # Build all packages
make setup          # Create artifacts dir + vendor deps
make revendor       # Clean and re-vendor dependencies
```

Run a single test:
```bash
go test -run TestFunctionName ./package/path/...
```

Run tests for a single package:
```bash
go test -race ./package/...
```

Linting runs in Docker (`golangci/golangci-lint` image). Formatting runs locally with `gci`, `goimports`, `fieldalignment`, `tagalign`, and `gofmt`.

## Import Ordering

Import ordering uses `gci` with four sections, separated by blank lines:

1. Standard library
2. `github.com/verygoodsoftwarenotvirus/prez` (this module)
3. `github.com/verygoodsoftwarenotvirus` (org-level packages)
4. Everything else (third-party)

The Makefile `THIS` variable must be the full module path (`github.com/verygoodsoftwarenotvirus/prez`) because `format_imports.sh` uses `dirname` to derive the org prefix.

## Testing

- Tests use `stretchr/testify` (assert, require, mock)
- Tests call `t.Parallel()` by default
- `make test` excludes: cmd packages
- Test command: `CGO_ENABLED=1 go test -shuffle=on -race -vet=all -failfast`

## Linting

- 42+ linters enabled via `.golangci.yml` (golangci-lint v2 format)
- Formatters: `gci` and `gofmt` (configured in the `formatters:` section)
- Notable strictness: `errcheck`, `errorlint`, `gosec`, `forcetypeassert`, `unconvert`, `unparam`
- Many linters relaxed for `_test.go` files (gosec, goconst, forcetypeassert, unparam, etc.)

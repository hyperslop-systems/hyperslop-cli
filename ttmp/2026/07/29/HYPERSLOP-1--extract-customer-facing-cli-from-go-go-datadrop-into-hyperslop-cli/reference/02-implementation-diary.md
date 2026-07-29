---
Title: Implementation diary
Ticket: HYPERSLOP-1
Status: active
Topics:
    - cli
    - architecture
    - refactor
    - datadrop
    - hyperslop
    - glazed
DocType: reference
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: "Step-by-step implementation diary for building the HYPERSLOP-1 extraction: moving the customer-facing CLI out of go-go-datadrop into hyperslop-cli. Records what changed, why, commands run, failures verbatim, and commit hashes per phase."
LastUpdated: 2026-07-29T11:31:44.983890846-04:00
WhatFor: "So the build can be reviewed, resumed, and audited against the design doc's phased plan."
WhenToUse: "Read before resuming the HYPERSLOP-1 build or reviewing a phase's correctness."
---

# Implementation diary

## Goal

Record the step-by-step implementation of the HYPERSLOP-1 extraction (move the
customer-facing CLI from `go-go-datadrop` into `hyperslop-cli`), following the
design doc's 9-phase plan, with verbatim commands, failures, decisions, and
commit hashes, so each phase is reviewable and the build can be resumed.

Working directory: `/home/manuel/workspaces/2026-07-29/split-cli` (a `go.work`
workspace linking `./glazed`, `./go-go-datadrop`, `./hyperslop-cli`).

## Pre-flight: toolchain and baseline

The workspace `go.work` declared `go 1.25` but every module requires
`go 1.26.1` (`toolchain go1.26.5`). The installed `go` is 1.25.5 with
`GOTOOLCHAIN=auto`. I bumped `go.work` to `go 1.26.1` + `toolchain go1.26.5`,
which made `go` auto-fetch `go1.26.5` (`go version` → `go1.26.5 linux/amd64`).

Baseline (workspace on): `go build ./...` green in all three modules;
`go test ./...` in `go-go-datadrop` fully green (incl. the 28s `cmd/datadrop`
smoke suite, `pkg/auth`, `pkg/server` authz, `pkg/cli`, `pkg/client`,
`pkg/datadrop`, `pkg/tabular`). This is the contract I must not break.

The `go.work` change is workspace-local config (the `split-cli` dir is not a git
repo), so it is not committed; each module's own `go.mod` already pins
`go 1.26.1`/`toolchain go1.26.5` so they also build with `GOWORK=off`.

## Step 0: Phase 0 — Rename the hyperslop-cli scaffold

Renamed the `go-template` scaffold from the placeholder `github.com/go-go-golems/XXX`
to the real module `github.com/hyperslop-systems/hyperslop-cli`, binary `hyperslop`.
This is the foundation every later phase imports from, and the user's explicit
"make sure the package is hyperslop-systems/hyperslop-cli" requirement.

### Prompt Context

**User prompt (verbatim):** "buidl the ticket, keep a detailed diary as you work, commit at appropriat intervals, make sure the package is hyperslop-systems/hyperslop-cli . Budget 1M"

**Assistant interpretation:** Execute the HYPERSLOP-1 ticket's phased build plan, keep a detailed diary, commit per phase, and ensure the hyperslop-cli module path is `github.com/hyperslop-systems/hyperslop-cli`.

**Inferred user intent:** Actually implement the extraction (not just design it), with the package identity fixed to hyperslop-systems/hyperslop-cli.

**Commit (code):** b97e018 — "Phase 0 (HYPERSLOP-1): rename scaffold to github.com/hyperslop-systems/hyperslop-cli"

### What I did

- `go.mod`: `module github.com/go-go-golems/XXX` → `github.com/hyperslop-systems/hyperslop-cli`.
- `git mv cmd/XXX cmd/hyperslop`; wrote a minimal valid `cmd/hyperslop/main.go` (placeholder `func main(){}`; real wiring comes in Phase 5).
- `logcopter_generate.go`: `package XXX` → `package hyperslop_cli` (underscores for hyphens, matching how `go-go-datadrop` uses `package go_go_datadrop`); `//go:generate` area-prefix `go-go-golems.XXX` → `hyperslop-systems.hyperslop-cli`, strip-prefix `github.com/go-go-golems/XXX` → `github.com/hyperslop-systems/hyperslop-cli`. Ran `go generate ./...` which regenerated `pkg/logcopter.go` → `logcopter.Package("hyperslop-systems.hyperslop-cli.pkg")`.
- `.goreleaser.yaml`: `project_name`/build ids/`binary`/`main` → `hyperslop` (`main: ./cmd/hyperslop`); brews `name`/`description`/`homepage`, nfpm `vendor`/`homepage`/`description`, fury publisher org → `hyperslop-systems`.
- `Makefile`: `logcopter-check`, `release`, `install` (`HYPERSLOP_BINARY`, `./cmd/hyperslop`) updated.
- `.github/workflows/release.yaml`: `publish-docs` template `XXX` → `hyperslop` (package_name, export_command, vault_role). Left the goreleaser signing/secret jobs (GO_GO_GOLEMS_SIGN_KEY etc.) as-is — those are org-specific release infra resolved in Phase 9.

### Why

Fix the package identity first so every later `import "github.com/hyperslop-systems/hyperslop-cli/..."` resolves to the local worktree via the workspace, and so `go-go-datadrop` can depend on this module.

### What worked

- `go build ./...`, `go test ./...`, `gofmt -l .`, `go mod tidy` all clean in `hyperslop-cli` after the rename.

### What didn't work

- `go build ./...` from the workspace root failed with "directory prefix . does not contain modules" because the workspace root is not a module. Resolution: build per-module (`cd hyperslop-cli && go build ./...`), which is how the Makefile already operates (`GOWORK=off`).

### What I learned

- The only `XXX` left after the rename is in `AGENT.md` (generic go-go-golems template guidance: "Run a binary in XXX/YYY/FOOO" and "listen-killer kill --port XXX"). These are template boilerplate, not code; left untouched.
- The GoReleaser *publish* targets (brew tap, fury.io, GPG signing) reference go-go-golems org secrets/taps. I updated the org owner fields to `hyperslop-systems` as config intent, but the actual signing keys/tap names are org-specific and not verifiable without org secrets — flagged as a Phase 9 release-infra item, not a build/test gate.

### What was tricky to build

- The root package name: a Go module path with hyphens (`hyperslop-cli`) is not a valid package identifier. Confirmed `go-go-datadrop` uses `package go_go_datadrop`, so I used `package hyperslop_cli` for the root `logcopter_generate.go`.

### What warrants a second pair of eyes

- The GoReleaser brew tap (`hyperslop-systems/homebrew`) and fury (`push.fury.io/hyperslop-systems/`) are guesses for the new org; confirm the real tap/fury account before Phase 9 release.

### What should be done in the future

- Phase 9: confirm hyperslop-systems Homebrew tap + fury.io account + GPG signing key before tagging v0.1.0.

### Code review instructions

- `git show b97e018` in `hyperslop-cli`. Verify `go.mod` line 1 and `go run ./cmd/hyperslop` (builds, does nothing yet).

### Technical details

- Module: `github.com/hyperslop-systems/hyperslop-cli`; binary: `hyperslop`; root package: `hyperslop_cli`; logcopter area: `hyperslop-systems.hyperslop-cli`.
- Baseline `go-go-datadrop` tests green before any extraction: `cmd/datadrop`, `pkg/auth`, `pkg/server`, `pkg/cli`, `pkg/cli/{authcmd,dataset,drops}`, `pkg/client`, `pkg/datadrop`, `pkg/tabular`, `pkg/store`, `pkg/stream`, `pkg/blob`, `pkg/schema`, `pkg/webui`, `pkg/doc`, `deploy/compose`.

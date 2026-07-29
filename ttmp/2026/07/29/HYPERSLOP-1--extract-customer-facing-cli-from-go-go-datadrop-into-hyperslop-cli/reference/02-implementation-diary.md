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
RelatedFiles:
    - Path: repo://.github/workflows/release.yaml
      Note: Phase 9 tag workflow repaired before v0.1.0 retag (commit bb5ddc8)
    - Path: repo://README.md
      Note: Customer-facing real-server Hyperslop exercise (commit eadd9b6)
    - Path: repo://cmd/hyperslop/smoke_test.go
      Note: Real-server buffered partial-output regression (commit 8f230e1)
    - Path: repo://pkg/cli/dataset/get.go
      Note: Sixth-pass whole-version staged download transaction (commit c72f6e6)
    - Path: repo://pkg/cli/dataset/push.go
      Note: Literal colon-path resolution (commit c72f6e6)
    - Path: repo://pkg/cli/events/tail.go
      Note: Central follow execution-mode validation (commit 4abebf3)
    - Path: repo://pkg/cli/exit.go
      Note: Fourth-pass typed exit propagation and failure-path processor finalization (commit 8f230e1)
    - Path: repo://pkg/client/datasets.go
      Note: |-
        Stable cancellation-aware upload snapshots (commit 4abebf3)
        Reject negative import row limits before requests (commit c72f6e6)
    - Path: repo://pkg/jsondoc/jsondoc.go
      Note: Lossless arbitrary-JSON boundary (commit 4abebf3)
    - Path: repo://pkg/tabular/rows.go
      Note: Fourth-pass CSV truncation and strict NDJSON semantics (commit 8f230e1)
    - Path: ws://release-hyperslop-v0.1.0/go.mod
      Note: Standalone v0.1.0 pin prepared in Datadrop PR 10
ExternalSources: []
Summary: 'Step-by-step implementation diary for building the HYPERSLOP-1 extraction: moving the customer-facing CLI out of go-go-datadrop into hyperslop-cli. Records what changed, why, commands run, failures verbatim, and commit hashes per phase.'
LastUpdated: 2026-07-29T11:31:44.983890846-04:00
WhatFor: So the build can be reviewed, resumed, and audited against the design doc's phased plan.
WhenToUse: Read before resuming the HYPERSLOP-1 build or reviewing a phase's correctness.
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

## Step 1: Phase 1 — Move wire types + scope/role model + tabular into hyperslop-cli

Moved the shared wire-types layer (`pkg/datadrop`), the client-side scope/role
model, and the event→table projection (`pkg/tabular`) out of `go-go-datadrop`
into `hyperslop-cli`, and rewired `go-go-datadrop` to import them back. This is
the foundational phase: it establishes the one-way dependency
(`go-go-datadrop → hyperslop-cli`) and breaks the would-be import cycle.

### Prompt Context

**User prompt (verbatim):** (see Step 0)

**Commit (code):**
- hyperslop-cli `16b9535` — "Phase 1: add pkg/datadrop (wire types + folded scope/role model) and pkg/tabular"
- go-go-datadrop `938682d` — "Phase 1: move wire types + scope/role model + tabular to hyperslop-cli"

### What I did

- **hyperslop-cli/pkg/datadrop**: copied the wire-type files from go-go-datadrop; folded the shared scope/role model in (new `scope.go` + `role.go`) so the package is a pure leaf with no `pkg/auth` dependency. `account.go`/`device.go` dropped the `pkg/auth` import and use `Scope`/`Role` directly. Added `scope_test.go`/`role_test.go` (moved from `auth_test.go`). Regenerated the logcopter area string.
- **DR-2 decision — chose Option 2 (fold), not Option 1 (alias re-export).** Rationale recorded below in "What I learned". No alias/shim was added; `pkg/datadrop` is a true leaf.
- **hyperslop-cli/pkg/tabular**: copied, repointed its `pkg/datadrop` import to `hyperslop-cli`. Dropped `fixture_test.go` (the Go↔TS UI contract test) — it stayed in go-go-datadrop.
- **go-go-datadrop**: `git rm -r pkg/datadrop pkg/tabular`; `git rm pkg/auth/scope.go`; rewrote `pkg/auth/role.go` to keep only `DropACL`/`EffectiveRole`/`Authorize` (importing `datadrop.{Role,Scope}`); trimmed `ValidateDeviceScopes` out of `pkg/auth/device.go`; updated `pkg/auth/principal.go` to use `datadrop.{Scope,ScopeSet,ScopeDropsRead,NewScopeSet}`.
- **Mechanical rename** via `sed` across go-go-datadrop: repointed `pkg/datadrop` + `pkg/tabular` import paths to hyperslop-cli; renamed `auth.{Scope,Role,…}` (shared symbols only) → `datadrop.{…}`. Then `goimports -w` removed now-unused `pkg/auth` imports and added `pkg/datadrop` where needed.
- **auth_test.go**: moved the 6 scope/role unit tests to hyperslop-cli; kept the token + authz-matrix + label + context tests, pointing shared symbols at `datadrop.*` (server-only `auth.Principal`/`auth.DropACL`/`auth.EffectiveRole`/etc. unchanged).
- **pkg/tabularfixture** (new): the Go↔TS projection fixture contract test stayed in go-go-datadrop because the fixture file + TypeScript consumer live under `ui/`. It imports `hyperslop-cli/pkg/{tabular,datadrop}` and writes/reads `../../ui/test/fixtures/envelope-projection.json` (resolves from `pkg/tabularfixture/`).
- **go.mod**: `require github.com/hyperslop-systems/hyperslop-cli v0.0.0` + `replace … => ../hyperslop-cli`; `GOWORK=off go mod tidy`.

### Why

The user requirement that the admin CLI import the customer commands forces `go-go-datadrop → hyperslop-cli`. If the wire types stayed in go-go-datadrop, hyperslop-cli's client would depend back on go-go-datadrop → import cycle. So the wire types (and the scope/role model they embed, and `pkg/tabular` which depends on them) must live in hyperslop-cli.

### What worked

- Both modules build and the **full test suite is green**, including `pkg/auth` and `pkg/server` authz-matrix tests (proving the fold + rename preserved authorization behavior), the `cmd/datadrop` 24s smoke suite, and the new `pkg/tabularfixture` (proving the moved tabular projection is byte-identical to the fixture the TypeScript side expects).
- The no-import-cycle invariant holds: `go list -deps ./...` in hyperslop-cli contains no `github.com/go-go-golems/go-go-datadrop`.
- `goimports` cleanly removed every now-unused `pkg/auth` import (verified: no file imports `pkg/auth` without using an `auth.*` server-only symbol).

### What didn't work

- `go build ./...` from the workspace root fails with "directory prefix . does not contain modules" (root is not a module). Resolution: build per-module.
- After the rename, the `cmd/datadrop` smoke tests failed first with `go: updates to go.mod needed; to update it: go mod tidy` — the smoke tests build the binary with `GOWORK=off`, so `go.sum` needed hyperslop-cli's transitive deps. Fixed by `GOWORK=off go mod tidy`; smoke tests then passed.

### What I learned

- **DR-2: folding (Option 2) is cleaner than the alias (Option 1).** Folding gives one uniform rename pattern (`auth.X → datadrop.X`) that reuses the existing `datadrop` import everywhere, avoids a second `auth` package in go-go-datadrop, and leaves `pkg/datadrop` a pure leaf. The alias would have been a shim (disallowed by AGENT.md / the goal) and would require keeping a re-export file in sync. The exhaustive authz-matrix tests made the mechanical rename safe.
- The scope/role model's error messages still say `"auth: …"` (e.g. `"auth: a credential must carry at least one scope"`). I preserved them verbatim to avoid a behavior change; they now originate from the `datadrop` package, which is a minor cosmetic wart noted for a future cleanup (not a contract; no test checks the text).
- `pkg/tabular` is shared by the CLI row projection (`pkg/cli/rows.go`) AND the server `/table` endpoint — both now import it from hyperslop-cli, so the single-projection invariant (CLI and web workbench name the same columns) is preserved across repos.

### What was tricky to build

- Splitting `pkg/auth/role.go`: the `Role` *type model* (Role, constants, AtLeast, Valid, ParseRole) moved to datadrop, but the *decision* (DropACL, EffectiveRole, Authorize) stayed. The decision functions use `Role`/`Scope` as both parameter and return types, so they had to switch to `datadrop.Role`/`datadrop.Scope` while `Principal` (server-only) stayed local. Getting the import + type references right in one pass mattered; the authz tests confirmed it.
- The fixture test's home: it must run where it can write `ui/test/fixtures/` (go-go-datadrop), but the projection code moved to hyperslop-cli. Solved with a dedicated `pkg/tabularfixture` package that imports hyperslop-cli's tabular and keeps the same relative fixture path.

### What warrants a second pair of eyes

- The `sed` rename touched 92 files in go-go-datadrop. Spot-checks (authcmd/device.go, handlers_drops.go, serve.go) confirm imports are correct and no unused `pkg/auth` imports remain, but a `git show 938682d --stat` review of the server/store files is worthwhile.
- `pkg/auth/role.go` and `principal.go` now import `hyperslop-cli/pkg/datadrop`; confirm the server never calls `datadrop.ValidateDeviceScopes` (it shouldn't — that's client-side policy) and that no server-only auth symbol was accidentally renamed.

### What should be done in the future

- Consider rewording the moved scope/role error messages from `"auth: …"` to `"datadrop: …"` in a follow-up (behavior-compatible if no consumer matches the text; verify first).
- DR-2 Option 2 is now fully landed (no alias to remove later) — the design doc's "fold as cleanup follow-up" is done.

### Code review instructions

- `git show 16b9535` (hyperslop-cli) and `git show 938682d` (go-go-datadrop).
- Verify: `cd go-go-datadrop && go test ./...` green; `cd hyperslop-cli && go test ./...` green; `cd hyperslop-cli && go list -deps ./... | grep go-go-datadrop` empty.

### Technical details

- hyperslop-cli/pkg/datadrop exports (new): `Scope`, `ScopeSet`, `AllScopes`, `NewScopeSet`, `FullScopeSet`, `ParseScopes`, `ValidateScopes`, `ValidateDeviceScopes`, `Role`, `RoleNone/Reader/Writer/Admin`, `AssignableRoles`, `ParseRole` (+ existing wire types).
- go-go-datadrop/pkg/auth now exports only server-only: `Principal`, `Kind*`, `Anonymous`, `FromContext`, `WithPrincipal`, `TokenPrefix`, `Mint*`, `Hash*`, `Verify*`, `ParseToken`, `LooksLikeToken`, `New*ID/Value`, `Claims`, `Provider`, `Discover*`, `Exchange`, `AuthCodeURL`, `DropACL`, `EffectiveRole`, `Authorize`.

## Step 2: Phase 2 — Move pkg/client into hyperslop-cli + device-flow methods (DR-7)

Moved the typed HTTP client (`pkg/client`) to hyperslop-cli (pure customer spine:
depends only on `pkg/datadrop`, used only by the CLI). go-go-datadrop deleted its
`pkg/client` and repointed the `pkg/cli` importers to hyperslop-cli.

DR-7: added `StartDeviceAuthorization` and `PollDeviceToken` to `*client.Client`
(unauthenticated — `do()` omits `Authorization` when `Token==""`) so the
device-pairing flow goes through the client instead of the command's own raw
HTTP. Added a `RetryAfter time.Duration` field to `APIError`, parsed from the
`Retry-After` header in `apiErrorFrom`, so the device poll loop's `RateLimited`
handling has the server's guidance. The polling loop/wait-interval stays with the
command (moved + refactored in Phase 4).

- **Commits:** hyperslop-cli `a44a2d3`, go-go-datadrop `be94e25`.
- **What worked:** build + full test green (incl. 36s smoke suite); no cycle.
- **What I learned:** the Phase 1 sed had already repointed `pkg/client`'s
  `pkg/datadrop` import to hyperslop-cli, so the copy needed no import-path fix —
  only the logcopter area string.

## Step 3: Phase 3 — Move shared CLI foundation + parameterize AppName/ErrorPrefix (DR-3)

Moved the glazed CLI foundation to hyperslop-cli/pkg/cli: the client section
(`section.go`), the cobra builder (`build.go`: `BuildCobraCommand`/`AddCommands`/
`Registrar`/`Builder`), the exit-code contract (`exit.go`: `ExitOn`/`WithExitCodes`/
`ExitCodeFor` + the 0–5 codes + `ErrorPrefix`), the row projections (`rows.go`),
field helpers (`fields.go`), and `whoami.go`. Depends only on hyperslop-cli's own
`pkg/{client,datadrop,tabular}` + glazed.

**DR-3 parameterization:** `AppName` (glazed env prefix) and `ErrorPrefix`
(diagnostic prefix) were `const`-hardcoded to `datadrop`; they are now package
state set once per root via `SetAppName`/`SetErrorPrefix` (`hyperslop`→`HYPERSLOP_*`,
admin `datadrop`→`DATADROP_*`). `BuildCobraCommand` uses `AppName()`; the client
section help interpolates the env-var name from `AppName()` (`[$HYPERSLOP_ADDR]` vs
`[$DATADROP_ADDR]`); `ExitOn` uses `ErrorPrefix()`. `exitCodeFor` exported as
`ExitCodeFor` so the admin root reuses it. `exit_test.go` updated to `ErrorPrefix()`.

go-go-datadrop/pkg/cli now keeps only the operator surface: `root.go` (admin
root), `serve.go`, `healthcheck.go`, `serve_test.go`, `logcopter.go`, and a
trimmed `build.go` (`buildOperatorCommand`/`addOperatorCommands` using
`hypcli.WithExitCodes`). `root.go` imports `hypcli "hyperslop-cli/pkg/cli"`, sets
`SetAppName("datadrop")`/`SetErrorPrefix("datadrop: ")`, and uses
`hypcli.AddCommands`/`NewWhoamiCommand`/`Registrar`/`ExitCodeFor`/`ErrorPrefix`/
exit codes. The customer command groups repointed `ddcli` to
`hyperslop-cli/pkg/cli` (they move physically in Phase 4).

- **Commits:** hyperslop-cli `44a9976`, go-go-datadrop `f18caf1`.
- **What worked:** build + full test green (28s smoke suite); `DATADROP_*` env and
  `datadrop: ` prefix preserved on the admin binary (smoke tests use them); no
  cycle; exit + rows change-detector tests pass with parameterized `ErrorPrefix()`.
- **What didn't work:** first `section.go` edit was rejected atomically because the
  second edit in the same call didn't match exact whitespace (multi-line
  `fields.New(...)` trailing commas). Fixed by reading the exact text and doing the
  import + the two help-string edits separately.
- **What was tricky:** the operator `build.go`/`root.go` are in package `cli` and
  must import `hyperslop-cli/pkg/cli` (also package `cli`) — aliased as `hypcli`
  to avoid the self-name conflict. `Builder`/`Registrar` moved to hyperslop-cli, so
  `addOperatorCommands` now takes `...hypcli.Builder` and `Execute`/`NewRootCmd`
  take `...hypcli.Registrar` (compatible: both are `func` types over
  `*cobra.Command`/`cmds.Command`).
- **What warrants a second pair of eyes:** `root.go`'s `SetAppName`/`SetErrorPrefix`
  are package-level state set in `NewRootCmd`. Acceptable for a CLI (single
  goroutine before commands build), but if a test builds two roots in one process
  with different app names, the last setter wins. No such test today.

## Step 4: Phase 4 — Move command groups + customer help; refactor device flow (DR-7); rewire admin main

Moved the five customer command groups (authcmd, drops, events, dataset,
schemacmd) into hyperslop-cli/pkg/cli. Their imports were already repointed to
hyperslop-cli by earlier phases, so the move was a copy + `go generate`
(logcopter areas). DR-7: refactored authcmd/device.go onto the typed client
(`client.StartDeviceAuthorization`/`PollDeviceToken`); deleted its raw
`http.Client`/`postJSON`/`apiProblem`; the poll loop uses `*client.APIError.Code`
+ `APIError.RetryAfter`. The addr/help/messages interpolate `AppName()` so they
read `HYPERSLOP_*` for hyperslop and `DATADROP_*` when imported by the admin
binary. Rewrote device_test.go for the new `pollWithWait(ctx, *client.Client, …)`
signature (mock httptest server returning a problem document + Retry-After).

Customer help: hyperslop-cli/pkg/doc (embeds topics/) with `AddDocToHelpSystem`,
the moved `06-cli-output.md`, and a new `02-getting-a-token.md` (agent
onboarding for the device flow).

go-go-datadrop: deleted the five groups + `06-cli-output.md`; `cmd/datadrop/main.go`
imports the registrars from hyperslop-cli; the admin root loads BOTH help sets
(`doc` + `hapidoc`) so `datadrop help cli-output`/`getting-a-token` resolve (no
slug overlap). `cmd/datadrop/tree_test.go` repointed to hyperslop-cli groups.

- **Commits:** hyperslop-cli `8643a49`, go-go-datadrop `61b7a70`.
- **What worked:** both modules build + full test green (incl. datadrop smoke +
  tree tests); `hyperslop --help` lists the customer verbs and NOT
  serve/healthcheck; `datadrop --help` lists both.

## Step 5: Phase 5 — Wire the hyperslop main + customer-only root

hyperslop-cli/pkg/cli/root.go: `NewHyperslopRootCmd`/`Execute` build the
customer-only tree (whoami + five groups, customer help, logging with
`HYPERSLOP_LOG_LEVEL`), setting `SetAppName("hyperslop")`/`SetErrorPrefix("hyperslop: ")`.
cmd/hyperslop/main.go names the five registrars. Verified: `hyperslop help
cli-output`/`getting-a-token` resolve; `hyperslop whoami` against an unreachable
server prints `hyperslop: …` and exits 1; `whoami --help` shows
`[$HYPERSLOP_ADDR]`/`[$HYPERSLOP_TOKEN]`.

- **Commit:** hyperslop-cli `57727bb`. (Phase 6 — admin main/root rewire — was
  done together with Phase 4, since deleting the groups required it.)

## Step 6: Phase 7 — Lint + hygiene

golangci-lint (GOWORK=off) found two unused operator functions
(`buildOperatorCommand`/`addOperatorCommands`) left in hyperslop-cli/pkg/cli/build.go
from the copy — they are operator-only and already live in go-go-datadrop. Removed
them. `GOWORK=off go mod tidy` added missing go.sum entries (lumberjack via
glazed/logging) so hyperslop-cli builds/lints standalone, not just in the workspace.

- **Result:** golangci-lint 0 issues in BOTH modules; GOWORK=off go build/test green.
- **Commit:** hyperslop-cli `9c8b43a`.
- **What I learned:** the workspace masked an incomplete go.sum — `GOWORK=off` is
  the honest standalone check, and the smoke tests/lint need it.

## Step 7: Phase 8 — hyperslop e2e smoke test + no-cycle CI guard

cmd/hyperslop/smoke_test.go: builds the REAL datadrop server (from the workspace
go-go-datadrop) + the hyperslop client, starts it with a mock OIDC discovery
provider, and drives hyperslop over a real socket. Three tests, all PASS:
whoami (exit 0, authenticated=false), auth device (prints URL+code, exits
non-zero on 1s timeout), exit-code contract (exit 1 + `hyperslop:` prefix on
unreachable server). The server-dependent tests skip gracefully under GOWORK=off
(standalone CI); the exit-code test always runs. The authenticated data verbs
are covered by go-go-datadrop's cmd/datadrop smoke tests (same hyperslop-cli
command code, same server).

.github/workflows/push.yml: added a no-import-cycle guard step that fails CI if
hyperslop-cli ever depends on go-go-golems/go-go-datadrop. Also fixed an
untracked generated pkg/doc/logcopter.go (logcopter-check now passes).

- **Commit:** hyperslop-cli `689a3cb`.
- **What was tricky:** hyperslop-cli cannot import go-go-datadrop/pkg/store to
  seed a token (that would be a cycle), so the hyperslop smoke test cannot mint
  a token via the DB. Resolution: verify the authenticated verbs through the
  datadrop smoke tests (which use the same command code), and verify the
  hyperslop binary's own wiring (whoami/device/exit-codes) against the real
  server without a token.

## Step 8: Phase 9 — Release (locally verified; publish BLOCKED on remote access)

**Locally verified:** `goreleaser build --snapshot --clean --single-target` (GOWORK=off)
succeeds and produces a working `hyperslop` binary (6s build; `--help` shows the
customer surface). So the release mechanism is sound.

`goreleaser check` exits 2 because the scaffold's `.goreleaser.yaml` uses
properties deprecated in goreleaser v2.13+ (`snapshot.name_template`,
`brews`→`homebrew_casks`). This is a pre-existing template issue, not introduced
by the extraction; it needs a config cleanup before a real release but does not
block building the binary.

**BLOCKED (cannot complete autonomously):** the actual publish requires
1. pushing hyperslop-cli (the `task/split-cli` work + main) to
   `git@github.com:hyperslop-systems/hyperslop-cli` — needs the remote repo to
   exist and push access I do not have authorization to use autonomously;
2. tagging `v0.1.0` and pushing the tag, which triggers the release workflow
   (needs org secrets: `GORELEASER_KEY`, GPG signing key, `HOMEBREW_TAP_TOKEN`,
   `FURY_TOKEN`) — and the hyperslop-systems Homebrew tap + fury account I put in
   `.goreleaser.yaml` (`hyperslop-systems/homebrew`, `push.fury.io/hyperslop-systems/`)
   are unconfirmed guesses;
3. waiting for `proxy.golang.org` to publish `hyperslop-cli@v0.1.0`;
4. then in go-go-datadrop: `go get github.com/hyperslop-systems/hyperslop-cli@v0.1.0`,
   remove the `replace … => ../hyperslop-cli`, `GOWORK=off go mod tidy`, commit.

Until then, go-go-datadrop builds GOWORK=off via the local `replace … =>
../hyperslop-cli` (the dev state), which is fully green. The extraction itself
(the "build the ticket" code work) is complete and verified; only the downstream
publish is blocked on access/authorization.

**What unblocks Phase 9:** user confirms I may push to the hyperslop-systems
remote (and the repo exists there); the org release secrets + the correct
Homebrew tap/fury account are configured; then the tag push + the go-go-datadrop
go.mod bump can run.

## Step 9: Phase 8 (revisited) — full hyperslop authenticated e2e

Extended cmd/hyperslop/smoke_test.go to run the WHOLE README quick start through
the hyperslop binary against the real datadrop server, not just whoami/device.
A tiny seeder main is compiled with `go run` against the split-cli workspace so
it can import go-go-datadrop/pkg/store (which hyperslop-cli must never import) to
mint a ddp_ token before the server opens the DB. Tests, all PASS:
- TestHyperslopFullDataPathAgainstRealServer: create, push (kv + stdin json +
  stdin ndjson => 4 events), query (4 events, data.temperature is a number),
  tail, export csv, schema put(strict)/show, dataset push(2 files)/list/get,
  whoami (authenticated=true).
- TestHyperslopExitCodeContract: exit 3 (bad credentials), 4 (unknown drop),
  5 (strict schema rejection) via hyperslop.

This satisfies the goal's literal e2e: hyperslop against a real datadrop server
for auth device -> create -> push -> query -> tail -> export -> dataset push/get
-> schema put/show -> whoami, plus the 0/1/3/4/5 exit codes. Server-dependent
tests skip under GOWORK=off (standalone CI); the unreachable-server exit-code
test always runs.

- **Commit:** hyperslop-cli `dbb39e3`.
- **What didn't work (first try):** `--format jsonl --output-fields authenticated`
  emits one JSON object per line, but decodeOneRow expected a JSON array; the
  full data path itself passed (server log showed drop created, schema
  registered, dataset committed) and only the final whoami decode failed. Fixed
  by switching those whoami checks to `--format json` (array of one row).
- **What was tricky:** naming — the test helper `cli(...)` collided with the
  `cli` package imported by main.go (same `package main`); renamed to `runCLI`.

## Step 10: Take over PR #1 and remediate the code review

Audited PR #1 from its actual GitHub head, read every inline review thread, and
reviewed the work as a takeover rather than treating the comments as a patch
queue. The extraction architecture is sound and unusually well documented, but
the inherited implementation had a happy-path bias: clean SSE EOF, failed
forced downloads, dotted-key collisions, one-time credential ordering, HTTP
status-only information, and overflow/fail-closed cases lacked adversarial tests.

Fixed all 16 findings with regression coverage, then found and fixed three
additional release-readiness issues: Dependency Review was unable to run because
the repository dependency graph was disabled; the template Makefile still said
`v0.1.14`; and GoReleaser used deprecated snapshot/Homebrew fields. The complete
assessment and finding-by-finding disposition is in
`code-review/01-pr-1-takeover-review.md`.

### Prompt Context

**User prompt (verbatim):** "Address code review issues at https://github.com/hyperslop-systems/hyperslop-cli/pull/1 , in fact take the time to take a step back and assess the work our colleague has been doing, as we are taking over."

**Assistant interpretation:** Take ownership of PR #1, independently assess the inherited implementation and its review feedback, fix every valid issue with tests, and leave an evidence-backed takeover record rather than merely applying suggested lines.

**Inferred user intent:** Make PR #1 safe and merge-ready while ensuring the new owner understands the architectural quality, weaknesses, and remaining release risks.

**Commit (code):** `1871472` — "fix(HYPERSLOP-1): address PR 1 correctness and safety review"

**Commit (companion):** go-go-datadrop `7647177` — "fix(HYPERSLOP-1): keep admin CLI aligned with PR review fixes"

### What I did

- Pulled PR metadata, checks, commit list, all 16 review comments, and GraphQL
  thread state; confirmed the local head exactly matched reviewed SHA `9c13c625`.
- Validated every finding against the implementation and server wire behavior.
- Made downloads transactional under `--force`; added single-file manifest hash
  verification using the resolved immutable version.
- Reconnected `tail --follow` after clean EOF with bounded backoff and cursor
  preservation; moved tail order normalization before cursor validation.
- Escaped literal dot/backslash path segments during flattening.
- Preserved append duplicate status (HTTP 200 vs 201), delete response bodies,
  total warning counts, and parameterized media types.
- Restored Cobra usage exit code 2 in both roots and tested both real binaries.
- Made role comparison fail closed and rejected duration multiplication overflow.
- Preflighted credential parents before device authorization and made token writes atomic.
- Parameterized all shared command examples with `{{app}}`; corrected the embedded row reference.
- Replaced broken `make install` logic with `go install` and verified a fresh `GOBIN`.
- Enabled GitHub Dependency Graph through the API; `/dependency-graph/sbom` now succeeds.
- Migrated deprecated GoReleaser fields and aligned the planned first version to `v0.1.0`.
- Ran full standalone and workspace tests, lint, vet, gofmt, logcopter, no-cycle,
  GoReleaser check/build, fresh install, and the complete real-server smoke path.

### Why

- The three P1 findings could silently lose events or user data.
- The P2 findings broke documented scripting, authorization, integrity, help, or
  audit contracts and therefore were not cosmetic cleanup.
- A takeover needs a durable assessment of why the work is good or risky, not
  only a green patch, so future merge/release decisions do not repeat discovery.

### What worked

- All 16 findings were reproducible from code inspection and resolved without
  changing the HTTP protocol or adding compatibility adapters.
- Targeted tests passed after each logical group.
- Final hyperslop `GOWORK=off go test ./... -count=1`: pass.
- Final go-go-datadrop `GOWORK=off go test ./... -count=1`: pass, including the
  53-second binary smoke and server/authz tests.
- Workspace `go test ./cmd/hyperslop -run TestHyperslop -count=1 -v`: all pass
  in 48.648s, including full authenticated data path and exit 1/2/3/4/5.
- Both linters reported `0 issues`; vet and gofmt clean.
- `goreleaser check` reports one valid configuration; snapshot build succeeds.
- Repository SBOM endpoint succeeds after enabling Dependency Graph.

### What didn't work

- The first companion server test run failed because the newly decoded response
  needed an `encoding/json` import. Exact failure:

  `pkg/server/handlers_datasets_test.go:547:12: undefined: json`

  `FAIL github.com/go-go-golems/go-go-datadrop/pkg/server [build failed]`

  Added the import, ran gofmt, and `TestDeleteVersion` passed.
- Initial PR Dependency Review failed before analyzing dependencies. Exact check error:

  `Dependency review is not supported on this repository. Please ensure that Dependency graph is enabled`

  Enabled Dependency Graph with the repository API and verified the SBOM endpoint;
  the check must rerun on the pushed head.
- Initial `goreleaser check` exited 2 because `snapshot.name_template` and `brews`
  were deprecated. Migrated to `snapshot.version_template` and
  `homebrew_casks`; validation and snapshot build now pass.
- The first go-go-datadrop push failed because Phase 4 had accidentally committed
  the local binary. Exact remote error:

  `remote: error: File datadrop is 119.88 MB; this exceeds GitHub's file size limit of 100.00 MB`

  A deletion commit would not remove the blob from pushed history. Preserved
  `backup/task-split-cli-with-binary`, reconstructed the two affected unpushed
  commits without the blob, added `/datadrop` to `.gitignore`, retested, and
  pushed clean commits `61b7a70` and `7647177`.

### What I learned

- The colleague made strong macro-level decisions: one-way dependency, no command
  duplication, no auth aliases, contract tests, phase commits, and excellent diary
  continuity. Those choices made takeover safe.
- The review gap was micro-level failure ordering and information preservation.
  Abstractions that decode JSON can accidentally discard status/body semantics;
  filesystem code must publish only after successful transfer and verification.
- Security workflows can fail because repository capabilities are disabled; do
  not weaken CI before checking repository settings.
- Shared code needs shared prose parameterization too: dynamic env/error prefixes
  were insufficient while help examples still hard-coded one binary.
- Passing tests do not prove a branch is publishable: inspect newly introduced
  Git objects and ignore root build artifacts before the first remote push.

### What was tricky to build

- Single-file verification had a TOCTOU edge: fetching `latest` and then fetching
  bytes from `latest` could cross a new commit. The exact solution was to fetch
  the manifest, take its numeric version and digest, then request bytes from that
  numeric version and publish only after hashing.
- Safe `--force` handling had to preserve no-clobber behavior too. Forced writes
  use temp+fsync+rename; non-forced writes use temp+hard-link so a destination
  created during transfer still cannot be overwritten.
- SSE reconnect must not retry permanent 401/404 forever. Typed `*client.APIError`
  returns immediately; clean EOF and transport failures back off from 1s to 30s.
- Flattened paths needed an injective encoding. Escaping backslash first and dot
  second distinguishes literal `a.b`, nested `a -> b`, and literal backslashes.
- Command help lives in both binaries. A deliberate `{{app}}` renderer avoided
  accidental replacement of product prose while keeping copy/paste examples correct.
- Removing a >100 MB file in a later commit cannot satisfy GitHub because the
  object remains reachable. Since the branch was unpushed, the safe solution was
  a backup ref plus reconstruction from the parent of Phase 4, not blind history filtering.

### What warrants a second pair of eyes

- Review `pkg/cli/dataset/get.go` publication ordering and Unix rename/link
  semantics; Windows builds remain disabled in GoReleaser.
- Review `pkg/cli/events/tail.go` retry policy and whether operators want a
  reconnect diagnostic on repeated clean EOFs.
- Confirm `homebrew_casks` is the intended packaging migration and the actual
  `hyperslop-systems/homebrew` tap exists before tagging.
- Package-level `AppName`/`ErrorPrefix` state is correct for one CLI root per
  process but is not designed for concurrently constructing both roots.

### What should be done in the future

- Both coordinated branches are pushed. Wait for all checks on the new head,
  resolve the 16 threads, and request a fresh Codex review.
- Merge/release sequencing remains Phase 9: merge first, confirm external package
  destinations/secrets, tag hyperslop-cli, then replace go-go-datadrop's local
  module replacement with the published version.

### Code review instructions

- Start with `ttmp/.../code-review/01-pr-1-takeover-review.md` for the complete map.
- Safety paths: `pkg/cli/dataset/get.go`, `pkg/cli/events/tail.go`,
  `pkg/tabular/flatten.go`, and their new tests.
- Wire/contract paths: `pkg/client/{client,datasets}.go`, `pkg/cli/{root,rows}.go`,
  `pkg/datadrop/{account,role,dataset}.go`.
- Validate with:
  - `cd hyperslop-cli && GOWORK=off go test ./... -count=1 && GOWORK=off golangci-lint run ./...`
  - `cd go-go-datadrop && GOWORK=off go test ./... -count=1 && GOWORK=off golangci-lint run ./...`
  - `cd hyperslop-cli && go test ./cmd/hyperslop -run TestHyperslop -count=1 -v`
  - `GOWORK=off goreleaser check && GOWORK=off goreleaser build --snapshot --clean --single-target`

### Technical details

- PR review head: `9c13c6253f2f9de7ffa5507a6a08bcc9f8ccc425`.
- Review threads: 16 open before remediation (3 P1, 13 P2).
- Fix heads: hyperslop-cli `1871472`; go-go-datadrop `7647177`.
- Rewritten clean go-go-datadrop Phase 4: `61b7a70`; backup of the unpushed
  binary-bearing history: `backup/task-split-cli-with-binary`.
- GitHub setting changed: Dependency Graph enabled; no security check was skipped.
- No HTTP route or JSON field was removed. New `DeleteDatasetVersionResult` types
  the JSON body the server already emitted.

## Step 11: Fresh PR review and second-pass edge-case remediation

After the first 16 threads were fixed, pushed, green, replied to, and resolved,
requested a fresh Codex review instead of assuming the takeover patch was
complete. The connector accepted the request immediately but needed roughly 11
minutes to review the 115-file PR. It returned seven additional valid findings:
two P1 data-integrity issues and five P2 consistency/validation issues.

Addressed all seven with focused tests, then reran the complete standalone and
workspace validation in both repositories. This second pass confirmed the value
of iterative independent review: the first collision fix correctly escaped dots
and backslashes but still lacked an explicit representation for an empty JSON
key segment, and the single-file version pin exposed the same alias race in the
whole-version loop.

### Prompt Context

**User prompt (verbatim):** (same as Step 10)

**Assistant interpretation:** Continue owning PR #1 through independent re-review, not merely the original comment list; fix any newly discovered correctness issues and preserve takeover evidence.

**Inferred user intent:** Reach a genuinely review-ready state through iterative assessment and validation, with no known review findings left deferred.

**Commit (code):** `a6c755a` — "fix(HYPERSLOP-1): address second-pass PR review edge cases"

### What I did

- Waited for a fresh Codex review on head `50407db`; review `4811526324` produced
  seven new threads.
- Encoded empty JSON key segments as `\\0`, keeping literal `\\0` distinct via
  normal backslash escaping.
- Escaped CSV leading headers and schema property keys through the same encoder
  used by row values, while retaining original headers for pre-flatten CSV text typing.
- Pinned all whole-version file requests to `found.Version`, not the mutable
  `latest` alias.
- Rejected `tail --follow` with `--after`, `--before`, `--from`, or `--to`, because
  the sequence-only SSE API cannot preserve those predicates.
- Made `export --output-file` transactional with temp+copy+fsync+close+rename.
- Reinitialized a null manifest map before applying title/license/description overrides.
- Rejected `.` as a dataset logical path.
- Added regression tests for each case and updated tail/output help.
- Reran full tests/lint/vet/gofmt in both repos, real-server smoke, no-cycle,
  logcopter, and GoReleaser validation.

### Why

- Empty-key and CSV projection mismatches could still silently lose or duplicate data.
- Mutable `latest` resolution could mix immutable dataset versions.
- Follow filters that disappear after the initial page violate the command the user asked for.
- In-place export truncation destroys known-good output on network failure.
- Null manifest panic and root path acceptance are preventable input-boundary failures.

### What worked

- All targeted tests passed immediately after the respective fixes.
- `GOWORK=off go test ./... -count=1` passed in hyperslop-cli.
- `GOWORK=off go test ./... -count=1` passed in go-go-datadrop, including the
  84.173-second admin binary package and shared tabular fixture.
- Real-server hyperslop suite passed in 77.906 seconds.
- Both linters reported `0 issues`; vet, gofmt, no-cycle, logcopter, and
  `goreleaser check` passed.

### What didn't work

- The first multi-edit attempt against `pkg/tabular/tabular_test.go` used spacing
  that did not exactly match gofmt's aligned map literal. Exact tool failure:

  `Could not find edits[1] in .../pkg/tabular/tabular_test.go. The oldText must match exactly including all whitespace and newlines.`

  Read the exact block and replaced the whole collision test plus inserted the
  CSV test in a second, non-overlapping edit. No code or test failure remained.

### What I learned

- An escaped separator is not yet an injective path encoding unless the empty
  segment also has a visible representation.
- Schema metadata, header order, and row values must all pass through the same
  name transform; fixing only values creates ghost columns.
- Once an alias is resolved to immutable metadata, every related byte request
  must use that same immutable identifier.
- Some flag combinations cannot be faithfully emulated client-side. An explicit
  rejection is safer than silently applying constraints only to an initial page.

### What was tricky to build

- Empty-segment encoding had to avoid colliding with a literal marker. Returning
  `\\0` for empty while escaping a literal backslash first maps literal `\\0` to
  `\\\\0`, preserving injectivity.
- CSV schema typing runs before flattening to preserve `001`, while builder
  metadata lookup runs after flattening. The solution keeps original names in
  `TextColumns` but escapes the builder's headers and property keys.
- Whole-version pinning needed to apply even under `--no-verify`; otherwise a
  command could silently assemble files from multiple versions rather than fail a hash.
- Follow time bounds cannot safely terminate on producer `time`, which need not
  be monotonic by sequence. Rejecting unsupported combinations is the honest API.

### What warrants a second pair of eyes

- Confirm `\\0` is an acceptable public column encoding and that downstream UI
  field selectors treat backslashes literally.
- Review the decision to reject rather than filter follow/range combinations.
- Review atomic export mode behavior: temporary files are owner-only by default,
  safer than the old `os.Create` behavior but potentially a visible mode change.

### What should be done in the future

- Push `a6c755a` plus this diary update, wait for all checks, reply to and resolve
  the seven new threads, then request a final review on the resulting head.
- Phase 9 release sequencing remains unchanged.

### Code review instructions

- Start with fresh-review rows 17-23 in the takeover code-review document.
- Projection: `pkg/tabular/{flatten,project}.go` and the collision/CSV tests.
- I/O: `pkg/cli/dataset/get.go`, `pkg/cli/events/export.go`, and their failure tests.
- Validation: `pkg/cli/events/tail.go`, `pkg/cli/dataset/push.go`,
  `pkg/datadrop/dataset.go`.
- Validate with the same full command matrix listed in Step 10.

### Technical details

- Fresh review ID: `4811526324`; reviewed head `50407db3329eb6e7f11a8d22da88e58083ce2ea2`.
- New comments: `3676929577`, `3676929581`, `3676929589`, `3676929593`,
  `3676929598`, `3676929599`, `3676929601`.
- Findings: 2 P1, 5 P2; none deferred.

## Step 12: Third PR review — stream, bounded-read, archive, and integration hardening

Requested another fresh review after second-pass fixes were green and resolved.
Review `4811762131` on head `8653727` found seven more valid issues: retry
classification for established SSE reads, logical-path media types, bounded
reader behavior, non-regular upload inputs, cancellation semantics, archive
verification, and overly broad integration-test skips.

Implemented all seven and reran the complete validation matrix. This pass moved
beyond extraction-specific bugs into general reliability properties: finite work
must fail on cancellation, sampling must not parse data outside its budget, and
integration tests must distinguish unavailable dependencies from broken ones.

### Prompt Context

**User prompt (verbatim):** (same as Step 10)

**Assistant interpretation:** Continue iterative takeover review until no known findings remain; treat new valid findings as part of making PR #1 genuinely ready.

**Inferred user intent:** Obtain a deeply reviewed, failure-safe CLI rather than stopping after the first automated review turns green.

**Commit (code):** `2114ac6` — "fix(HYPERSLOP-1): address third-pass PR review findings"

**Commit (CI follow-up):** `0e60966` — "fix(dataset): bound archive verification reads"

### What I did

- Added typed `client.StreamReadError`; follow reconnects on read/transport failure
  while malformed JSON remains fatal.
- Made upload and mount Content-Type derive from the logical dataset path.
- Reworked CSV/NDJSON/JSON row caps to detect extra input without decoding the
  out-of-budget record.
- Rejected non-regular push inputs while allowing symlinks to regular files.
- Removed global cancellation-as-success; follow retains its local success path.
- Added verified archive streaming: pin numeric version, parse tar, verify every
  manifest file's membership/type/size/SHA-256, and publish atomically.
- Split companion availability detection from build/seeder execution so only a
  genuinely absent module skips integration tests.
- Added tests for every finding and ran the full validation matrix.

### Why

- Long-lived SSE transports commonly fail with read errors, not clean EOF.
- Dataset metadata must not depend on deduplication state.
- A row budget is a resource boundary, not merely an output truncation step.
- Special files can block forever or change between hash and upload.
- Interrupted finite output is incomplete and must not exit 0.
- `--archive` promised verification but previously made `--no-verify` meaningless.
- Broad skips can make a broken companion integration appear green.

### What worked

- Targeted parser/follow, media type, row cap, input type, exit, archive, and
  standalone/workspace smoke tests all passed.
- hyperslop-cli full standalone suite and lint/vet/gofmt passed.
- go-go-datadrop full standalone suite passed, including 72.264s binary package;
  lint/vet/gofmt passed.
- Real-server hyperslop suite passed in 71.309s.
- no-cycle, logcopter, and GoReleaser checks passed.

### What didn't work

- After removing the cancellation special case, I also removed the `context`
  import from `exit.go`, overlooking that wrapper method signatures still use
  `context.Context`. Exact failure:

  `pkg/cli/exit.go:149:6: undefined: context`

  `pkg/cli/exit.go:161:6: undefined: context`

  `pkg/cli/exit.go:172:39: undefined: context`

  Restored the import and reran the targeted tests successfully.
- GitHub GoSec then found G110 at the two unbounded `io.Copy` calls reading tar
  entries. Exact result:

  `pkg/cli/dataset/get.go:494 - G110: Potential DoS vulnerability via decompression bomb`

  `pkg/cli/dataset/get.go:473 - G110: Potential DoS vulnerability via decompression bomb`

  The archive is uncompressed, but bounded reads are still the stronger design.
  Capped metadata at 16 MiB, required file header sizes to match the manifest,
  switched to `io.CopyN`, and reran the exact GoSec command: 0 issues.

### What I learned

- Retry policy needs typed error provenance; matching error text cannot reliably
  distinguish transport failures from malformed protocol frames.
- A limit must be enforced before decoding the next value. Exact-cap detection
  requires format-aware peeking, not one extra full decode.
- Integrity for a container format is entry-level, not a hash of an unpublished
  aggregate unless the server supplies such a hash.
- Skip conditions belong at dependency discovery; failures after discovery are regressions.

### What was tricky to build

- CSV uses an internal buffered reader. A physical-line-bounded source prevents
  inaccessible prefetch while still supporting quoted records spanning lines;
  NDJSON combines `decoder.Buffered()` with the underlying reader; JSON arrays
  can rely on `More()`.
- Archive verification had to preserve server tar bytes while inspecting entries.
  `io.TeeReader` feeds both the destination and `tar.Reader`; file bodies are
  hashed as the parser consumes them, with temp publication handled separately.
- Stream errors needed a type wrapping the original error so `errors.As` can
  drive retry without flattening the cause.

### What warrants a second pair of eyes

- Review `physicalLineReader` against unusual CSV records and performance on very
  long physical lines.
- Review archive policy for unexpected non-file entries and stdout behavior on a
  late verification failure (nonzero exit but already-streamed bytes).
- Review whether cancellation should eventually have a dedicated exit code; the
  current documented contract maps it to generic error 1.

### What should be done in the future

- Push `2114ac6` plus docs, wait for checks, resolve seven threads, and request
  another review. Continue until a review pass reports no suggestions.
- Phase 9 release remains after merge and external-secret/package confirmation.

### Code review instructions

- Streaming: `pkg/client/client.go`, `pkg/cli/events/tail.go` and tests.
- Bounded reads: `pkg/tabular/rows.go`, malformed-record cap tests.
- Archive: `pkg/cli/dataset/get.go`, `TestDownloadArchiveVerifiesEntriesBeforePublication`.
- Integration gating: `cmd/hyperslop/smoke_test.go`.
- Run the full matrix from Step 10.

### Technical details

- Third review ID: `4811762131`; reviewed head `8653727301f068718483f9747863050d35bafc7a`.
- New comments: `3677123924`, `3677123929`, `3677123935`, `3677123941`,
  `3677123946`, `3677123955`, `3677123964`.
- Findings: 3 P1, 4 P2; none deferred.

## Step 12: Fourth Review Pass and Deferred Exit Redesign

A fourth automated review found six valid edge cases. I fixed all six rather than applying comments mechanically: the P1 exposed that the inherited `os.Exit` shim was designed for Glazed v1.3.8, while this repository uses v1.4.1 with Cobra `RunE`. The replacement returns typed coded errors and explicitly finalizes Glaze processors on failure, preserving successful buffered rows before the root prints and exits.

The remaining fixes made truncation and NDJSON semantics format-accurate, preserved typed distinct values, honored export cursor direction, and rejected explicitly empty schemas. Both repositories passed full tests, vet, lint, and formatting; a real-server binary test proves valid JSON containing the successful first push row is flushed before row two exits 5.

### Prompt Context

**User prompt (verbatim):** (same as Step 10)

**Assistant interpretation:** Independently validate and remediate every fourth-pass finding while continuing the takeover-level architectural assessment.

**Inferred user intent:** Deliver a PR whose edge-case behavior is trustworthy, not merely one with review threads closed.

**Commit (code):** `8f230e1` — "fix(cli): address fourth review pass edge cases"

**Commit (companion):** `2703a73` — "fix(cli): handle propagated coded command errors"

### What I did
- Made CSV cap detection distinguish ignored empty lines from whitespace records.
- Enforced exactly one JSON document per nonblank NDJSON physical line.
- Added type-discriminated distinct keys.
- Kept export default ascending while honoring explicit descending/before cursors.
- Rejected zero-length explicit schema input.
- Replaced immediate process exit with coded errors plus failure-path processor close.
- Added unit and real-server regressions for all six findings.

### Why
- Bounded sampling, provenance, cardinality, cursor flags, schema intent, and partial-write reporting are user-visible contracts.
- `os.Exit` inside a command bypasses formatter cleanup and can make completed writes invisible, encouraging duplicate retries.

### What worked
- `GOWORK=off go test ./...`, workspace `go test ./...`, `go vet ./...`, `golangci-lint run ./...`, and gofmt passed in hyperslop-cli.
- Full tests, vet, lint, and gofmt passed in go-go-datadrop.
- `TestHyperslopExitCodeContract` passed all 3/4/5 mappings and buffered partial-output JSON finalization against the real server.

### What didn't work
- An initial complete rewrite tool call for `exit_test.go` exceeded the tool argument limit and was not executed. I reissued a smaller complete file and then ran targeted tests successfully.

### What I learned
- Glazed v1.4.1 already propagates errors through `RunE`; only processor finalization and stable code annotation belong in the wrapper.
- Whitespace is format-specific: CSV spaces are data, while blank NDJSON whitespace is not a record.

### What was tricky to build
- On command failure Glazed's outer runner returns before closing its processor. The wrapper must close exactly once on failure, using `context.WithoutCancel`, while leaving success closure to Glazed.
- Export needed an ascending default without overwriting a user's explicit order; a parameterized shared field builder preserves both.

### What warrants a second pair of eyes
- Review the single-close invariant in `exitCodeGlazeCommand` against future Glazed runner changes.
- Confirm product expectations that export defaults ascending but supports explicit descending traversal.

### What should be done in the future
- Push both commits, wait for checks, reply to and resolve the six threads, and request another review.
- Phase 9 release remains externally gated.

### Code review instructions
- Start with `pkg/cli/exit.go` and `cmd/hyperslop/smoke_test.go` for the P1.
- Review `pkg/tabular/rows.go`, `pkg/tabular/infer.go`, `pkg/cli/events/export.go`, and `pkg/cli/dataset/push.go` for the P2 fixes.
- Validate with the full two-repository matrix and `go test ./cmd/hyperslop -run TestHyperslopExitCodeContract -count=1 -v`.

### Technical details
- Fourth review head: `fbc09f0`; six comments `3677354459`, `3677354467`, `3677354473`, `3677354481`, `3677354487`, `3677354494`.
- Findings: 1 P1, 5 P2; all accepted and fixed.

## Step 13: Replace Fifth-Wave Patches with Boundary Invariants

The fifth review's seven findings shared systemic causes: lossy arbitrary-JSON decoding, incomplete execution-mode validation, and hashing a mutable path rather than captured bytes. I introduced one lossless JSON boundary, centralized push/follow invariants, and bound dataset cache/upload decisions to private stable snapshots.

I also assessed protobuf for the TypeScript UI. It can eliminate manual DTO drift in a versioned follow-up, but protobuf `Struct` is itself float64-based and canonical proto JSON would change the v1 wire contract. For this PR, lossless raw JSON plus existing typed wire structs is the correct boundary.

### Prompt Context

**User prompt (verbatim):** "OMG still so many review issues. Is this something that maybe needs to be addressed at a more systemic level? https://github.com/hyperslop-systems/hyperslop-cli/pull/1"

**Follow-up prompt (verbatim):** "will the protobuf help us wrt to the UI, or are we just overengineering it? also, does the new json package for golang solve the issue / allow us to solve the issue?"

**Assistant interpretation:** Fix the seven findings by removing their common failure modes, and decide whether protobuf belongs in this remediation.

**Inferred user intent:** Stop repeated review whack-a-mole and establish enforceable data/input boundaries without overengineering the extraction.

**Commit (code):** `4abebf3` — "fix(cli): enforce systemic input and JSON invariants"

### What I did
- Added `pkg/jsondoc` with `Decoder.UseNumber` and exactly-one-document decoding; audited every dynamic production decode.
- Routed payload fields, manifests, projections, flattening, and manifest probes through the lossless boundary.
- Centralized push source and tail follow/formatter validation.
- Snapshot/hash dataset inputs into private temporary files before cache lookup or upload; made copying cancellation-aware.
- Rejected negative GC ages before any request and added adversarial regressions for all seven findings.

### Why
- Shared boundaries prevent the same defect from recurring at the next call site.
- Protobuf `Struct` would not preserve arbitrary JSON numbers, and protojson would break current v1 JSON semantics.

### What worked
- Full tests, vet, lint, and gofmt passed in both repositories; hyperslop's workspace suite included real-server smoke.
- A direct binary check rejected `tail --follow --format table` before network access.
- Source-mutation and exact-number regressions pass.

### What didn't work
- Initial exact-JSON tests compared object byte order and failed when `encoding/json` sorted keys: `round trip = {"decimal":0.123456789012345678901,"integer":9007199254740993}, want {"integer":9007199254740993,"decimal":0.123456789012345678901}`. Changed them to assert exact numeric lexemes.
- The first tail test used a loop variable named `settings`, shadowing the Glazed package: `settings.OutputJSONL undefined (type rangeSettings has no field or method OutputJSONL)`. Renamed the variable.
- `nonamedreturns` rejected the first snapshot cleanup signature; replaced the named error with an explicit keep flag.

### What I learned
- Cross-language schema generation helps known DTOs, not opaque JSON documents.
- Stable content addressing requires digest and transfer to reference captured bytes, not a pathname.

### What was tricky to build
- Snapshot cleanup must cover copy/sync/close/network failures without retaining large temporary files; each file is removed by the per-file operation.
- Follow validation needs the structured-output section as well as default command settings.

### What warrants a second pair of eyes
- Review temporary-disk capacity expectations for very large uploads.
- Decide separately whether v1 should generate TS types from OpenAPI/JSON Schema or a future v2 should adopt protobuf/Buf.

### What should be done in the future
- Push, resolve the seven threads, and request review; stop per user instruction.
- Keep protobuf out of this PR and open a separate versioned API/UI schema effort if desired.

### Code review instructions
- Start at `pkg/jsondoc/jsondoc.go`, `pkg/client/datasets.go`, `pkg/cli/drops/push.go`, and `pkg/cli/events/tail.go`.
- Run both complete repository suites plus lint/vet/gofmt.

### Technical details
- Fifth review head: `9659686`; comments `3677701638`, `3677701646`, `3677701648`, `3677701650`, `3677701654`, `3677701661`, `3677701668`.
- Findings: 2 P1, 5 P2; all accepted and fixed systemically.

## Step 14: Sixth Review Pass — Dataset Transaction and Input Contracts

The sixth review found six valid contracts. The important one was whole-version `dataset get --force`: individual files were transactional, but the dataset was not. I changed it to download and verify every file into hidden sibling stages before replacing any target; force targets are backed up and restored if publication itself fails.

The remaining changes make side-effecting numeric bounds explicit, preserve literal colon-bearing paths, reject conflicting download modes, make the schema example extract the actual schema document, and count human-facing token labels by Unicode code point.

### Prompt Context

**User prompt (verbatim):** "ok, go ahead."

**Assistant interpretation:** Implement all six independently validated sixth-review findings with regressions.

**Inferred user intent:** Close genuine CLI correctness gaps while preserving the customer CLI’s existing wire and output contracts.

**Commit (code):** `c72f6e6` — "fix(cli): address sixth review input and download contracts"

### What I did
- Rejected negative import row limits before any HTTP request.
- Resolved existing local paths before interpreting `LOCAL:LOGICAL`, preserving literal colons.
- Staged and verified full-version downloads before a transactional publish/rollback phase.
- Rejected `--archive` with `--file`.
- Corrected schema round-trip help to JSONL plus `jq '.spec'` extraction.
- Used `utf8.RuneCountInString` for token-name limits.

### Why
- Side-effecting defaults must never hide invalid user input.
- A multi-file download needs a dataset-level transaction, not merely per-file atomicity.

### What worked
- Targeted tests and the full hyperslop-cli standalone suite, vet, lint, and gofmt passed.
- Full go-go-datadrop suite, vet, lint, gofmt, logcopter, and merged pbui shell TypeScript check passed.

### What didn't work
- The first transactional publisher used a named return for deferred rollback and lint rejected it exactly: `pkg/cli/dataset/get.go:351:1: named return "retErr" with type "error" found (nonamedreturns)`. Replaced it with an explicit `committed` flag and reran the complete hyperslop validation.
- Remote CI then failed its logcopter check because the fifth-pass new `pkg/jsondoc` package had no generated logger artifact. Exact failure: `logcopter-gen: generated file is not current: /home/runner/work/hyperslop-cli/hyperslop-cli/pkg/jsondoc/logcopter.go`. Ran `make logcopter-generate` and `make logcopter-check`; the generated file is committed with this documentation follow-up.

### What I learned
- A staged file is only a safe transaction boundary if publication can restore every pre-existing target after any later promotion failure.
- User-visible “characters” must not be implemented as UTF-8 bytes.

### What was tricky to build
- The batch publisher first moves all force destinations to same-directory hidden backups, then promotes staged payloads, and rolls back published files plus backups on error. The tests cover the more common late-transfer/digest failure before publication; the rollback protects the later promotion phase.

### What warrants a second pair of eyes
- Review the operational disk-space implication: staged downloads temporarily require a full additional version, and `--force` briefly retains old targets as backups.

### What should be done in the future
- Push this commit with docs, resolve six threads after CI, and request a new review.
- Phase 9 release remains externally gated.

### Code review instructions
- Start with `pkg/cli/dataset/get.go` and `TestDownloadVersionForceDoesNotMixVersionsOnLaterFailure`.
- Validate client input bounds, colon-path resolution, schema help, and Unicode token names with their colocated unit tests.

### Technical details
- Sixth review head: `98faa25`; comments `3677882000`, `3677882006`, `3677882009`, `3677882017`, `3677882021`, `3677882026`.
- Findings: 1 P1, 5 P2; all accepted and fixed.

## Step 15: Publish the Go Module and Add a Real-Server User Quickstart

With PR #1 merged into `upstream/main`, I replaced the scaffold README with a customer-facing Hyperslop guide. It installs the released CLI, starts the documented local Datadrop/ZITADEL stack, pairs by device authorization, and exercises drops, streaming, schemas, and datasets against the real service. The guide deliberately contains no administrative-binary or internal command-sharing discussion.

I validated the merged source with standalone tests, vet, lint, logcopter, a GoReleaser snapshot build, and the real-server Hyperslop smoke suite. I then tagged `v0.1.0`; `proxy.golang.org` resolves the immutable module to the tagged commit. The GitHub binary release workflow remains blocked because the repository does not have the required `GORELEASER_KEY` secret, so it produced no GitHub release or downloadable binary assets. The companion pinning change is prepared and independently verified in PR #10.

### Prompt Context

**User prompt (verbatim):** "we rebased on origin/main, so maybe we can now also publish our first version of this, and set up a README to actually exercise the CLI against the real server."

**Assistant interpretation:** Publish the first release from merged main, add a customer-facing real-server README exercise, and complete release pinning where external release infrastructure permits.

**Inferred user intent:** Make Hyperslop usable by an external customer rather than leaving the extraction as an internal branch artifact.

**Commit (README):** `eadd9b6` — "docs: add real-server Hyperslop quickstart"

**Commit (security CI):** `cb1d645` — "ci: scan pushed commit range for secrets"

**Commit (release workflow):** `bb5ddc8` — "ci: remove disabled docs publishing template from release"

**Tag:** `v0.1.0` — points to `bb5ddc8c7ecf82fb83aabb6a812764c415b2f4e6`

**Companion pin PR:** `go-go-golems/go-go-datadrop#10`, commit `58a824b`

### What I did
- Replaced the scaffold README with an install guide and an actual local server/device-pairing exercise.
- Verified `devctl`’s `dev-stack` plan starts real ZITADEL and a real Go server on port 8080.
- Ran standalone full tests, vet, lint, logcopter, GoReleaser check/snapshot, and the real-server hyperslop smoke suite.
- Fixed a secret-scanning workflow bug that incorrectly compared `main` to `HEAD` on a main push.
- Removed an unused disabled reusable workflow job that caused tag workflow startup failure.
- Created, pushed, and verified `v0.1.0` through `proxy.golang.org`.
- Updated a separate pin branch to require `hyperslop-cli v0.1.0`, removed the local replace, and passed complete standalone validation.

### Why
- The release README is a customer contract; it must use the real identity and service flow, not an unauthenticated fictional command.
- A public Go tag is sufficient for `go install` and module pinning, but it is not a complete binary distribution without a successful artifact release.

### What worked
- All checks on `bb5ddc8` main passed: CodeQL, Dependency Scanning, Secret Scanning, golangci-lint, and the Go pipeline.
- `GOPROXY=https://proxy.golang.org go list -m -json github.com/hyperslop-systems/hyperslop-cli@v0.1.0` resolved version, timestamp, checksum, and tag commit.
- The pin branch passed `GOWORK=off go build ./...`, `go test ./... -count=1`, `go vet ./...`, `golangci-lint run ./...`, and `make logcopter-check`.

### What didn't work
- The initial tag workflow failed before any job because GitHub Actions resolved a disabled reusable `publish-docs` template. The tag was deleted before publication artifacts existed, the template job was removed, main checks passed, and the tag was recreated at the fixed workflow commit.
- The recreated tag workflow reached GoReleaser but both Linux and Darwin jobs failed exactly with: `missing GoReleaser Pro key, please provide one with --key or using the GORELEASER_KEY environment variable`. The action log showed `GORELEASER_KEY:` empty. No GitHub release was created.

### What I learned
- GitHub Actions validates/resolves a reusable-workflow job even when its `if` guard is constant false; leaving an unavailable template reference can prevent all tag jobs from starting.
- The Go module proxy publishes from a valid semantic tag independently of GitHub release assets.

### What was tricky to build
- The initial source README was a template, not stale product documentation. The replacement had to distinguish safe customer instructions from internal architecture and make device authorization’s stdout/stderr behavior explicit so command substitution captures only the one-time token.
- Retagging is normally avoided. Here it was limited to a first tag whose initial workflow had startup-failed before GoReleaser or proxy verification; the final public tag points at the CI-validated release workflow revision.

### What warrants a second pair of eyes
- Provision repository/organization secret `GORELEASER_KEY`, then rerun the existing `v0.1.0` tag workflow or perform a consciously versioned binary-release remediation. Confirm GPG, Homebrew, and Fury secrets/destinations before retrying.
- Review and merge PR #10 before declaring Phase 9 completely finished.

### What should be done in the future
- Do not mark Phase 9 complete until `v0.1.0` has a successful GitHub binary release and PR #10 is merged.
- After release secrets are configured, rerun a tag release and verify the GitHub release assets, Homebrew publication, and Fury packages.

### Code review instructions
- Read `README.md` as a new customer; execute the dev-stack and device pairing path against a real local service.
- Review `.github/workflows/secret-scanning.yml` event-specific revision selection and `.github/workflows/release.yaml` for tag workflow dependencies.
- Review PR #10’s two-file module pin diff and its standalone validation.

### Technical details
- Main release commits: `eadd9b6`, `cb1d645`, `bb5ddc8`.
- Failed tag workflow: `30494503254`; failure reason: missing `GORELEASER_KEY`.
- Module checksum: `h1:PAcSlMw5HVkbHvfJtBNP0RNH0CeugQJi1xDrmp5iNpo=`.

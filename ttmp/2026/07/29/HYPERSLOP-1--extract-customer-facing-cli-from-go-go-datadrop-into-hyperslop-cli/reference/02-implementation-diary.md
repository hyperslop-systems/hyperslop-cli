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

---
Title: Investigation diary
Ticket: HYPERSLOP-1
Status: active
Topics:
    - cli
    - architecture
    - refactor
    - datadrop
    - hyperslop
DocType: reference
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: "Chronological record of the investigation that produced the HYPERSLOP-1 design doc: how the go-go-datadrop CLI was mapped, how the customer/admin boundary was found, and how the import-cycle constraint drove the architecture."
LastUpdated: 2026-07-29T11:31:44.983890846-04:00
WhatFor: "So the next person resuming this ticket can see what was examined, what was decided, and what is still open before they write any code."
WhenToUse: "Read before resuming HYPERSLOP-1 or before challenging any of its decision records."
---

# Investigation diary

## Goal

Record the evidence-gathering and reasoning that produced the HYPERSLOP-1 design
doc ("Extracting the customer-facing CLI into hyperslop-cli — analysis, design
and intern implementation guide"), so the architecture decisions can be audited
against the code as it stood on 2026-07-29.

## Step 1: Orient in the `split-cli` workspace and locate the source repo

The workspace at `/home/manuel/workspaces/2026-07-29/split-cli` is a WSM Go
workspace (`go.work`) linking three worktrees: `glazed` (the framework),
`go-go-datadrop` (the source — server + admin CLI), and `hyperslop-cli` (a fresh
go-go-golems `go-template` binary scaffold, module path still the placeholder
`github.com/go-go-golems/XXX`). The canonical source repo is
`/home/manuel/code/wesen/go-go-golems/go-go-datadrop`; `hyperslop-cli`'s git
remote is already `git@github.com:hyperslop-systems/hyperslop-cli`, so the
target module path is `github.com/hyperslop-systems/hyperslop-cli`.

### Prompt Context

**User prompt (verbatim):** "Create a new docmgr ticket to extract customer side facing CLI to interact with the datadrop / device auth, etc... out of go-datadrop into hyperslop-cli . That CLI will be given to agents to interact with the (proprietary) backend. Use glazed commands ofc. The admin CLI in go-datadrop can then import the customer-facing functionality to avoid duplication. SO it's mostly about moving stuff out of go-datadrop into a CLI. Create a detailed analysis / design / implementation guide that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable."

**Assistant interpretation:** Produce a docmgr ticket (HYPERSLOP-1) with an intern-grade analysis/design/implementation guide for extracting the customer-facing half of the `datadrop` CLI into a new `hyperslop-cli` binary, using glazed commands, with the admin CLI importing the customer functionality back. Store the docs in the ticket and upload to reMarkable.

**Inferred user intent:** A new `hyperslop` CLI is the agent/customer-facing client for the proprietary datadrop backend; it must be split out of the server repo so agents get a clean client binary, while the admin `datadrop` CLI reuses the same verbs to avoid drift. The deliverable is thorough onboarding documentation an intern can execute from.

### What I did

- Ran `docmgr status --summary-only`; found the workspace already initialised at `hyperslop-cli/ttmp` with zero tickets.
- Located `go-go-datadrop` via `find` under `~/code/wesen/go-go-golems`.
- Read `go.work`, `split-cli/AGENT.md`, `.wsm/wsm.json`, `hyperslop-cli/go.mod`, `hyperslop-cli/AGENT.md`, `hyperslop-cli/.goreleaser.yaml`, `Makefile`, `README.md`, and the `cmd/XXX` placeholder.

### Why

Establish the exact module paths, the workspace topology, and what the `hyperslop-cli` scaffold already provides (project plumbing) vs. what is placeholder (`cmd/XXX`, module `XXX`) before claiming anything about the target.

### What worked

- `go.work` confirmed the three-module layout and that local replaces are already wired for development.
- `.wsm/wsm.json` confirmed the real `hyperslop-cli` repo path (`hyperslop-systems/hyperslop-cli`) and that `pbui` is also in the workspace (out of scope).
- `hyperslop-cli/AGENT.md` confirmed the project uses glazed for CLIs and pnpm/React for web — the former is directly relevant.

### What didn't work

- N/A.

### What I learned

- The scaffold is a standard go-go-golems binary project (Makefile, lefthook, golangci-lint pin, GoReleaser, GitHub Actions) — the `go-go-golems-project-setup` conventions already apply; Phase 0 is just a rename, not scaffolding from scratch.
- The `glazed` framework is vendored as a worktree in the workspace, so the CLI is built against the same glazed version the server uses.

### What was tricky to build

- None yet (orientation only).

### What warrants a second pair of eyes

- The module-path rename in Phase 0 touches `go.mod`, `.goreleaser.yaml`, `Makefile`, `pkg/logcopter.go`'s area string, and `cmd/XXX`. Easy to miss one.

### What should be done in the future

- Add a WSM/CI check that the `hyperslop-cli` module path matches its git remote after rename.

### Code review instructions

- Start at `split-cli/go.work` and `hyperslop-cli/go.mod`; confirm `module github.com/hyperslop-systems/hyperslop-cli`.

### Technical details

- `go.work`: `go 1.25`, `use (./glazed ./go-go-datadrop ./hyperslop-cli)`.
- `go-go-datadrop` module: `github.com/go-go-golems/go-go-datadrop`, `go 1.26.1`.
- `hyperslop-cli` remote: `git@github.com:hyperslop-systems/hyperslop-cli`.

## Step 2: Map the command tree and the shared CLI foundation

Read `cmd/datadrop/main.go`, `pkg/cli/root.go`, `pkg/cli/build.go`,
`pkg/cli/section.go`, `pkg/cli/exit.go`, `pkg/cli/rows.go`, `pkg/cli/fields.go`,
`pkg/cli/whoami.go`, `pkg/cli/healthcheck.go`, and every group `root.go`
(`authcmd`, `drops`, `events`, `dataset`, `schemacmd`).

### What I did

- Confirmed `main.go` only names five `Registrar`s and calls `cli.Execute`.
- Confirmed the registrar pattern: each group's `Register(root)` calls `ddcli.AddCommands(root, NewXCommand, …)`; verbs are built with `ddcli.BuildCobraCommand` which applies `WithExitCodes` and a `CobraParserConfig{AppName: "datadrop"}`.
- Confirmed the client section (`datadrop-client`, `--addr`/`--token`, env `DATADROP_ADDR`/`DATADROP_TOKEN`), `ClientFrom(vals) → client.New`.
- Confirmed the exit-code contract (0/1/2/3/4/5), `ExitOn`, `WithExitCodes` wrapping Glaze/Writer/Bare commands, and the `ErrorPrefix = "datadrop: "` load-bearing fix for glazed's `cobra.CheckErr` (upstream issue #611).
- Confirmed row projections in `rows.go` are a public row API pinned by `rows_test.go`, and that `RowsForEnvelopes` delegates to `tabular.FromEvents`.
- Confirmed the operator commands (`serve`, `healthcheck`) are built with `buildOperatorCommand` (no `AppName`, so no `DATADROP_*` env) and use a local `envOr` for their specific env vars.

### Why

The shared `pkg/cli` foundation is the layer both customer and operator commands stand on, so it determines what moves, what stays, and what must be parameterized (app name, error prefix) for two binaries to share it.

### What worked

- The foundation is cleanly separable: `section/build/exit/rows/fields/whoami` are customer-facing shared helpers; `serve/healthcheck` are operator-only; `root.go` is binary-specific.
- `WithExitCodes`/`ExitOn`/exit codes are generic and shared by both audiences — they must remain available to the operator commands after the move.

### What didn't work

- N/A.

### What I learned

- `AppName = "datadrop"` is hardcoded and load-bearing (it switches on glazed's env source). Two binaries sharing the foundation need it parameterized (became DR-3).
- `ErrorPrefix = "datadrop: "` is also hardcoded and must become parameterized.
- The device command (`authcmd/device.go`) hardcodes `DATADROP_ADDR`/`DATADROP_TOKEN` in its own field defaults and `envOr`, bypassing the client section (it is a `BareCommand` with its own `addr` field) — so parameterizing `AppName` must reach it too.

### What was tricky to build

- Realising that `buildOperatorCommand` exists *because* `DATADROP_ADDR` means two different things (client "talk to" vs `serve` "bind"), and that this ambiguity is resolved by skipping the env source for operator commands. Preserving that isolation while sharing the foundation is a real constraint, captured in DR-3/DR-4.

### What warrants a second pair of eyes

- The parameterization of `AppName`/`ErrorPrefix` (DR-3): package-level mutable state set at root-assembly time is acceptable for a CLI but must be set before any command is built, and must not race. The `NewClientSection` help text must interpolate the env name from `AppName()`.

### What should be done in the future

- Consider threading app name as an explicit parameter through `AddCommands`/`BuildCobraCommand` (a `*CLI` struct) instead of package-level state, if more binaries share the foundation later.

### Code review instructions

- Compare `go-go-datadrop/pkg/cli/build.go` and `exit.go` against the §8.3 / §9.6 sketches in the design doc; confirm the parameterization preserves the `ShortHelpSections` and `WithExitCodes` behavior.

### Technical details

- Exit codes: `ExitOK=0, ExitError=1, ExitUsage=2, ExitAuth=3, ExitNotFound=4, ExitValidation=5`. `exitCodeFor` maps `*client.APIError.Status`: 401/403→3, 404→4, 400/409/413/422→5, else→1.
- `ClientSectionSlug = "datadrop-client"`; `--token` is `fields.TypeSecret` (redacted in `--print-parsed-fields` and `--help`).

## Step 3: Map the client, the wire types, and the `pkg/auth` dependency

Read `pkg/client/{client,me,datasets}.go`, `pkg/datadrop/{device,account}.go`,
`pkg/auth/{scope,role,principal,token,device,oidc}.go`, and grepped who imports
`pkg/datadrop`, `pkg/auth`, `pkg/client`, `pkg/tabular`, `pkg/stream`.

### What I did

- Confirmed `pkg/client` is a pure HTTP client depending only on `pkg/datadrop`, used only by `pkg/cli/*` — the cleanest package to move.
- Discovered `pkg/datadrop` **imports `pkg/auth`**: `auth.Scope` in `DeviceAuthorization`, `StartDeviceAuthorizationRequest`, `DeviceTokenResponse`, `APIToken`, `CreateTokenRequest`; `auth.Role` in `Member`, `SetMemberRequest`.
- Split `pkg/auth` into shared (scope model; Role type model; `ValidateDeviceScopes`) vs server-only (`Principal`, token minting/hashing, device-code crypto, OIDC, `EffectiveRole`/`Authorize`/`DropACL`).
- Confirmed `pkg/tabular` imports only `pkg/datadrop` and is used by both `pkg/cli/rows.go` and `pkg/server/handlers_{export,import,table}.go` — shared, moves with the wire types.
- Confirmed `pkg/stream` is used only by `pkg/server/server.go` — server-only, stays.
- Measured the `auth.Scope`/`auth.Role`/etc. usage: 27 files, ≈158 occurrences.

### Why

The dependency between the wire types and the scope/role model is the crux: it determines what must move together and what the import-cycle constraint forces.

### What worked

- The grep results gave a precise, file-backed picture of the extraction unit and the server-side churn surface.

### What didn't work

- N/A.

### What I learned

- **The import-cycle constraint is the architecture.** Because the admin CLI must import the customer commands from `hyperslop-cli` (user requirement), `go-go-datadrop → hyperslop-cli`. If the wire types stayed in `go-go-datadrop`, `hyperslop-cli → go-go-datadrop` (client needs wire types) → cycle. So the wire types, the scope/role model, and `pkg/tabular` must move *into* `hyperslop-cli`. This became DR-1 and the spine of §6.
- `pkg/auth` is one package but two concerns; splitting it cleanly is the trickiest part of the move (became DR-2 with three options).

### What was tricky to build

- The `pkg/auth` split: `role.go` itself is mixed (the `Role` type model is shared wire; `EffectiveRole`/`Authorize`/`DropACL` is server-only decision logic). The design has to say "move the type model, keep the decision functions" without leaving either side broken.

### What warrants a second pair of eyes

- DR-2's alias re-export vs fold-into-`pkg/datadrop` tradeoff. The alias (Option 1) minimizes churn but is arguably an "adapter/shim" the `AGENT.md` general guideline discourages — so DR-2 is marked `proposed` and explicitly flagged for user confirmation, with Option 2 specified as the fallback.

### What should be done in the future

- After this ticket, adopt DR-2 Option 2 (fold `Scope`/`Role` into `pkg/datadrop`, sed-rename the 27 files) to delete the alias layer and make `pkg/datadrop` a true leaf.

### Code review instructions

- Verify the §4.5/§4.6 evidence: `grep -rn 'auth\.Scope\|auth\.Role' go-go-datadrop/pkg/datadrop` and `grep -rl 'go-go-datadrop/pkg/datadrop' go-go-datadrop/pkg/{server,store,auth}`.

### Technical details

- `pkg/datadrop` imports `pkg/auth` in `device.go` and `account.go` only.
- `pkg/client` importers: only `pkg/cli/{dataset/get,dataset/push,drops/push,events/tail,exit,exit_test,rows,rows_test,section}.go`.
- `pkg/tabular` importers: `pkg/cli/rows.go`, `pkg/server/handlers_{export,import,table,table_test,handlers_test}.go`.

## Step 4: Design the target architecture and write the doc

Synthesised §1–§13 of the design doc: executive summary, background for a new
intern, evidence-backed current-state, gap analysis, target architecture with
before/after diagrams, eight decision records, API references (HTTP table,
client methods, foundation signatures, exit codes, env vars), key-flow
pseudocode (wiring, Glaze/Writer/Bare examples, parameterized builder), a
nine-phase file-level implementation plan, a test strategy (including a CI guard
that `hyperslop-cli` never imports `go-go-datadrop`), and risks/alternatives/open
questions.

### What I did

- Created the docmgr ticket `HYPERSLOP-1` and its design-doc + diary docs.
- Added vocabulary (topics: cli, architecture, refactor, datadrop, hyperslop, glazed, agent-cli, extraction; doc-types: design-doc, reference, + the standard set) so `doctor` passes.
- Wrote the 85 KB design doc into the ticket's `design-doc/01-…md`.
- Wrote this diary into `reference/01-investigation-diary.md`.

### Why

The deliverable is an intern-grade guide; the structure follows the `ticket-research-docmgr-remarkable` skill's writing-style guide (exec summary → problem → current state → gap → proposed architecture+APIs → decision records → pseudocode/flows → phased plan → test strategy → risks/alternatives → references).

### What worked

- Having line-anchored evidence for every major claim (package docs, `exit.go`'s glazed-#611 comment, the `DATADROP_ADDR`-means-two-things comment in `build.go`, the `pkg/datadrop`→`pkg/auth` import) made the decision records concrete rather than speculative.

### What didn't work

- Nothing failed; this was a writing step.

### What I learned

- The cleanest single sentence for the intern is: "the customer CLI already has a hard boundary against the server — it only talks HTTP — so the extraction is a *move*, not a redesign." Everything else (cycle, auth split, parameterization) follows from preserving that boundary and the user's import direction.

### What was tricky to build

- Keeping the doc honest about what is *decided* vs *proposed*. DR-1 and DR-2 are marked `proposed` because they rest on the `AGENT.md` "no adapters/shims unless asked" guideline; the doc gives a fully-specified fallback (DR-2 Option 2) so execution is not blocked either way.

### What warrants a second pair of eyes

- DR-1 (wire types move into `hyperslop-cli`, making the server depend on a CLI repo) and DR-2 (alias re-export) — both flagged `proposed` and restated as open question #1 in §12.
- The §11.6 CI guard: `go list -deps github.com/hyperslop-systems/hyperslop-cli/...` must not contain `go-go-golems/go-go-datadrop`. This is the invariant that prevents the cycle from creeping back.

### What should be done in the future

- Execute Phase 0–9; after Phase 1, revisit DR-2 (alias vs fold) with the user before Phase 7 hardens it.

### Code review instructions

- Read the design doc §6 (target architecture) and §7 (decision records) first, then cross-check against the §13 file references and the §4 evidence.

### Technical details

- Design doc path: `hyperslop-cli/ttmp/2026/07/29/HYPERSLOP-1--extract-customer-facing-cli-from-go-go-datadrop-into-hyperslop-cli/design-doc/01-extracting-the-customer-facing-cli-into-hyperslop-cli-analysis-design-and-intern-implementation-guide.md`.
- Diary path: `hyperslop-cli/ttmp/2026/07/29/HYPERSLOP-1--…/reference/01-investigation-diary.md`.

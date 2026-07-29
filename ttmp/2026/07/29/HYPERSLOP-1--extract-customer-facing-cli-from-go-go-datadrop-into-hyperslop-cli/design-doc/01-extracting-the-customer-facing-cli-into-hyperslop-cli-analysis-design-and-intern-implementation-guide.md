---
Title: Extracting the customer-facing CLI into hyperslop-cli — analysis, design and intern implementation guide
Ticket: HYPERSLOP-1
Status: active
Topics:
    - cli
    - architecture
    - refactor
    - datadrop
    - hyperslop
    - glazed
    - agent-cli
    - extraction
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: abs:///home/manuel/code/wesen/go-go-golems/go-go-datadrop/cmd/datadrop/main.go
      Note: entrypoint that names the five registrars — the wiring the hyperslop main and the rewired datadrop main both follow
    - Path: abs:///home/manuel/code/wesen/go-go-golems/go-go-datadrop/pkg/auth/scope.go
      Note: shared scope model (Scope, ScopeSet, AllScopes, Validate/ParseScopes) — moves to hyperslop-cli; the rest of pkg/auth is server-only (DR-2)
    - Path: abs:///home/manuel/code/wesen/go-go-golems/go-go-datadrop/pkg/cli/build.go
      Note: BuildCobraCommand/AppName + buildOperatorCommand — the shared foundation that must be parameterized (DR-3) and split (operator stays)
    - Path: abs:///home/manuel/code/wesen/go-go-golems/go-go-datadrop/pkg/cli/exit.go
      Note: 'exit-code contract (0-5), ExitOn, WithExitCodes — the load-bearing glazed #611 fix that must be preserved verbatim'
    - Path: abs:///home/manuel/code/wesen/go-go-golems/go-go-datadrop/pkg/cli/section.go
      Note: ClientSection (--addr/--token, DATADROP_ADDR/TOKEN) — the shared client connection config that moves to hyperslop-cli
    - Path: abs:///home/manuel/code/wesen/go-go-golems/go-go-datadrop/pkg/client/client.go
      Note: typed HTTP client — the pure customer spine (depends only on pkg/datadrop) that moves cleanly
    - Path: abs:///home/manuel/code/wesen/go-go-golems/go-go-datadrop/pkg/datadrop/device.go
      Note: wire types embedding auth.Scope — the pkg/datadrop->pkg/auth dependency that forces the wire types to move with the scope model (DR-1)
ExternalSources: []
Summary: Extract the customer/agent-facing CLI (drops, events, datasets, schemas, device auth, whoami) and its supporting library (wire types, HTTP client, scope/role model, tabular projection, glazed CLI foundation) out of go-go-datadrop into a new hyperslop-cli binary. The proprietary server and its admin CLI stay in go-go-datadrop and import the customer-facing packages back from hyperslop-cli so nothing is duplicated.
LastUpdated: 2026-07-29T11:31:44.983890846-04:00
WhatFor: Onboarding an intern to (a) understand how the datadrop system works today, (b) understand exactly what moves where and why, and (c) execute the extraction in safe phases.
WhenToUse: Read before touching the split between go-go-datadrop and hyperslop-cli. Follow the phased plan in §10.
---








# Extracting the customer-facing CLI into hyperslop-cli — analysis, design and intern implementation guide

## 1. Executive summary

`go-go-datadrop` is today a single Go binary (`datadrop`) that is **both** a server
(`datadrop serve`) **and** a thin client of that server's HTTP API
(`datadrop create`, `push`, `query`, `tail`, `export`, `dataset …`, `schema …`,
`auth device`, `whoami`). The server is proprietary and stays where it is. The
client half is what we hand to agents and customers so they can talk to the
backend. This ticket extracts that client half into its own binary and
repository, **`hyperslop-cli`**, and rewires `go-go-datadrop`'s admin CLI to
**import** the customer-facing commands from `hyperslop-cli` so the two never
drift.

The extraction is mechanical in shape but has one hard constraint that drives
every decision below: the admin CLI in `go-go-datadrop` must import the
customer-facing packages from `hyperslop-cli`, which means `go-go-datadrop`
depends on `hyperslop-cli`. The wire types (`pkg/datadrop`) currently live in
`go-go-datadrop` and are imported by the client; if they stayed there,
`hyperslop-cli` would depend back on `go-go-datadrop` and the two modules would
form an **import cycle**. The wire types — and the small scope/role model they
depend on — therefore have to move *with* the client into `hyperslop-cli`.

Concretely, `hyperslop-cli` becomes the owner of: the wire/contract types
(`pkg/datadrop`), the client-side scope/role model (the shared parts of
`pkg/auth`), the event→table projection (`pkg/tabular`), the typed HTTP client
(`pkg/client`), the shared glazed CLI foundation (`pkg/cli`: client section,
command builder, exit codes, row projections), all five customer command groups
(`authcmd`, `drops`, `events`, `dataset`, `schemacmd`), `whoami`, and the
customer-facing help pages. `go-go-datadrop` keeps the server
(`pkg/server`, `pkg/store`, `pkg/blob`, `pkg/webui`, `pkg/stream`), the
server-side identity/crypto/OIDC logic (the rest of `pkg/auth`), the operator
commands (`serve`, `healthcheck`), and its web-UI help pages; its admin CLI
re-attaches the customer commands by importing their `Register` functions from
`hyperslop-cli`.

The result is two binaries from one codebase of shared libraries, no duplicated
verbs, no import cycle, and a clean product boundary: `hyperslop` is the
agent-facing client; `datadrop` is the proprietary server plus its operator
shell.

## 2. Problem statement and scope

### 2.1 The problem

The `datadrop` binary mixes two audiences in one process:

- **Operators** run `datadrop serve` against a SQLite file, configure OIDC, and
  manage the proprietary backend. They need the server, the store, the web UI,
  OIDC, token-minting crypto, and a container healthcheck.
- **Agents and customers** run `datadrop create/push/query/tail/export/dataset/schema/auth device/whoami`
  against a server they do not control. They need none of the server code —
  they need a typed HTTP client and a set of glazed commands over it.

Shipping the server binary to agents is wrong on every axis: it is a large
binary that pulls in SQLite, OIDC, the embedded web UI, and DuckDB extensions;
it exposes operator-only commands (`serve`) that a customer should never see;
and it conflates the proprietary backend with the public client in one repo and
one release artifact.

### 2.2 The goal

Move the customer-facing CLI into a new, independently-released binary,
`hyperslop-cli`, that:

1. Speaks the **same HTTP API** the server exposes — if `hyperslop push` works,
   `curl` works, because both hit the same endpoints.
2. Uses **glazed commands** (structured output, sections, fields) exactly as the
   current verbs do, preserving the row contracts, the exit-code contract, and
   the env-var contract that scripts already depend on.
3. Carries the **device-pairing** flow (`auth device`) so an agent can mint a
   scoped `ddp_` token by browser approval without ever touching an OIDC bearer
   token.
4. Is **depended on by** `go-go-datadrop`'s admin CLI, so the admin `datadrop`
   binary reuses the identical customer verbs instead of duplicating them.

### 2.3 Out of scope

- Changing the HTTP wire protocol or the JSON shapes (those are the contract;
  this ticket preserves them).
- Rebuilding the server, the store, the web UI, or OIDC.
- Adding new customer commands (this is a move, not a feature ticket).
- Renaming the `datadrop` product or the `ddp_` token format.

### 2.4 Repositories and module paths involved

| Repo | Directory in the `split-cli` workspace | Current module path | Target module path |
|---|---|---|---|
| go-go-datadrop | `go-go-datadrop/` | `github.com/go-go-golems/go-go-datadrop` | unchanged |
| hyperslop-cli | `hyperslop-cli/` | `github.com/go-go-golems/XXX` (placeholder) | `github.com/hyperslop-systems/hyperslop-cli` |
| glazed | `glazed/` | `github.com/go-go-golems/glazed` | unchanged |

The workspace `go.work` already links all three so the extraction can be done
with local replaces before any tag is cut. The `hyperslop-cli` git remote is
already `git@github.com:hyperslop-systems/hyperslop-cli` (see
`hyperslop-cli/.git`), so the module path rename in §10 (Phase 0) is just
editing `go.mod`, `.goreleaser.yaml`, the Makefile, and `pkg/logcopter.go`.

## 3. Background: how datadrop works today (read this first)

If you are new to the codebase, read this section before touching anything. It
explains the product, the data model, the auth model, the HTTP contract, and
why the binary is shaped the way it is. Everything in §4–§10 builds on it.

### 3.1 The product: a CLI-first research data inbox

`datadrop` is a self-hostable, **CLI-first** research data inbox: a single
binary that accepts append-only event data over HTTP and CLI, stores it durably
in SQLite, validates it against JSON Schema, serves latest-N and time-range
queries, streams new events over SSE, and exports open formats (CSV, NDJSON,
JSON). It is inspired by Wolfram Data Drop and is designed to be ordinary enough
to work with `curl`, pipes, and SQL (`go-go-datadrop/README.md`).

The defining design rule, repeated throughout the CLI code, is:

> Client subcommands must never reach into the SQLite file directly — if
> `datadrop push` works, `curl` works, because both exercise the same
> endpoints.
> (`go-go-datadrop/pkg/cli/root.go`, package doc comment)

This is why every customer verb goes through the typed HTTP client in
`pkg/client`, and it is the reason the client can be lifted out cleanly: the
client already has a hard boundary against the server.

### 3.2 The two data shapes

The server holds two kinds of data, and the distinction is the main thing to
understand before using it (`go-go-datadrop/README.md`):

| | **Stream** (v0.1) | **Dataset** (v0.2) |
|---|---|---|
| Shape | unbounded, append-only, live | finite, versioned, immutable |
| Unit | one small JSON event | a body of files with a manifest |
| Identity | a server-assigned sequence | the content's SHA-256 digest |
| Correction | append a superseding event | publish a new version |
| Read | latest-N, time range, live SSE | whole file, or a byte range |

A dataset can be *materialized* into a stream, so a published CSV becomes events
that each point back at the exact bytes and row they came from. The CLI mirrors
this split: `pkg/cli/events` reads streams; `pkg/cli/dataset` publishes and
retrieves datasets.

### 3.3 The authentication model

There are three ways to be authenticated, and the CLI touches all of them
(`go-go-datadrop/pkg/auth/principal.go`, `scope.go`, `device.go`,
`token.go`):

1. **Anonymous** — no credential. Only works against a `public_read` drop.
2. **Browser session** — a human signs in through the web workbench via OIDC
   (ZITADEL). This produces a `dd_session` cookie. The CLI does **not** use
   sessions; sessions are a server/web-UI concept.
3. **API token** — a machine credential shaped `ddp_<id>_<secret>` (see
   `pkg/auth/token.go` for the format and why each byte earns its place). This
   is what the CLI uses. A token carries **scopes** (`drops:read`,
   `drops:write`, `datasets:write`, `admin`) that *limit* what the credential
   may do; the effective right is the intersection of the token's scopes and
   the user's membership on the target drop, computed per request on the server
   (`pkg/auth/role.go`, `Authorize`).

The crucial property for the CLI is: **the CLI never accepts an OIDC/ZITADEL
bearer token as a datadrop data-plane credential.** Instead, an agent obtains a
scoped `ddp_` token through the **device-pairing flow**:

```
agent (CLI)                 datadrop server                 human browser
   |  POST /v1/device/authorizations  ->|                        |
   |  <-- {device_code, user_code, url} |                        |
   |  print url + user_code to stderr   |                        |
   |  poll POST /v1/device/tokens       |  <-- sign in + verify   |
   |  (AuthorizationPending… retry)     |      code               |
   |  <-- {token: ddp_…}                |                        |
   |  export DATADROP_TOKEN=ddp_…       |                        |
```

This is RFC-8628-style polling, implemented in
`go-go-datadrop/pkg/cli/authcmd/device.go`. The token is printed **once** to
stdout (so it can be captured) or written to a `0600` credential file.

### 3.4 The HTTP API surface (the wire contract)

The customer CLI is a thin wrapper over a small REST API rooted at `/v1`. The
endpoints are enumerated in §8.1. The contract types live in
`go-go-datadrop/pkg/datadrop` (`Drop`, `DropStats`, `Envelope`, `EventQuery`,
`QueryResult`, `Schema`, `Dataset`, `DatasetVersion`, `DeviceAuthorization`,
`APIToken`, `User`, `Member`, etc.). Errors come back as a problem document
(`pkg/client/client.go`, `APIError` with `code`/`detail`/`request_id`/`errors`).

### 3.5 The single-binary design and why it is being split

The single binary was the right call for v0.1: it let one person run the server
and exercise it from another shell with the same executable, and it guaranteed
the CLI and the HTTP API could not drift (same types, same repo). The cost only
appears now that the CLI has to be given to **agents who do not run the
server**: those agents get a 50+ MB binary full of server code they cannot use
and operator commands they should not see. The split keeps the "no drift"
property (the admin CLI imports the customer verbs) while giving the customer
their own small binary.

## 4. Current-state architecture (evidence-backed)

All paths below are relative to the `go-go-datadrop` worktree at
`/home/manuel/workspaces/2026-07-29/split-cli/go-go-datadrop` (an up-to-date
checkout of `github.com/go-go-golems/go-go-datadrop`; the canonical tree is at
`/home/manuel/code/wesen/go-go-golems/go-go-datadrop`).

### 4.1 Repository layout

```
go-go-datadrop/
  cmd/datadrop/main.go          # the one entrypoint; wires every command group
  pkg/
    cli/                        # shared CLI foundation + operator commands
      root.go section.go build.go exit.go rows.go fields.go whoami.go
      serve.go healthcheck.go   # operator-only
      authcmd/  drops/  events/  dataset/  schemacmd/   # customer command groups
    client/                     # typed HTTP client (client.go, me.go, datasets.go)
    datadrop/                   # wire/contract types + validators
    auth/                       # identity model: scopes, roles, principal, token, OIDC, device crypto
    tabular/                    # event -> table projection (shared by CLI rows + server /table)
    server/  store/  blob/  webui/  stream/  schema/   # server-only
    doc/                        # embedded help pages (topics/ + tutorials/)
```

### 4.2 The command tree and how it is wired

`cmd/datadrop/main.go` is intentionally tiny. It does not know about cobra
directly; it names the five group registrars and hands them to `cli.Execute`:

```go
// go-go-datadrop/cmd/datadrop/main.go
func main() {
    os.Exit(cli.Execute(
        authcmd.Register,
        drops.Register,
        events.Register,
        schemacmd.Register,
        dataset.Register,
    ))
}
```

`pkg/cli/root.go` (`NewRootCmd`, `Execute`) builds the tree: it attaches the
verbs it owns directly (`whoami`, `serve`, `healthcheck`), then calls each
registrar so each group attaches its own verbs, then loads the glazed help
system from `pkg/doc`, then installs the logging section. The registrars are
passed **as arguments** rather than imported by `pkg/cli` because the group
packages import `pkg/cli` back (for the client section, row projections, and the
exit helper) — so the dependency runs one way only, and `main.go` is "the one
place in the tree that knows about all of them" (`pkg/cli/root.go` comment,
`pkg/cli/build.go`, `Registrar` type).

Each group package has the same shape: a `root.go` with a `Register(root *cobra.Command) error`
that calls the shared `ddcli.AddCommands(root, NewXCommand, …)` and nothing
else. Example (`pkg/cli/drops/root.go`):

```go
package drops
import ("github.com/spf13/cobra"; ddcli "github.com/go-go-golems/go-go-datadrop/pkg/cli")
func Register(root *cobra.Command) error {
    return ddcli.AddCommands(root, NewCreateCommand, NewListCommand, NewInspectCommand, NewPushCommand)
}
```

### 4.3 The shared CLI foundation (`pkg/cli`)

This is the layer every customer verb — and the operator verbs — stand on. It
is the heart of what moves to `hyperslop-cli`.

- **`section.go` — the client section.** A glazed *section* named
  `datadrop-client` carrying two fields: `--addr` (server base URL, default
  `http://localhost:8080`, env `DATADROP_ADDR`) and `--token` (bearer token,
  `fields.TypeSecret` so it is redacted in `--print-parsed-fields` and
  `--help`, env `DATADROP_TOKEN`). `ClientFrom(vals)` decodes the section and
  builds a `*client.Client`. `ClientSettingsFrom(vals)` decodes without
  building, for `whoami` which reports the address it used. The section is a
  glazed section (not persistent cobra flags) so it can be filled from a config
  file or named profile, and so `--addr`/`--token` appear **only** on commands
  that talk to a server (DR-76 in the original tree).
- **`build.go` — the command builder.** `BuildCobraCommand(command)` turns one
  glazed command into a cobra command with datadrop's conventions:
  `WithExitCodes` is applied, and a `CobraParserConfig` sets
  `ShortHelpSections = [schema.DefaultSlug, ClientSectionSlug]` and
  `AppName = "datadrop"`. `AppName` is load-bearing: it is what switches on
  glazed's env source, so `DATADROP_ADDR`/`DATADROP_TOKEN` resolve. Leave it
  empty and the env vars silently stop working. `buildOperatorCommand` is the
  variant **without** `AppName`, used by `serve`/`healthcheck` so they do not
  read `DATADROP_*` (because `DATADROP_ADDR` means "which server to talk to"
  for a client but `serve --addr` means "which socket to bind" — a name that
  means two things; `pkg/cli/build.go` comment). `AddCommands` and
  `addOperatorCommands` loop over builders; `Registrar` and `Builder` are the
  two function types.
- **`exit.go` — the exit-code contract.** Five codes scripts depend on:
  `ExitOK=0`, `ExitError=1`, `ExitUsage=2`, `ExitAuth=3`, `ExitNotFound=4`,
  `ExitValidation=5`. `ErrorPrefix = "datadrop: "`. `exitCodeFor(err)` maps a
  `*client.APIError`'s HTTP status to a code (401/403→Auth, 404→NotFound,
  400/409/413/422→Validation, else→Error). `ExitOn(err)` reports with the
  prefix and exits, except for `nil`, `context.Canceled` (Ctrl-C on `tail
  --follow` is not a failure), and `*cmds.ExitWithoutGlazeError`. `WithExitCodes`
  wraps a `GlazeCommand`/`WriterCommand`/`BareCommand` so every error path goes
  through `ExitOn` once per command — this is the fix for glazed's cobra builder
  calling `cobra.CheckErr` (which prints `Error:` and exits 1) instead of
  returning the error to `Execute` (`pkg/cli/exit.go` long comment, upstream
  glazed issue #611).
- **`rows.go` — the row projections.** One projection function per response
  type (`RowForDrop`, `RowForDropStats`, `RowForAppendResult`, `RowForSchema`,
  `RowForPutSchemaResult`, `RowForDataset`, `RowForDatasetVersion`,
  `RowForDeletedVersion`, `RowForImportResult`, `RowForGCResult`,
  `RowForPrincipal`, `RowsForEnvelopes`, `RowForEnvelope`). The row shape is a
  **public API** (DR-79): the moment `datadrop query --output-fields seq,time,data.temp_c`
  works, those names are something scripts depend on, so `rows_test.go` pins the
  exact key set of each projection as a change detector. Key names are the API's
  JSON names; timestamps use `datadrop.FormatTime`. Event flattening is **not**
  implemented here — it delegates to `tabular.FromEvents`, the same projection
  the server's `/table` endpoint returns (DR-83), so the CLI and the web
  workbench name the same columns. Depends on `pkg/client` (for `Me`,
  `GCResult`), `pkg/datadrop`, and `pkg/tabular`.
- **`fields.go` — small field helpers.** `DropStreamField()` (the
  `--drop-stream` flag, domain-specific because glazed v1.4 dropped the generic
  `--stream`), `ReadSpec(path)` (read a JSON document from a file or `-` stdin,
  used by `schema put` and dataset manifests), `HumanBytes(n)`. Depends on
  `pkg/datadrop` (for `datadrop.DefaultStream`).
- **`whoami.go` — the one-verb group.** `WhoamiCommand` is a `GlazeCommand`
  that calls `ClientSettingsFrom` + `ClientFrom` + `client.Whoami` and emits
  one row via `RowForPrincipal`. It lives in `pkg/cli` (not a subpackage)
  because it is the only verb in its group.

### 4.4 The typed HTTP client (`pkg/client`)

`pkg/client` is a pure HTTP client with no server, no SQLite, no OIDC. It speaks
only `pkg/datadrop` types. This is the cleanest package in the tree and the
easiest to move.

- **`client.go`** — `Client{BaseURL, Token, HTTP}`; `New(baseURL, token)`
  validates the URL (absolute http/https, no userinfo/query/fragment) and builds
  a transport with bounded header/TLS timeouts but **no overall timeout**
  (streams and large downloads are long-lived by design; request lifetime comes
  from `ctx`). `APIError` is the problem-document type. Methods: `CreateDrop`,
  `ListDrops`, `GetDrop`, `Push`, `Query`, `PutSchema`, `GetSchema`, `Export`
  (returns an `io.ReadCloser` the caller owns), `Stream` (SSE: returns a
  `<-chan StreamEvent` and a `<-chan error`; `parseSSE` decodes
  `text/event-stream` with a 4 MiB token buffer). Internals: `doJSON`, `do`
  (sets `Authorization: Bearer <token>` and `Content-Type: application/json`,
  converts ≥400 into `*APIError` via `apiErrorFrom`).
- **`me.go`** — `Me`, `MeUser`, `MeProvider` (deliberately duplicated from
  `server.MeResponse` so the client does not depend on the server package for
  one struct) and `Whoami(ctx)` → `GET /v1/me`.
- **`datasets.go`** — the dataset client: `ListDatasets`, `GetDataset`,
  `GetDatasetVersion`, `OpenDatasetVersion`, `CommitDatasetVersion`,
  `DeleteDatasetVersion`, `BlobExists` (HEAD precheck), `UploadDatasetFile`
  (streams a local file, sends locally-computed digest, sets `ContentLength`),
  `MountDatasetFile` (mount already-held bytes without transfer),
  `DownloadDatasetFile`, `DownloadDatasetArchive` (tar), `PushDataset` (the
  open/upload-or-mount/commit orchestration that turns a republish with one
  changed file into a single upload), `HashFile`, `VerifyFile` (client-side
  integrity check), `ImportDataset` (materialize a dataset file into a stream),
  `GarbageCollect`, `GCResult`. Helpers: `datasetPath`, `versionPath`,
  `escapePath`, `apiErrorFrom`.

### 4.5 The wire types (`pkg/datadrop`) and why they depend on `pkg/auth`

`pkg/datadrop` is the contract package: the request/response structs and the
pure validators that both the client and the server speak. Files: `drop.go`,
`event.go`, `query.go`, `schema.go`, `dataset.go`, `device.go`, `account.go`.

**Critical fact for the extraction:** `pkg/datadrop` imports `pkg/auth`. The
wire types embed `auth.Scope` and `auth.Role`:

- `pkg/datadrop/device.go`: `DeviceAuthorization.RequestedScopes []auth.Scope`,
  `StartDeviceAuthorizationRequest.Scopes []auth.Scope`,
  `DeviceTokenResponse.Scopes []auth.Scope`.
- `pkg/datadrop/account.go`: `APIToken.Scopes []auth.Scope`,
  `Member.Role auth.Role`, `SetMemberRequest.Role auth.Role`,
  `CreateTokenRequest.Scopes []auth.Scope`.

So whatever moves the wire types out of `go-go-datadrop` must also move the
`Scope` and `Role` types they depend on (or break the dependency — see DR-2).

### 4.6 The `pkg/auth` package and what is shared vs server-only

`pkg/auth` is a single package today, but it is really two things glued
together. The extraction has to separate them.

**Shared (used by the wire types and/or the client device command — must move to
`hyperslop-cli`):**

- `scope.go` (all of it): `Scope` (`drops:read`, `drops:write`,
  `datasets:write`, `admin`), `ScopeSet`, `AllScopes`, `NewScopeSet`,
  `FullScopeSet`, `Has` (admin implies all), `Slice`, `String`, `ParseScopes`,
  `ValidateScopes`, `isKnownScope`, `joinScopes`.
- `role.go` (the **type model** only): `Role` (`reader`/`writer`/`admin`),
  `RoleNone`, `AssignableRoles`, `rank`, `AtLeast`, `Valid`, `ParseRole`.
- `device.go` (one function): `ValidateDeviceScopes` (rejects `admin` on device
  tokens; used by `pkg/cli/authcmd/device.go`).

**Server-only (stays in `go-go-datadrop`):**

- `principal.go`: `Kind`, `Principal`, `Anonymous`, `IsAuthenticated`, `Label`,
  `Allowed`, `WithPrincipal`, `FromContext` — request-time identity, used by
  server middleware/handlers. Not referenced by wire types or the client.
- `token.go`: `MintToken`, `HashSecret`, `VerifySecret`, `ParseToken`,
  `LooksLikeToken`, `NewUserID`, `NewSessionValue`, `NewFlowValue`,
  `newPrefixedID`, `TokenPrefix="ddp_"` — minting and hashing, server-only.
- `device.go` (the rest): `NewDeviceUserCode`, `NormalizeDeviceUserCode`,
  `HashDeviceUserCode`, `VerifyDeviceUserCode`, `NewDeviceAuthorizationID`,
  `HashDeviceCode` — server-side code generation and keyed hashing with a
  pepper.
- `oidc.go`: `Claims`, the `Provider` interface, OIDC ID-token verification
  (depends on `coreos/go-oidc` and `golang.org/x/oauth2`).
- `role.go` (the **decision** part): `DropACL`, `EffectiveRole`, `Authorize` —
  the per-request authorization matrix, used only by the server.

`auth.Scope`/`auth.Role`/`auth.ValidateDeviceScopes`/`auth.AllScopes`/etc. are
referenced in **27 files** across `pkg/server`, `pkg/store`, `pkg/auth`, and
`pkg/cli` (≈158 occurrences). The split strategy for these references is DR-2.

### 4.7 The tabular projection (`pkg/tabular`) — shared by CLI and server

`pkg/tabular` (`infer.go`, `project.go`, `table.go`) projects events into a
column-resolved table. It imports only `pkg/datadrop` (and stdlib). It is
imported by **both** `pkg/cli/rows.go` (the CLI's `RowsForEnvelopes`) and the
server's `pkg/server/handlers_{export,import,table}.go` (the `/table` endpoint
and the CSV/NDJSON exporters). Because it depends on `pkg/datadrop`, it moves
with the wire types; the server then imports it back from `hyperslop-cli`.

### 4.8 The command groups

| Group package | Verbs | Command kinds | Notes |
|---|---|---|---|
| `pkg/cli/authcmd` | `auth device` | `BareCommand` | Device pairing; prints token to stdout or a `0600` file. Uses raw `http.Client` (not `pkg/client`) to `/v1/device/*`. |
| `pkg/cli/drops` | `create`, `list`, `inspect`, `push` | `GlazeCommand` | Top-level verbs (no `drops` parent group). |
| `pkg/cli/events` | `query`, `tail`, `export` | `GlazeCommand` (query, tail), `WriterCommand` (export) | `tail --follow` streams; `export` writes server-formatted bytes. |
| `pkg/cli/dataset` | `push`, `list`, `show`, `get`, `import`, `rm`, `gc` | `GlazeCommand` (list, show, rm, gc, import), `BareCommand` (get, push) | `get`/`push` write to the filesystem, so they are Bare (DR-81). |
| `pkg/cli/schemacmd` | `schema put`, `schema show` | `GlazeCommand` | Named `schemacmd` to avoid clashing with `glazed/pkg/cmds/schema`. |

The three command kinds matter for the intern:

- **`GlazeCommand`** (`RunIntoGlazeProcessor`) emits rows into a glazed
  processor; output is structured (`--format table/json/jsonl/csv`, `--output-fields`).
  This is the common case.
- **`WriterCommand`** (`RunIntoWriter`) writes bytes the server formatted
  (e.g. `export --format csv`). It is the one place `--format` (server-side
  export format) vs `--output` (client-side row rendering) is concrete.
- **`BareCommand`** (`Run`) has side effects that are not rows: `auth device`
  (a secret on stdout / a credential file), `dataset get`/`push` (files on
  disk), `serve`/`healthcheck` (a process / an exit code).

`WithExitCodes` wraps all three kinds, so every error path honours the exit-code
contract regardless of kind.

### 4.9 The operator surface (`serve`, `healthcheck`)

- **`pkg/cli/serve.go`** — `ServeCommand`, a `BareCommand` built via
  `buildOperatorCommand` (no `DATADROP_*` env). Imports `pkg/auth`, `pkg/blob`,
  `pkg/server`, `pkg/store`, `pkg/webui`. Its flags (`--addr`, `--db`,
  `--blobs`, `--external-url`, `--oidc-*`, `--device-code-pepper-file`,
  `--trusted-proxy-cidrs`, `--session-lifetime`, `--max-body-bytes`,
  `--max-upload-bytes`, `--no-ui`, …) take env fallbacks via the local `envOr`
  helper reading the *specific* variable each means (`DATADROP_EXTERNAL_URL`,
  `DATADROP_OIDC_*`, `DATADROP_DEVICE_CODE_PEPPER_FILE`, …), not the client
  prefix.
- **`pkg/cli/healthcheck.go`** — `HealthcheckCommand`, a `BareCommand` that
  probes `GET /healthz` and exits 0/1. Exists because the runtime image is
  distroless (no shell, no curl); used as a container healthcheck. Reads
  `DATADROP_HEALTH_URL` via `envOr`.

Both are **server/operator-only** and stay in `go-go-datadrop`. They use the
shared `WithExitCodes`/exit-code helpers, so those helpers must remain available
to them after the move (see DR-3 and §6.6).

### 4.10 The help system (`pkg/doc`)

`pkg/doc/doc.go` embeds `topics/` and `tutorials/` and loads them into a glazed
`HelpSystem` via `AddDocToHelpSystem`, called once from the CLI root
(`pkg/cli/root.go`). Pages carry glazed help frontmatter, queryable by slug
(`datadrop help web-ui-object-model`) and filterable by topic. Today the pages
are mostly about the **web workbench** (object model, presentation protocol,
window manager, store instances, component layers) plus
`topics/06-cli-output.md` (CLI output) and two tutorials (embedding a
workbench, the ZITADEL dev stack). The web-UI pages describe the browser
interface `datadrop serve` hosts and are operator-facing; `06-cli-output.md` is
customer-facing.

### 4.11 Dependency graph (today)

```
                         glazed (v1.4.1)
                            ^
                            |
cmd/datadrop/main.go ----> pkg/cli --+--> pkg/client --> pkg/datadrop --> pkg/auth (Scope/Role)
   |                  |    |         |                                          ^
   |                  |    |         +--> pkg/tabular --> pkg/datadrop          | (server-only crypto/oidc/principal)
   |                  |    +--> serve.go --> pkg/server, pkg/store, pkg/blob, pkg/webui, pkg/stream, pkg/auth
   |                  |    +--> healthcheck.go (stdlib only)
   |                  +-- registrars: authcmd, drops, events, dataset, schemacmd (all import pkg/cli, pkg/datadrop, pkg/client, pkg/auth[scopes])
   |
   +-- pkg/server --> pkg/store, pkg/blob, pkg/auth, pkg/datadrop, pkg/tabular, pkg/stream, pkg/webui
```

The customer surface is the left spine: `main → pkg/cli → pkg/client →
pkg/datadrop → pkg/auth(scopes)`, plus `pkg/tabular` and the command groups.
The server surface is everything hanging off `serve.go` and `pkg/server`.

## 5. Gap analysis — why the current structure cannot ship as a customer CLI

1. **One binary, two audiences.** The customer gets `serve`, `healthcheck`, and
   the whole server in their binary. There is no way to ship just the client.
2. **Heavyweight dependency closure.** A customer binary today transitively
   pulls in `modernc.org/sqlite`, `coreos/go-oidc`, `oauth2`, the embedded web
   UI, and DuckDB Wasm extensions — none of which the client uses. The binary is
   far larger than the customer job requires.
3. **No product boundary.** The client and the proprietary server share a repo,
   a module, and a release. There is no place to put agent-facing docs, agent
   onboarding, or a separate release cadence.
4. **Cross-audience env-var collision.** `DATADROP_ADDR` already means two
   things (client "talk to", `serve` "bind"), papered over today by
   `buildOperatorCommand` skipping the env source. A customer binary should not
   even carry that ambiguity.
5. **Duplication risk.** If we just *copied* the client into `hyperslop-cli`,
   the two CLIs would drift the first time someone fixed a verb in one and not
   the other. The user's explicit requirement is that the admin CLI **import**
   the customer-facing functionality to avoid duplication — which fixes the
   direction as `go-go-datadrop → hyperslop-cli` and forces the wire types out
   (§6.4).

## 6. Target architecture

### 6.1 Two binaries, one shared library

`hyperslop-cli` becomes **both** a library (the `pkg/...` packages) and a binary
(`cmd/hyperslop`). `go-go-datadrop` depends on `hyperslop-cli` as a library and
re-attaches the customer verbs in its own `cmd/datadrop` root. The server
packages in `go-go-datadrop` depend on `hyperslop-cli` only for the wire types,
the scope/role model, and `pkg/tabular` — the slim contract surface, not the
CLI machinery.

### 6.2 What moves into `hyperslop-cli` (the extraction unit)

Everything in the "customer spine" from §4.11, plus the shared foundation the
operator commands also use:

| Source (go-go-datadrop) | Target (hyperslop-cli) | What it is |
|---|---|---|
| `pkg/datadrop/` | `pkg/datadrop/` | Wire/contract types + validators |
| `pkg/auth/scope.go` + `role.go` (type model) + `ValidateDeviceScopes` | `pkg/auth/` (slim, client-side) | Shared scope/role model |
| `pkg/tabular/` | `pkg/tabular/` | Event→table projection (shared) |
| `pkg/client/` | `pkg/client/` | Typed HTTP client |
| `pkg/cli/{section,build,exit,rows,fields,whoami}.go` | `pkg/cli/` | Shared glazed CLI foundation |
| `pkg/cli/{authcmd,drops,events,dataset,schemacmd}/` | `pkg/cli/{…}/` | Customer command groups |
| `pkg/doc/topics/06-cli-output.md` (+ new agent docs) | `pkg/doc/` | Customer-facing help |
| — (new) | `cmd/hyperslop/main.go` | Customer binary entrypoint |
| — (new) | `pkg/cli/root.go` | `hyperslop` root: customer registrars + logging + help |

### 6.3 What stays in `go-go-datadrop`

- `pkg/server/`, `pkg/store/`, `pkg/blob/`, `pkg/webui/`, `pkg/stream/`,
  `pkg/schema/` — the proprietary server, unchanged except import paths.
- `pkg/auth/` — **server-only** identity/crypto/OIDC/authz-decision
  (`principal.go`, `token.go`, the crypto half of `device.go`, `oidc.go`,
  `EffectiveRole`/`Authorize`/`DropACL` from `role.go`). It imports the shared
  `Scope`/`Role` from `hyperslop-cli/pkg/auth` (see DR-2).
- `pkg/cli/serve.go`, `pkg/cli/healthcheck.go` — operator commands.
- `pkg/cli/root.go` — the **`datadrop` admin root** (operator commands +
  re-imported customer registrars + logging + web-UI help).
- `pkg/cli/build.go` — only `buildOperatorCommand`/`addOperatorCommands`
  remain; `BuildCobraCommand`/`AddCommands`/`Registrar`/`Builder` move to
  `hyperslop-cli` and are re-imported.
- `pkg/doc/topics/01–05` and `tutorials/` — web-UI/operator help.
- `cmd/datadrop/main.go` — rewired to import customer registrars from
  `hyperslop-cli`.

### 6.4 The dependency direction and why (cycle avoidance)

```
            glazed
              ^
              |
   hyperslop-cli (module: hyperslop-systems/hyperslop-cli)
     pkg/datadrop  pkg/auth(scope/role)  pkg/tabular
     pkg/client    pkg/cli(FOUNDATION + groups)   cmd/hyperslop
              ^
              |  (go-go-datadrop depends ON hyperslop-cli; never the reverse)
              |
   go-go-datadrop (module: go-go-golems/go-go-datadrop)
     pkg/server pkg/store pkg/blob pkg/webui pkg/stream
     pkg/auth(server crypto/oidc/principal/authz)  pkg/cli(serve,healthcheck,admin root)
     cmd/datadrop  -> imports hyperslop-cli/pkg/cli/{authcmd,drops,events,dataset,schemacmd} + foundation
```

The arrow is one-way: `go-go-datadrop → hyperslop-cli`. `hyperslop-cli` never
imports `go-go-datadrop`. This is what lets the admin CLI reuse the customer
verbs (it imports them) **and** what forces the wire types to move: if
`pkg/datadrop` stayed in `go-go-datadrop`, `hyperslop-cli`'s `pkg/client` would
need it, creating the reverse arrow and a cycle. Moving `pkg/datadrop` (and the
`pkg/auth` scope/role model it depends on, and `pkg/tabular` which depends on
it) into `hyperslop-cli` breaks the would-be cycle.

### 6.5 Target package layout in `hyperslop-cli`

```
hyperslop-cli/                                module github.com/hyperslop-systems/hyperslop-cli
  cmd/hyperslop/main.go                       # wires customer registrars; calls cli.Execute
  pkg/
    auth/        scope.go role.go device-validate.go   # shared Scope/Role + ValidateDeviceScopes
    datadrop/    drop.go event.go query.go schema.go dataset.go device.go account.go
    tabular/     infer.go project.go table.go
    client/      client.go me.go datasets.go
    cli/
      root.go section.go build.go exit.go rows.go fields.go whoami.go   # foundation + hyperslop root
      authcmd/ drops/ events/ dataset/ schemacmd/                        # command groups
    doc/         doc.go topics/06-cli-output.md topics/<new agent docs>   # customer help
  go.mod Makefile .goreleaser.yaml lefthook.yml .golangci.yml .github/    # go-go-golems project plumbing
```

### 6.6 Target `go-go-datadrop` after extraction

```
go-go-datadrop/                               module github.com/go-go-golems/go-go-datadrop
  cmd/datadrop/main.go                        # imports hyperslop-cli/pkg/cli/* registrars + local serve/healthcheck
  pkg/
    auth/        principal.go token.go device.go(crypto) oidc.go role.go(EffectiveRole/Authorize/DropACL) + alias.go
    server/ store/ blob/ webui/ stream/ schema/                            # unchanged (import paths updated)
    cli/         root.go(admin) build.go(operator) serve.go healthcheck.go # operator + admin root
    doc/         doc.go topics/01-05*.md tutorials/                        # web-UI help
  go.mod (requires github.com/hyperslop-systems/hyperslop-cli)
```

### 6.7 Module path and naming (see DR-6)

- `hyperslop-cli` module path → `github.com/hyperslop-systems/hyperslop-cli`
  (matches the existing git remote; the placeholder
  `github.com/go-go-golems/XXX` in `go.mod` is renamed in Phase 0).
- Binary name → `hyperslop` (so `cmd/hyperslop/main.go`, `.goreleaser` `binary: hyperslop`,
  `main: ./cmd/hyperslop`). The `cmd/XXX` placeholder directory is renamed.
- App name (glazed env prefix) → `hyperslop`, so env vars become
  `HYPERSLOP_ADDR`, `HYPERSLOP_TOKEN`, `HYPERSLOP_LOG_LEVEL`. The admin `datadrop`
  binary keeps `DATADROP_*` (see DR-3).
- The `ddp_` token prefix, the `/v1` API, and all JSON shapes are unchanged.

### 6.8 Diagrams — before and after

**Before** (one module, one binary, mixed audiences):

```
+--------------------------- go-go-datadrop (module) ---------------------------+
|  cmd/datadrop/main.go                                                        |
|      |                                                                       |
|      v                                                                       |
|  pkg/cli (foundation + serve + healthcheck + groups)                         |
|      |--> pkg/client --> pkg/datadrop --> pkg/auth (scopes + server crypto)  |
|      |--> pkg/tabular --> pkg/datadrop                                       |
|      +--> serve --> pkg/server, pkg/store, pkg/blob, pkg/webui, pkg/stream   |
+------------------------------------------------------------------------------+
```

**After** (two modules, two binaries, one shared library, one-way dependency):

```
+---- hyperslop-cli (module) ----+        +---- go-go-datadrop (module) ----+
| cmd/hyperslop                  |        | cmd/datadrop                    |
|   |                            |        |   |  imports registrars          |
|   v                            |        |   +--------------+               |
| pkg/cli (foundation + groups)  |<-------+   pkg/cli (admin root, serve,   |
|   |--> pkg/client              |  uses     healthcheck)                   |
|   |--> pkg/datadrop            |  customer  |--> serve --> pkg/server,     |
|   |--> pkg/tabular             |  verbs     |             pkg/store,       |
|   +--> pkg/auth (scope/role)   |            |             pkg/blob,        |
+--------------------------------+            |             pkg/webui,       |
                                              |             pkg/stream       |
                                              +--> pkg/auth (server crypto)  |
                                                   uses hyperslop-cli/      |
                                                   pkg/auth{Scope,Role}     |
                                              +-----> pkg/datadrop,          |
                                              |       pkg/tabular (from      |
                                              |       hyperslop-cli)         |
                                              +------------------------------+
```

## 7. Decision records

### Decision DR-1: Module home for the shared wire types + scope/role model

- **Context:** The admin CLI must import the customer commands from
  `hyperslop-cli`, so `go-go-datadrop → hyperslop-cli`. The customer commands
  need the wire types (`pkg/datadrop`), which currently live in
  `go-go-datadrop`. If they stayed, `hyperslop-cli → go-go-datadrop` and the
  modules cycle.
- **Options considered:**
  1. Move `pkg/datadrop` (+ the `pkg/auth` scope/role model it depends on, +
     `pkg/tabular`) into `hyperslop-cli`; `go-go-datadrop` imports them back.
  2. Create a *third* module `hyperslop-api` for the wire types + scope/role +
     client, and have both `hyperslop-cli` and `go-go-datadrop` depend on it.
  3. Keep `pkg/datadrop` in `go-go-datadrop`; have `hyperslop-cli` depend on
     `go-go-datadrop` for wire types and do **not** have the admin CLI import
     `hyperslop-cli` (instead keep the customer verbs in `go-go-datadrop` and
     have `hyperslop-cli` be a thin wrapper that imports them).
- **Decision:** Option 1.
- **Rationale:** Matches the user's instruction directly ("moving stuff out of
  go-datadrop into a CLI"; "the admin CLI imports the customer-facing
  functionality"). Avoids a third module to maintain and release. The
  dependency direction is exactly the one the user described. Option 2 is
  cleaner conceptually (the server would not depend on a CLI repo for its core
  types) but adds release/coordination overhead the user did not ask for; it is
  recorded as the upgrade path in §12. Option 3 contradicts the "move out"
  requirement (the verbs would still live in the server repo).
- **Consequences:** The server repo (`go-go-datadrop`) depends on a CLI repo
  (`hyperslop-cli`) for its wire types — unusual but acceptable because
  `hyperslop-cli`'s `pkg/datadrop`/`pkg/auth`/`pkg/tabular` are pure libraries
  with no CLI imports. Must validate that `hyperslop-cli/pkg/datadrop` does not
  accidentally grow a dependency on `pkg/cli` or `pkg/client` (it must stay a
  leaf). Cutting a `hyperslop-cli` release now also gates a `go-go-datadrop`
  release (or the workspace uses a local replace).
- **Status:** proposed (awaiting user confirmation per the "no adapters/shims
  unless asked" guideline in `AGENT.md`, since Option 1 is the assumption the
  whole plan rests on).

### Decision DR-2: How to split `pkg/auth` (scope/role model vs server crypto)

- **Context:** `pkg/auth` is one package but splits into shared (scope/role
  model, used by wire types + the device command) and server-only (principal,
  token minting, device-code crypto, OIDC, `EffectiveRole`/`Authorize`). The
  shared part must move to `hyperslop-cli`; the server part stays. 27 files
  (≈158 occurrences) reference `auth.Scope`/`auth.Role`/etc.
- **Options considered:**
  1. **Alias re-export.** Move the shared scope/role model into
     `hyperslop-cli/pkg/auth` (package `auth`). In `go-go-datadrop/pkg/auth`,
     re-export them with type aliases and vars:
     `type Scope = hapiauth.Scope; type Role = hapiauth.Role; var ValidateDeviceScopes = hapiauth.ValidateDeviceScopes; var AllScopes = hapiauth.AllScopes; …`.
     The 27 server files keep writing `auth.Scope`/`auth.Role` **unchanged**.
  2. **Fold into wire types.** Move `Scope`/`Role`/validation *into*
     `pkg/datadrop` (`datadrop.Scope`, `datadrop.Role`), eliminating the
     wire→auth dependency entirely. Mechanically sed-rename
     `auth.Scope`→`datadrop.Scope` etc. across all 27 files.
  3. **Rename the server package.** Keep `hyperslop-cli/pkg/auth` for the
     shared model; rename `go-go-datadrop/pkg/auth` to `pkg/identity` (or split
     into `pkg/secrets` + `pkg/oidc`), and update server files to
     `identity.Principal` etc.
- **Decision:** Option 1 (alias re-export), **subject to explicit user
  confirmation**, because it is the lowest-risk, lowest-churn path and keeps
  the `auth` namespace coherent on both sides. If the user considers the
  re-export an "adapter/shim" disallowed by `AGENT.md`, fall back to Option 2.
- **Rationale:** Option 1 touches ~0 server call-sites (the 27 files keep
  `auth.Scope`), so the risk of a broken authz matrix is minimized during a
  refactor whose goal is *moving*, not *changing*, auth. Option 2 is the most
  conceptually correct (scopes/roles are wire types) and is the recommended
  end-state if the team is willing to do the sed rename and update tests;
  record it as the cleanup follow-up. Option 3 is the most churn for the least
  benefit.
- **Consequences:** Option 1 adds a small `alias.go` in `go-go-datadrop/pkg/auth`
  that must be kept in sync with `hyperslop-cli/pkg/auth`'s exported surface
  (a `go test` that builds the server covers this). `EffectiveRole`/`Authorize`
  in `go-go-datadrop/pkg/auth` use the aliased `Role`/`Scope` and compile
  unchanged. The client device command imports `hyperslop-cli/pkg/auth`
  directly.
- **Status:** proposed (needs user OK re: the alias/shim guideline).

### Decision DR-3: App name, env prefix, and error prefix are parameterized

- **Context:** The shared foundation hardcodes `AppName = "datadrop"`
  (`pkg/cli/build.go`) and `ErrorPrefix = "datadrop: "` (`pkg/cli/exit.go`).
  After the split, the same foundation must serve `hyperslop` (env `HYPERSLOP_*`,
  prefix `hyperslop: `) **and** the `datadrop` admin CLI (env `DATADROP_*`,
  prefix `datadrop: `).
- **Options considered:**
  1. Parameterize: make `AppName`/`ErrorPrefix` configurable on the shared
     foundation; each binary sets them at root-assembly time.
  2. Keep `DATADROP_*` as the connection env vars in both binaries (the env var
     names the backend, not the client), and keep a single hardcoded prefix.
  3. Duplicate the foundation in each repo.
- **Decision:** Option 1.
- **Rationale:** A product called `hyperslop` reading `DATADROP_TOKEN` is
  confusing for the agent users it is being built for. Parameterizing is a small
  one-time refactor (§9.6) and keeps one copy of the foundation (Option 3 is
  the duplication the user explicitly wants to avoid). Option 2 is defensible
  but worse for the new product's identity.
- **Consequences:** `BuildCobraCommand`/`AddCommands`/`ExitOn`/`WithExitCodes`
  and the `ClientSection` help text (which prints `[$HYPERSLOP_ADDR]`) must read
  the configured app name. The `datadrop` admin binary configures the foundation
  with `AppName="datadrop"`/`ErrorPrefix="datadrop: "` before attaching the
  imported customer registrars; `hyperslop` configures `hyperslop`/`"hyperslop: "`.
  The device command's own hardcoded `DATADROP_ADDR`/`DATADROP_TOKEN`
  (`pkg/cli/authcmd/device.go`) must be replaced with the configured app name
  too. Validate with the smoke tests (§11).
- **Status:** proposed.

### Decision DR-4: The admin CLI reuses customer commands by import; the two roots differ

- **Context:** `NewRootCmd`/`Execute` today build one tree with `serve`,
  `healthcheck`, `whoami`, and the five customer groups. After the split there
  are two roots: `hyperslop` (customer only) and `datadrop` (operator +
  re-imported customer).
- **Decision:** Keep two root functions. `hyperslop-cli/pkg/cli/root.go` owns
  `NewHyperslopRootCmd`/`Execute` (customer registrars + `whoami` + logging
  with `AppName="hyperslop"` + customer help). `go-go-datadrop/pkg/cli/root.go`
  keeps `NewRootCmd`/`Execute` (`serve` + `healthcheck` built locally +
  customer registrars imported from `hyperslop-cli` + `whoami` imported from
  `hyperslop-cli` + logging with `AppName="datadrop"` + web-UI help).
- **Rationale:** The roots genuinely differ (operator commands, help pages,
  app name). Sharing them would couple the admin binary to the customer root's
  help and vice-versa. The *registrars* are the shared unit, not the roots.
- **Consequences:** `go-go-datadrop/pkg/cli/root.go` imports
  `hyperslop-cli/pkg/cli` for `AddCommands`/`BuildCobraCommand`/`WithExitCodes`/`NewWhoamiCommand`/`NewClientSection`
  and the five group `Register` funcs. The operator commands are still built
  with a local `buildOperatorCommand` (which can stay local or import the
  shared `WithExitCodes`).
- **Status:** proposed.

### Decision DR-5: Help pages split by audience

- **Context:** `pkg/doc` embeds web-UI topics (operator) and `06-cli-output.md`
  (customer). Both binaries call `AddDocToHelpSystem` from their root.
- **Decision:** `hyperslop-cli/pkg/doc` owns the customer help
  (`06-cli-output.md` plus new agent-facing topics: getting a token with
  `auth device`, env vars, structured output). `go-go-datadrop/pkg/doc` keeps
  the web-UI topics and tutorials. The `datadrop` admin root loads **both**
  (its own web-UI help + the customer help imported from `hyperslop-cli`) so
  `datadrop help cli-output` still resolves; the `hyperslop` root loads only
  its own.
- **Rationale:** Each binary advertises the help relevant to its audience, but
  the admin binary — which can do everything the customer binary can — should
  not lose access to the customer docs.
- **Consequences:** `hyperslop-cli/pkg/doc` must export an
  `AddDocToHelpSystem`-equivalent the admin root can call alongside its own.
  Slug collisions across the two embedded sets must be avoided (the existing
  `doc_test.go` duplicate-slug check should be extended to the union).
- **Status:** proposed.

### Decision DR-6: Binary name, module path, env vars for `hyperslop`

- **Context:** The scaffold is `github.com/go-go-golems/XXX` with `cmd/XXX`.
  The git remote is already `hyperslop-systems/hyperslop-cli`.
- **Decision:** module `github.com/hyperslop-systems/hyperslop-cli`; binary
  `hyperslop` (`cmd/hyperslop`); env prefix `HYPERSLOP` (`HYPERSLOP_ADDR`,
  `HYPERSLOP_TOKEN`, `HYPERSLOP_LOG_LEVEL`); error prefix `hyperslop: `.
- **Rationale:** Consistent product identity; matches the existing remote.
- **Consequences:** Phase 0 renames the module, the `cmd/XXX` dir, the
  `.goreleaser` `project_name`/`binary`/`main`, the Makefile `VERSION`/targets,
  and `pkg/logcopter.go`'s `logcopter.Package("…")` area string. The
  `ddp_` token prefix and `/v1` API are unchanged (they are the backend's
  contract, not the client's brand).
- **Status:** proposed.

### Decision DR-7: Device flow — keep raw HTTP or route through `pkg/client`

- **Context:** `pkg/cli/authcmd/device.go` uses a raw `http.Client` to hit
  `/v1/device/authorizations` and `/v1/device/tokens`, with its own
  `apiProblem` error type and its own `envOr("DATADROP_ADDR", …)`. It does not
  use `pkg/client.Client` (which requires a token the device flow is trying to
  *obtain*).
- **Decision:** Keep the device flow on raw HTTP, but move the two device
  endpoints into `pkg/client` as unauthenticated methods (`StartDeviceAuthorization`,
  `PollDeviceToken`) so the client owns all HTTP to the backend and the
  command's `apiProblem` can become a typed `*client.APIError`. Replace the
  hardcoded `DATADROP_ADDR` with the configured app name (DR-3).
- **Rationale:** Consolidating HTTP into `pkg/client` is the existing design
  rule ("if `datadrop push` works, `curl` works"). The device endpoints are the
  one place the client currently reaches around itself. Adding two
  unauthenticated methods is small and removes a duplicated error type.
- **Consequences:** `pkg/client` gains `StartDeviceAuthorization(ctx, req)` and
  `PollDeviceToken(ctx, req)` (both use `doJSON` with no `Authorization`
  header, which `Client` already omits when `Token == ""`). The command becomes
  a thin orchestrator over `client.StartDeviceAuthorization` + `PollDeviceToken`
  + the existing polling loop. The `apiProblem`/`postJSON` helpers are deleted.
- **Status:** proposed.

### Decision DR-8: Versioning and release of `hyperslop-cli` vs `go-go-datadrop`

- **Context:** The two modules now share a library (`hyperslop-cli`) that the
  server depends on. Releases must not desync.
- **Decision:** `hyperslop-cli` is released independently (GoReleaser, Homebrew
  via the go-go-golems OIDC/Vault publishing — the scaffold already has
  `.goreleaser.yaml`). `go-go-datadrop`'s `go.mod` pins a
  `hyperslop-cli` **released version**; during development the `go.work`
  workspace uses a local replace so both can be edited together. Cut a
  `hyperslop-cli` tag before bumping `go-go-datadrop`'s `go.mod` off the local
  replace.
- **Rationale:** Keeps the customer binary independently shippable to agents
  while preventing the server from building against an untagged library.
- **Consequences:** The Phase plan (§10) ends with a `hyperslop-cli` release,
  then a `go-go-datadrop` `go.mod` bump. The `go-go-golems-project-setup`
  conventions (Makefile, golangci-lint pin, lefthook, GitHub Actions, GoReleaser)
  already exist in the scaffold and apply unchanged.
- **Status:** proposed.

## 8. API references (contracts the intern must preserve)

### 8.1 HTTP API surface (the wire contract)

All endpoints are under the server's base URL (`--addr` / `HYPERSLOP_ADDR`).
Authn is `Authorization: Bearer <ddp_ token>`; the device endpoints and
public-read reads are unauthenticated. Errors are problem documents
(`{"code","detail","request_id","errors"}`).

| Method | Path | Client method | Command |
|---|---|---|---|
| POST | `/v1/drops` | `CreateDrop` | `create` |
| GET | `/v1/drops` | `ListDrops` | `list` |
| GET | `/v1/drops/{drop}` | `GetDrop` | `inspect` |
| POST | `/v1/drops/{drop}/events` | `Push` | `push` |
| GET | `/v1/drops/{drop}/events` | `Query` | `query`, `range` |
| GET | `/v1/drops/{drop}/events/stream` | `Stream` (SSE) | `tail` |
| GET | `/v1/drops/{drop}/export` | `Export` | `export` |
| PUT | `/v1/drops/{drop}/schemas/{stream}` | `PutSchema` | `schema put` |
| GET | `/v1/drops/{drop}/schemas/{stream}` | `GetSchema` | `schema show` |
| GET | `/v1/drops/{drop}/datasets` | `ListDatasets` | `dataset list` |
| GET | `/v1/drops/{drop}/datasets/{dataset}` | `GetDataset` | `dataset show` |
| GET | `…/datasets/{dataset}/versions/{v}` | `GetDatasetVersion` | `dataset show` |
| POST | `…/datasets/{dataset}/versions` | `OpenDatasetVersion` | `dataset push` (internal) |
| POST | `…/versions/{v}/commit` | `CommitDatasetVersion` | `dataset push` (internal) |
| DELETE | `…/versions/{v}` | `DeleteDatasetVersion` | `dataset rm` |
| PUT | `…/versions/{v}/files/{path}?digest=` | `UploadDatasetFile`/`MountDatasetFile` | `dataset push` (internal) |
| GET | `…/versions/{v}/files/{path}` | `DownloadDatasetFile` | `dataset get` |
| GET | `…/versions/{v}/archive` | `DownloadDatasetArchive` | `dataset get` |
| POST | `…/versions/{v}/import` | `ImportDataset` | `dataset import` |
| HEAD | `/v1/blobs/{digest}` | `BlobExists` | `dataset push` (internal) |
| POST | `/v1/blobs/gc` | `GarbageCollect` | `dataset gc` |
| POST | `/v1/device/authorizations` | `StartDeviceAuthorization` (new, DR-7) | `auth device` |
| POST | `/v1/device/tokens` | `PollDeviceToken` (new, DR-7) | `auth device` |
| GET | `/v1/me` | `Whoami` | `whoami` |
| GET | `/healthz` | (raw, `healthcheck` only) | `healthcheck` (operator) |

### 8.2 `pkg/client` method reference (preserved, plus DR-7 additions)

```go
// pkg/client/client.go (sketch — preserve these signatures exactly)
func New(baseURL, token string) (*Client, error)
type APIError struct { Status int; Code, Detail, RequestID string; Errors []datadrop.Violation }

func (c *Client) CreateDrop(ctx, datadrop.CreateDropRequest) (datadrop.Drop, error)
func (c *Client) ListDrops(ctx) ([]datadrop.Drop, error)
func (c *Client) GetDrop(ctx, drop string) (datadrop.DropStats, error)
func (c *Client) Push(ctx, drop, stream string, body json.RawMessage, envelope bool) (datadrop.AppendResult, error)
func (c *Client) Query(ctx, datadrop.EventQuery) (datadrop.QueryResult, error)
func (c *Client) PutSchema(ctx, drop, stream string, spec json.RawMessage, mode datadrop.Mode) (datadrop.PutSchemaResult, error)
func (c *Client) GetSchema(ctx, drop, stream string) (datadrop.Schema, error)
func (c *Client) Export(ctx, drop, format string, q datadrop.EventQuery) (io.ReadCloser, error)
func (c *Client) Stream(ctx, drop, stream string, after int64) (<-chan StreamEvent, <-chan error, error)

// pkg/client/me.go
func (c *Client) Whoami(ctx) (Me, error)

// pkg/client/datasets.go (subset)
func (c *Client) ListDatasets(ctx, drop string) ([]datadrop.Dataset, error)
func (c *Client) GetDatasetVersion(ctx, drop, dataset, version string) (datadrop.DatasetVersion, error)
func (c *Client) PushDataset(ctx, drop, dataset string, files []PushFile, req datadrop.CommitVersionRequest) (PushResult, error)
func (c *Client) DownloadDatasetFile(ctx, drop, dataset, version, logicalPath string) (io.ReadCloser, error)
func (c *Client) DownloadDatasetArchive(ctx, drop, dataset, version string) (io.ReadCloser, error)
func (c *Client) ImportDataset(ctx, drop, dataset, version, logicalPath, stream, format string, maxRows int, strict bool) (datadrop.ImportResult, error)
func (c *Client) GarbageCollect(ctx, minAgeSeconds int) (GCResult, error)
func HashFile(path string) (digest string, size int64, err error)
func VerifyFile(path, expected string) error

// DR-7 additions (unauthenticated; Token == "" → no Authorization header)
func (c *Client) StartDeviceAuthorization(ctx, datadrop.StartDeviceAuthorizationRequest) (datadrop.StartDeviceAuthorizationResponse, error)
func (c *Client) PollDeviceToken(ctx, datadrop.PollDeviceTokenRequest) (datadrop.DeviceTokenResponse, error)
```

### 8.3 `pkg/cli` shared foundation reference (after parameterization, DR-3)

```go
// pkg/cli/build.go (parameterized)
func SetAppName(name string)              // call once at root assembly; sets env prefix + section help
func AppName() string
func BuildCobraCommand(cmd cmds.Command) (*cobra.Command, error)   // uses AppName(), WithExitCodes
func AddCommands(parent *cobra.Command, builders ...Builder) error
type Registrar func(root *cobra.Command) error
type Builder func() (cmds.Command, error)

// pkg/cli/section.go
const ClientSectionSlug = "datadrop-client"   // keep the slug (it's an internal section id, not user-facing)
const DefaultAddr = "http://localhost:8080"
func NewClientSection() (schema.Section, error)  // help text reads AppName() → "[$HYPERSLOP_ADDR]"
func ClientFrom(vals *values.Values) (*client.Client, error)
func ClientSettingsFrom(vals *values.Values) (*ClientSettings, error)

// pkg/cli/exit.go
const ( ExitOK=0; ExitError=1; ExitUsage=2; ExitAuth=3; ExitNotFound=4; ExitValidation=5 )
func SetErrorPrefix(prefix string)          // "hyperslop: " or "datadrop: "
func ExitOn(err error) error
func WithExitCodes(cmd cmds.Command) cmds.Command

// pkg/cli/rows.go (unchanged signatures)
func RowForDrop(datadrop.Drop) types.Row
func RowForPrincipal(server string, client.Me) types.Row
// … one projection per response type …

// pkg/cli/whoami.go
func NewWhoamiCommand() (cmds.Command, error)
```

### 8.4 The exit-code contract (must not change)

| Code | Meaning | When |
|---|---|---|
| 0 | OK | success |
| 1 | Error | any failure not below |
| 2 | Usage | (reserved; cobra usage errors) |
| 3 | Auth | HTTP 401/403 from `*client.APIError` |
| 4 | NotFound | HTTP 404 |
| 5 | Validation | HTTP 400/409/413/422 |

`cmd/datadrop/*_smoke_test.go` and `pkg/cli/exit_test.go` assert these. They
must pass unchanged after the move (the codes are numbers scripts branch on).

### 8.5 The env-var contract (after DR-3)

| Variable | Used by | Means |
|---|---|---|
| `HYPERSLOP_ADDR` | `hyperslop` client verbs | server base URL |
| `HYPERSLOP_TOKEN` | `hyperslop` client verbs | bearer `ddp_` token |
| `HYPERSLOP_LOG_LEVEL` | `hyperslop` root | zerolog level |
| `DATADROP_ADDR` | `datadrop` admin client verbs | server base URL (admin reuse) |
| `DATADROP_TOKEN` | `datadrop` admin client verbs | bearer token |
| `DATADROP_LOG_LEVEL` | `datadrop` root | zerolog level |
| `DATADROP_HEALTH_URL` | `healthcheck` | probe target |
| `DATADROP_EXTERNAL_URL`, `DATADROP_OIDC_*`, `DATADROP_DEVICE_CODE_PEPPER_FILE`, `DATADROP_TRUSTED_PROXY_CIDRS` | `serve` | server/operator config (unchanged) |

## 9. Key flows and pseudocode

### 9.1 `hyperslop` command wiring (`hyperslop-cli/cmd/hyperslop/main.go`)

```go
package main

import "os"
import "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
import "github.com/hyperslop-systems/hyperslop-cli/pkg/cli/authcmd"
import "github.com/hyperslop-systems/hyperslop-cli/pkg/cli/dataset"
import "github.com/hyperslop-systems/hyperslop-cli/pkg/cli/drops"
import "github.com/hyperslop-systems/hyperslop-cli/pkg/cli/events"
import "github.com/hyperslop-systems/hyperslop-cli/pkg/cli/schemacmd"

func main() {
    // The customer binary. No serve, no healthcheck.
    os.Exit(cli.Execute(
        authcmd.Register,
        drops.Register,
        events.Register,
        schemacmd.Register,
        dataset.Register,
    ))
}
```

`hyperslop-cli/pkg/cli/root.go`:

```go
func NewHyperslopRootCmd(registrars ...Registrar) (*cobra.Command, error) {
    cli.SetAppName("hyperslop")      // DR-3: env prefix HYPERSLOP_*, section help
    cli.SetErrorPrefix("hyperslop: ")
    root := &cobra.Command{Use: "hyperslop", Short: "Agent/customer CLI for the datadrop backend",
        SilenceUsage: true, SilenceErrors: true,
        PersistentPreRunE: func(cmd *cobra.Command, _ []string) error { return logging.InitLoggerFromCobra(cmd) }}
    if err := cli.AddCommands(root, cli.NewWhoamiCommand); err != nil { return nil, err }
    for _, r := range registrars { if err := r(root); err != nil { return nil, err } }
    helpSystem := help.NewHelpSystem()
    if err := doc.AddDocToHelpSystem(helpSystem); err != nil { return nil, err }
    help_cmd.SetupCobraRootCommand(helpSystem, root)
    if err := logging.AddLoggingSectionToRootCommand(root, "hyperslop"); err != nil { return nil, err }
    return root, nil
}

func Execute(registrars ...Registrar) int {
    root, err := NewHyperslopRootCmd(registrars...)
    if err != nil { fmt.Fprintln(os.Stderr, "hyperslop: "+err.Error()); return cli.ExitError }
    if err := root.Execute(); err != nil { fmt.Fprintln(os.Stderr, cli.ErrorPrefix()+err.Error()); return cli.ExitError }
    return cli.ExitOK
}
```

### 9.2 `datadrop` admin command wiring (after extraction)

```go
// go-go-datadrop/cmd/datadrop/main.go
package main

import "os"
import "github.com/go-go-golems/go-go-datadrop/pkg/cli"   // local: admin root + serve + healthcheck
import "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"            // foundation (imported via local pkg/cli)
import hapiauthcmd "github.com/hyperslop-systems/hyperslop-cli/pkg/cli/authcmd"
import hapidataset "github.com/hyperslop-systems/hyperslop-cli/pkg/cli/dataset"
import hapidrops "github.com/hyperslop-systems/hyperslop-cli/pkg/cli/drops"
import hapievents "github.com/hyperslop-systems/hyperslop-cli/pkg/cli/events"
import hapischema "github.com/hyperslop-systems/hyperslop-cli/pkg/cli/schemacmd"

func main() {
    os.Exit(cli.Execute(                       // admin Execute (datadrop prefix, serve+healthcheck)
        hapiauthcmd.Register,
        hapidrops.Register,
        hapievents.Register,
        hapischema.Register,
        hapidataset.Register,
    ))
}
```

`go-go-datadrop/pkg/cli/root.go` (admin) sets `hypcli.SetAppName("datadrop")`,
`hypcli.SetErrorPrefix("datadrop: ")`, attaches local `serve`/`healthcheck`
via a local `buildOperatorCommand`, attaches `whoami` and the five groups via
the imported `hypcli.AddCommands`/registrars, loads its own web-UI help **and**
`hapidoc.AddDocToHelpSystem` (DR-5), and installs logging with `"datadrop"`.

### 9.3 A `GlazeCommand` end-to-end (`create`)

```go
// hyperslop-cli/pkg/cli/drops/create.go (after move; only import paths change)
func (c *CreateCommand) RunIntoGlazeProcessor(ctx, vals, gp) error {
    s := &createSettings{}
    if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil { return err }
    api, err := ddcli.ClientFrom(vals)                 // decodes HYPERSLOP_ADDR/HYPERSLOP_TOKEN section
    if err != nil { return err }
    created, err := api.CreateDrop(ctx, datadrop.CreateDropRequest{Name: s.Name, Retention: s.Retention, PublicRead: s.PublicRead})
    if err != nil { return err }                        // WithExitCodes maps *client.APIError → exit code
    return gp.AddRow(ctx, ddcli.RowForDrop(created))    // row contract: name, created_at, retention, public_read, owner_id, your_role
}
```

### 9.4 A `BareCommand` — device pairing (after DR-7)

```go
// hyperslop-cli/pkg/cli/authcmd/device.go (after refactor)
func (c *DeviceCommand) Run(ctx, vals) error {
    s := decode(vals)
    scopes, _ := parseScopes(s.Scopes)
    if err := auth.ValidateDeviceScopes(scopes); err != nil { return err }   // hyperslop-cli/pkg/auth
    api, err := client.New(s.Addr, "")                                        // no token yet
    if err != nil { return err }
    started, err := api.StartDeviceAuthorization(ctx, datadrop.StartDeviceAuthorizationRequest{
        Name: s.Name, Scopes: scopes, ExpiresIn: s.ExpiresIn})
    if err != nil { return err }
    fmt.Fprintf(os.Stderr, "Open %s\nCode: %s\nWaiting…\n", started.VerificationURIComplete, started.UserCode)
    token, err := poll(ctx, api, started)                                     // loops api.PollDeviceToken; AuthorizationPending/SlowDown/RateLimited/ExpiredToken
    if err != nil { return err }
    if s.CredentialFile != "" { return writeCredentialFile(s.CredentialFile, token.Token) }  // 0600
    _, err = fmt.Fprintln(os.Stdout, token.Token)                             // capture: export HYPERSLOP_TOKEN="$(hyperslop auth device …)"
    return err
}
```

### 9.5 A `WriterCommand` — `export`

```go
// hyperslop-cli/pkg/cli/events/export.go (after move)
func (c *ExportCommand) RunIntoWriter(ctx, vals, w) error {
    api, err := ddcli.ClientFrom(vals); if err != nil { return err }
    rc, err := api.Export(ctx, drop, format, queryFromVals(vals)); if err != nil { return err }
    defer rc.Close()
    _, err = io.Copy(w, rc)   // server-formatted CSV/NDJSON/JSON bytes streamed straight to --output
    return err
}
```

### 9.6 Parameterized `BuildCobraCommand` (DR-3)

```go
// hyperslop-cli/pkg/cli/build.go
var appName = "hyperslop" // default; set by each root before building commands

func SetAppName(name string) { appName = name }
func AppName() string        { return appName }

func BuildCobraCommand(command cmds.Command) (*cobra.Command, error) {
    return cli.BuildCobraCommandFromCommand(
        WithExitCodes(command),
        cli.WithParserConfig(cli.CobraParserConfig{
            ShortHelpSections: []string{schema.DefaultSlug, ClientSectionSlug},
            AppName:           appName,                 // <-- was hardcoded "datadrop"
        }))
}
```

`NewClientSection` builds `--addr`/`--token` with help strings that interpolate
`strings.ToUpper(AppName())` → `[$HYPERSLOP_ADDR]` / `[$DATADROP_ADDR]`.

## 10. Implementation plan (phased, file-level)

Do these in order. After each phase, `go build ./...` and `go test ./...` (with
`GOWORK=off` where the Makefile says so) must pass inside the workspace before
moving on. Commit per phase.

### Phase 0 — Rename the `hyperslop-cli` scaffold

- `hyperslop-cli/go.mod`: `module github.com/hyperslop-systems/hyperslop-cli`.
- Rename `cmd/XXX/` → `cmd/hyperslop/`; put a real (empty) `main.go` there for
  now.
- `pkg/logcopter.go`: `logcopter.Package("hyperslop-systems.hyperslop-cli.pkg")`;
  regenerate with `go generate ./...`.
- `.goreleaser.yaml`: `project_name: hyperslop`, `main: ./cmd/hyperslop`,
  `binary: hyperslop`, build ids `hyperslop-linux`/`hyperslop-darwin`.
- `Makefile`: update `VERSION`, any `XXX` references.
- `go.work` already links `./hyperslop-cli`; verify `go build ./...` in the
  workspace.
- **Validate:** `cd hyperslop-cli && go build ./... && go test ./...`.

### Phase 1 — Move the wire types, scope/role model, and tabular

- Move `go-go-datadrop/pkg/datadrop/` → `hyperslop-cli/pkg/datadrop/` (all
  files). Update internal imports: `go-go-datadrop/pkg/auth` →
  `hyperslop-cli/pkg/auth` (next bullet).
- Create `hyperslop-cli/pkg/auth/` with `scope.go` (verbatim), the **type-model
  part** of `role.go` (`Role`, constants, `AssignableRoles`, `rank`, `AtLeast`,
  `Valid`, `ParseRole`), and a new `device-validate.go` containing
  `ValidateDeviceScopes` (moved out of `device.go`). This package must be a
  leaf: no imports of `pkg/cli`, `pkg/client`, or anything server-side.
- Move `go-go-datadrop/pkg/tabular/` → `hyperslop-cli/pkg/tabular/`; update its
  `pkg/datadrop` import to `hyperslop-cli/pkg/datadrop`.
- In `go-go-datadrop`, delete the moved files. Apply DR-2 (alias re-export):
  add `go-go-datadrop/pkg/auth/alias.go` re-exporting `Scope`, `Role`,
  `ScopeSet`, `AllScopes`, `NewScopeSet`, `FullScopeSet`, `ParseScopes`,
  `ValidateScopes`, `ValidateDeviceScopes`, `ParseRole`, `AssignableRoles`
  from `hyperslop-cli/pkg/auth`. Keep `principal.go`, `token.go`, `oidc.go`,
  the crypto half of `device.go`, and `EffectiveRole`/`Authorize`/`DropACL` in
  `role.go`; update their imports to use the aliased types (they already write
  `auth.Scope`, so with the aliases they compile unchanged).
- Update `go-go-datadrop` server/store import paths: every
  `github.com/go-go-golems/go-go-datadrop/pkg/datadrop` →
  `github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop`, and
  `…/pkg/tabular` → `github.com/hyperslop-systems/hyperslop-cli/pkg/tabular`
  (mechanical sed across `pkg/server`, `pkg/store`, `pkg/schema`, `pkg/stream`,
  `cmd/datadrop/*_test.go`).
- Add `require github.com/hyperslop-systems/hyperslop-cli v0.x.x` to
  `go-go-datadrop/go.mod` (workspace replace keeps it local for now).
- **Validate:** `go build ./...` and `go test ./...` in **both** modules
  (workspace). The server's authz tests (`pkg/server/authz_test.go`,
  `pkg/auth/auth_test.go`) must pass — they prove the alias re-export works.

### Phase 2 — Move the client (+ DR-7 device methods)

- Move `go-go-datadrop/pkg/client/` → `hyperslop-cli/pkg/client/`; update
  `pkg/datadrop` import to `hyperslop-cli/pkg/datadrop`.
- Add `StartDeviceAuthorization` and `PollDeviceToken` to
  `hyperslop-cli/pkg/client/client.go` (DR-7), unauthenticated, returning
  `datadrop.StartDeviceAuthorizationResponse` / `datadrop.DeviceTokenResponse`
  and a typed `*APIError` (handle `AuthorizationPending`/`SlowDown`/
  `RateLimited`/`ExpiredToken` via the error `Code`).
- In `go-go-datadrop`, delete `pkg/client/`. Update the only importers
  (`pkg/cli/*`) — but those move in Phase 3/4, so do this phase together with
  Phase 3 to avoid a broken intermediate state. (Practically: land Phase 2 and
  Phase 3 in one commit.)
- **Validate:** `hyperslop-cli` client tests (`pkg/client/client_test.go`)
  pass after import-path updates.

### Phase 3 — Move the shared CLI foundation and parameterize (DR-3)

- Move `go-go-datadrop/pkg/cli/{section,build,exit,rows,fields,whoami}.go` and
  `exit_test.go`/`rows_test.go` → `hyperslop-cli/pkg/cli/`.
- Parameterize: convert `AppName` const → `var` + `SetAppName`/`AppName`;
  convert `ErrorPrefix` const → `var` + `SetErrorPrefix`/`ErrorPrefix`;
  `NewClientSection` interpolates the env name from `AppName()`.
- In `go-go-datadrop/pkg/cli/`, keep `root.go` (admin), `serve.go`,
  `healthcheck.go`, and a trimmed `build.go` (`buildOperatorCommand`,
  `addOperatorCommands`, and an `envOr` if not already present). Import the
  foundation from `hyperslop-cli/pkg/cli` for `AddCommands`, `BuildCobraCommand`,
  `WithExitCodes`, `NewClientSection`, `ClientFrom`, row projections,
  `NewWhoamiCommand`. The operator `buildOperatorCommand` still skips
  `AppName` (it must not read `DATADROP_*`/`HYPERSLOP_*`).
- **Validate:** `go build ./...` both modules; `pkg/cli/exit_test.go` and
  `rows_test.go` pass in `hyperslop-cli`.

### Phase 4 — Move the command groups + help (DR-5, DR-7)

- Move `pkg/cli/{authcmd,drops,events,dataset,schemacmd}/` →
  `hyperslop-cli/pkg/cli/…`. Update each file's imports:
  `ddcli "github.com/go-go-golems/go-go-datadrop/pkg/cli"` →
  `ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"`;
  `…/pkg/datadrop` → `hyperslop-cli/pkg/datadrop`;
  `…/pkg/client` → `hyperslop-cli/pkg/client`;
  `…/pkg/auth` → `hyperslop-cli/pkg/auth`.
- Refactor `authcmd/device.go` per DR-7: use `client.New(addr, "")` +
  `StartDeviceAuthorization`/`PollDeviceToken`; delete `apiProblem`/`postJSON`;
  replace `envOr("DATADROP_ADDR", …)` with the configured app name
  (`ddcli.AppName()` → `HYPERSLOP_ADDR`). Keep the polling loop and the
  `0600` credential-file behavior.
- Move `pkg/doc/topics/06-cli-output.md` → `hyperslop-cli/pkg/doc/topics/`;
  create `hyperslop-cli/pkg/doc/doc.go` with its own `AddDocToHelpSystem`.
  Add agent-facing topics (e.g. `02-getting-a-token.md`,
  `03-env-and-structured-output.md`).
- In `go-go-datadrop`, remove the moved groups from `pkg/cli`; remove
  `06-cli-output.md` from its `pkg/doc/topics`. Export
  `hyperslop-cli/pkg/doc.AddDocToHelpSystem` so the admin root can load both.
- **Validate:** `go build ./...` both modules; the command-group tests
  (`drops/push_test.go`, `dataset/get_test.go`, `authcmd/device_test.go`) pass
  in `hyperslop-cli`.

### Phase 5 — Wire `hyperslop` main and root

- Write `hyperslop-cli/cmd/hyperslop/main.go` (§9.1) and
  `hyperslop-cli/pkg/cli/root.go` (`NewHyperslopRootCmd`/`Execute`, §9.1).
- Replace the placeholder `cmd/XXX` and `pkg/doc.go`/`pkg/logcopter.go` init
  stubs as needed.
- **Validate:** `go run ./cmd/hyperslop --help` lists `whoami`, `auth`,
  `create`, `list`, `inspect`, `push`, `query`, `tail`, `export`, `schema`,
  `dataset` — and **not** `serve`/`healthcheck`. `go run ./cmd/hyperslop
  whoami --addr http://localhost:8080` works against a running server.

### Phase 6 — Rewire `go-go-datadrop` admin CLI (DR-4)

- Rewrite `go-go-datadrop/cmd/datadrop/main.go` (§9.2) to import the five
  registrars + `whoami` from `hyperslop-cli` and keep local `serve`/
  `healthcheck`.
- Rewrite `go-go-datadrop/pkg/cli/root.go` (admin): `SetAppName("datadrop")`,
  `SetErrorPrefix("datadrop: ")`, attach operator commands locally, attach
  customer commands via imported `hypcli.AddCommands`/registrars, load web-UI
  help + `hapidoc.AddDocToHelpSystem`, install logging with `"datadrop"`.
- **Validate:** `go run ./cmd/datadrop --help` lists **both** `serve`,
  `healthcheck` **and** the full customer surface. `datadrop whoami` and
  `datadrop create …` work exactly as before (same flags, same rows, same exit
  codes). The smoke tests in `cmd/datadrop/*_smoke_test.go` pass.

### Phase 7 — Server-side import-path hygiene and the auth alias test

- Grep for any remaining `go-go-datadrop/pkg/{datadrop,client,tabular}` imports
  in `go-go-datadrop` and fix them. `pkg/stream` stays (server-only).
- Add a compile test in `go-go-datadrop/pkg/auth` that asserts the alias surface
  matches `hyperslop-cli/pkg/auth` (or rely on the existing authz tests).
- Run `golangci-lint run -v` and `govulncheck ./...` in both modules.
- **Validate:** `go test ./...` green in both modules; `make ci-check` green.

### Phase 8 — Tests, smoke tests, CI

- Port the end-to-end CLI smoke tests (`cmd/datadrop/smoke_test.go`,
  `table_smoke_test.go`, `dataset_smoke_test.go`, `device_smoke_test.go`,
  `tree_test.go`, `test_auth_test.go`) to `hyperslop-cli/cmd/hyperslop/`
  (rename `datadrop` → `hyperslop`, `DATADROP_*` → `HYPERSLOP_*`). The admin
  `datadrop` smoke tests stay and continue to assert the customer verbs work
  through the imported registrars.
- Add CI workflows to `hyperslop-cli/.github/workflows` (unit, smoke, lint,
  security) per `go-go-golems-project-setup`.
- **Validate:** both modules' CI is green; the exit-code contract tests pass
  in both binaries.

### Phase 9 — Release plumbing (DR-8)

- Tag `hyperslop-cli` v0.1.0; GoReleaser builds `hyperslop` for linux/darwin
  amd64/arm64 (the `.goreleaser.yaml` already has the CGO/cross-compile matrix).
- Bump `go-go-datadrop/go.mod` from the workspace replace to the tagged
  `hyperslop-cli` version; `go mod tidy`.
- Update `go-go-datadrop/README.md` to document that the customer verbs are now
  imported from `hyperslop-cli`, and point agents at the `hyperslop` binary.
- **Validate:** `go build ./...` in `go-go-datadrop` with `GOWORK=off` (i.e.
  against the released `hyperslop-cli`) is green.

## 11. Test and validation strategy

1. **Unit tests travel with their packages.** `pkg/client/client_test.go`,
   `pkg/cli/{exit,rows}_test.go`, `pkg/cli/drops/push_test.go`,
   `pkg/cli/dataset/get_test.go`, `pkg/cli/authcmd/device_test.go`,
   `pkg/datadrop/datadrop_test.go`, `pkg/tabular/*_test.go` all move to
   `hyperslop-cli` and must pass there with updated import paths.
2. **Exit-code contract.** `pkg/cli/exit_test.go` and the smoke tests assert the
   0/1/2/3/4/5 mapping. Run them in **both** binaries; the codes are numbers
   scripts branch on and must not change.
3. **Row-shape contract.** `pkg/cli/rows_test.go` pins the exact key set of
   every projection. It is a change detector; it must pass unchanged, proving
   the row API did not drift across the move.
4. **Authz matrix.** `go-go-datadrop/pkg/auth/auth_test.go` and
   `pkg/server/authz_test.go` exhaustively exercise `EffectiveRole`/`Authorize`.
   They must pass after DR-2's alias re-export, proving the moved scope/role
   model did not change behavior.
5. **End-to-end smoke.** Start `datadrop serve` in a tmux, run the `hyperslop`
  binary against it, and exercise `auth device → create → push → query → tail
  → export → dataset push/get → schema put/show → whoami`. Then do the same
  with the `datadrop` admin binary and confirm identical output and exit codes.
6. **Dependency-direction invariant.** `go list -deps github.com/hyperslop-systems/hyperslop-cli/...`
   must **not** contain `github.com/go-go-golems/go-go-datadrop`. Add a CI test
   that fails if `hyperslop-cli` ever imports `go-go-datadrop` (this is the
   cycle-prevention guard).
7. **Binary-size sanity check.** Compare `hyperslop` binary size vs `datadrop`;
   `hyperslop` should be markedly smaller (no SQLite/OIDC/web-UI). Not a gate,
   but a sanity signal that the extraction actually shed server deps.
8. **`docmgr doctor --ticket HYPERSLOP-1 --stale-after 30`** passes (this
   ticket's own bookkeeping).

## 12. Risks, alternatives, open questions

### Risks

- **Import cycle introduced by accident.** The single most important invariant
  is that `hyperslop-cli` never imports `go-go-datadrop`. The CI guard in §11.6
  exists for this. The risk is highest if someone later adds a "talk to the
  server's admin API" feature to the client and reaches for a server type;
  keep `pkg/datadrop` a pure leaf.
- **Auth alias drift (DR-2 Option 1).** If `hyperslop-cli/pkg/auth` exports a
  new scope/role symbol and `go-go-datadrop/pkg/auth/alias.go` is not updated,
  the server cannot see it. Mitigate with a compile test that reflects the
  exported surface, or adopt Option 2 (fold into `pkg/datadrop`) as a follow-up
  to remove the alias entirely.
- **Env-var migration for existing users.** Operators with `DATADROP_TOKEN`
  exported who switch to the `hyperslop` binary will need `HYPERSLOP_TOKEN`.
  This is intended (DR-3) but must be documented prominently. The admin
  `datadrop` binary still reads `DATADROP_*`, so operators are unaffected.
- **Release ordering.** `go-go-datadrop` cannot build against a tagged
  `hyperslop-cli` until one exists. Phase 9 ordering handles this; during
  development the workspace replace covers it.
- **Help slug collisions.** Loading both `hyperslop-cli/pkg/doc` and
  `go-go-datadrop/pkg/doc` into the admin root can collide on slugs. Extend the
  duplicate-slug test to the union.

### Alternatives (recorded, not chosen)

- **DR-1 Option 2 (third `hyperslop-api` module).** Cleanest separation: the
  server would depend on a slim `hyperslop-api` for wire types/scope/role/client
  rather than on a CLI repo. Adopt this if the team later wants to release the
  wire protocol independently or stop the server from depending on a CLI
  repository. It is a strict superset of this plan's Phase 1 (the moved packages
  would go to `hyperslop-api` instead of `hyperslop-cli`, and both binaries
  would depend on `hyperslop-api`).
- **DR-2 Option 2 (fold Scope/Role into `pkg/datadrop`).** The most conceptually
  correct end state. Recommended as a cleanup follow-up after this ticket lands:
  `sed` `auth.Scope`→`datadrop.Scope`, `auth.Role`→`datadrop.Role` across the
  27 files, delete the alias file, and make `pkg/datadrop` a true leaf.
- **DR-3 Option 2 (keep `DATADROP_*` everywhere).** Lower friction, worse
  product identity. Revisit if agent users turn out to be primarily existing
  datadrop operators.

### Open questions

1. **Confirm DR-1 and DR-2 against the `AGENT.md` "no adapters/shim"
   guideline.** The plan assumes moving wire types into `hyperslop-cli`
   (DR-1) and an alias re-export in `go-go-datadrop/pkg/auth` (DR-2 Option 1)
   are acceptable. If the user considers the alias a shim, switch to DR-2
   Option 2 (fold into `pkg/datadrop`). Both are specified well enough to
   execute either way.
2. **Should the admin `datadrop` binary keep the `auth device` verb?** It is a
   customer verb, imported via the registrar. Keeping it is free (no
   duplication) and useful for operators testing device flow. Recommend yes.
3. **Config-file/profile support.** The client section is already a glazed
   section that can be filled from a config file. This ticket does not add
   profile support, but the move is the natural moment to document it. Out of
   scope unless requested.
4. **`pbui` repo in the workspace.** The `split-cli` WSM workspace also
   contains `pbui` (the web workbench). It is unrelated to this extraction;
   leave it alone.

## 13. References (key files)

All `go-go-datadrop` paths are under
`/home/manuel/workspaces/2026-07-29/split-cli/go-go-datadrop` (worktree) or
`/home/manuel/code/wesen/go-go-golems/go-go-datadrop` (canonical). All
`hyperslop-cli` paths are under
`/home/manuel/workspaces/2026-07-29/split-cli/hyperslop-cli`.

**Entrypoint and wiring**

- `go-go-datadrop/cmd/datadrop/main.go` — current entrypoint; names the five registrars.
- `go-go-datadrop/pkg/cli/root.go` — `NewRootCmd`, `Execute`, logging section, help setup.
- `go-go-datadrop/pkg/cli/build.go` — `BuildCobraCommand`, `buildOperatorCommand`, `AddCommands`, `Registrar`, `AppName`.

**Shared CLI foundation (moves to hyperslop-cli)**

- `go-go-datadrop/pkg/cli/section.go` — `ClientSection`, `ClientFrom`, `ClientSettingsFrom`.
- `go-go-datadrop/pkg/cli/exit.go` — exit codes, `ExitOn`, `WithExitCodes`, `ErrorPrefix`.
- `go-go-datadrop/pkg/cli/rows.go` — row projections (depends on `pkg/tabular`, `pkg/client`, `pkg/datadrop`).
- `go-go-datadrop/pkg/cli/fields.go` — `DropStreamField`, `ReadSpec`, `HumanBytes`.
- `go-go-datadrop/pkg/cli/whoami.go` — `WhoamiCommand`.

**Customer command groups (move to hyperslop-cli)**

- `go-go-datadrop/pkg/cli/authcmd/{device.go,root.go}` — device pairing.
- `go-go-datadrop/pkg/cli/drops/{create,list,inspect,push,root}.go`
- `go-go-datadrop/pkg/cli/events/{query,tail,export,range,root}.go`
- `go-go-datadrop/pkg/cli/dataset/{push,list,show,get,import,rm,gc,root}.go`
- `go-go-datadrop/pkg/cli/schemacmd/{put,show,root}.go`

**Client and wire types (move to hyperslop-cli)**

- `go-go-datadrop/pkg/client/client.go` — typed HTTP client, `APIError`, `Stream`, `parseSSE`.
- `go-go-datadrop/pkg/client/me.go` — `Whoami`, `Me`.
- `go-go-datadrop/pkg/client/datasets.go` — dataset client, `PushDataset`, `HashFile`, `VerifyFile`.
- `go-go-datadrop/pkg/datadrop/{drop,event,query,schema,dataset,device,account}.go` — wire types.
- `go-go-datadrop/pkg/tabular/{infer,project,table}.go` — event→table projection (shared with server).

**Auth split (shared part moves; server part stays)**

- `go-go-datadrop/pkg/auth/scope.go` — **moves** (Scope, ScopeSet, AllScopes, Validate/ParseScopes).
- `go-go-datadrop/pkg/auth/role.go` — **type model moves** (Role, ParseRole, Valid, AtLeast); **decision stays** (EffectiveRole, Authorize, DropACL).
- `go-go-datadrop/pkg/auth/device.go` — **`ValidateDeviceScopes` moves**; crypto stays.
- `go-go-datadrop/pkg/auth/{principal,token,oidc}.go` — **stay** (server-only).

**Operator surface (stays in go-go-datadrop)**

- `go-go-datadrop/pkg/cli/serve.go` — `ServeCommand`, OIDC/store/webui wiring.
- `go-go-datadrop/pkg/cli/healthcheck.go` — `HealthcheckCommand`, `/healthz` probe.
- `go-go-datadrop/pkg/server/`, `pkg/store/`, `pkg/blob/`, `pkg/webui/`, `pkg/stream/`, `pkg/schema/` — the proprietary server.

**Help (split)**

- `go-go-datadrop/pkg/doc/doc.go` — `AddDocToHelpSystem`, embed.
- `go-go-datadrop/pkg/doc/topics/06-cli-output.md` — **moves** (customer).
- `go-go-datadrop/pkg/doc/topics/01–05*.md`, `tutorials/` — **stay** (web-UI/operator).

**Target scaffold**

- `hyperslop-cli/go.mod` — module to rename to `github.com/hyperslop-systems/hyperslop-cli`.
- `hyperslop-cli/cmd/XXX/main.go` — to become `cmd/hyperslop/main.go`.
- `hyperslop-cli/.goreleaser.yaml`, `Makefile`, `lefthook.yml`, `.golangci.yml`, `.github/workflows/` — go-go-golems project plumbing (already present).
- `split-cli/go.work` — workspace linking `glazed`, `go-go-datadrop`, `hyperslop-cli`.

**Prior art in the repo (read for context, not for this ticket's scope)**

- `go-go-datadrop/ttmp/2026/07/26/DATADROP-9--glazed-verbs-convert-the-cli-to-glazed-commands-with-structured-output/design/01-…md` — the conversion of the CLI to glazed commands (the row/exit/section design this ticket preserves).
- `go-go-datadrop/ttmp/2026/07/27/DATADROP-12--root-token-removal-device-authentication-and-k3s-storage-deployment/design-doc/01-…md` — device authentication design.
- `go-go-datadrop/ttmp/2026/07/25/DATADROP-5--user-accounts-zitadel-…/design/01-…md` — user accounts, scopes, tokens, the `ddp_` format.
- `go-go-datadrop/README.md` — product overview, quick start, command table.

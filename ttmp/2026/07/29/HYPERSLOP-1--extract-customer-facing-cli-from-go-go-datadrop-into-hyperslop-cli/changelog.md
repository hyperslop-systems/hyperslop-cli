# Changelog

## 2026-07-29

- Initial workspace created


## 2026-07-29

Created HYPERSLOP-1: intern-grade analysis/design/implementation guide for extracting the customer-facing CLI from go-go-datadrop into hyperslop-cli. Mapped the customer spine (pkg/client -> pkg/datadrop -> pkg/auth scopes) and the shared pkg/cli foundation; identified the import-cycle constraint (admin CLI imports hyperslop-cli => wire types must move with the client); recorded 8 decision records (DR-1 wire-type home, DR-2 auth split, DR-3 app-name parameterization, DR-4 dual roots, DR-5 help split, DR-6 naming, DR-7 device flow via client, DR-8 release ordering) and a 9-phase file-level plan.

### Related Files

- /home/manuel/code/wesen/go-go-golems/go-go-datadrop/pkg/cli/build.go — DR-3 parameterization target (AppName/ErrorPrefix)


## 2026-07-29

Related 7 decision-shaping source files to the design doc and wrote the investigation diary (reference/01).

### Related Files

- /home/manuel/code/wesen/go-go-golems/go-go-datadrop/pkg/datadrop/device.go — wire types depend on auth.Scope (DR-1 cycle driver)


## 2026-07-29

Phase 1 complete: moved pkg/datadrop (wire types + folded scope/role model, DR-2 Option 2) and pkg/tabular into hyperslop-cli; rewired go-go-datadrop to import them (one-way dep, no cycle); added pkg/tabularfixture for the Go<->TS fixture contract. All tests green incl. authz matrix + smoke suite. Commits: hyperslop-cli 16b9535, go-go-datadrop 938682d.

### Related Files

- /home/manuel/workspaces/2026-07-29/split-cli/hyperslop-cli/pkg/datadrop/scope.go — folded shared scope model (DR-2 Option 2)


## 2026-07-29

Phases 2-3 complete: moved pkg/client (+DR-7 device methods) and the shared CLI foundation into hyperslop-cli; parameterized AppName/ErrorPrefix (DR-3) so hyperslop uses HYPERSLOP_* and the admin datadrop keeps DATADROP_*. go-go-datadrop now imports hyperslop-cli/pkg/cli for the foundation; operator commands (serve/healthcheck) + admin root stay. All tests green; no cycle. Commits: hyperslop-cli a44a2d3, 44a9976; go-go-datadrop be94e25, f18caf1.

### Related Files

- /home/manuel/workspaces/2026-07-29/split-cli/hyperslop-cli/pkg/cli/build.go — DR-3 AppName parameterization


## 2026-07-29

Phases 4-8 complete: command groups + customer help moved to hyperslop-cli (device flow refactored onto the client, DR-7); admin datadrop main rewired to import them (Phase 6 merged); hyperslop main+root wired (Phase 5, HYPERSLOP_* env); lint clean + go.sum tidied (Phase 7); hyperslop e2e smoke test against the real datadrop server + no-cycle CI guard (Phase 8). All tests green in both modules; hyperslop --help has no serve/healthcheck; datadrop --help has both. Commits: hyperslop-cli 8643a49,57727bb,9c8b43a,689a3cb; go-go-datadrop 61b7a70.

### Related Files

- /home/manuel/workspaces/2026-07-29/split-cli/hyperslop-cli/cmd/hyperslop/smoke_test.go — hyperslop e2e against the real datadrop server


## 2026-07-29

Phase 8 expanded: hyperslop smoke test now runs the full authenticated path (create/push/query/tail/export/schema/dataset/whoami) + exit-code contract (3/4/5) via hyperslop against the real datadrop server, seeding a token through a workspace 'go run' seeder. All hyperslop smoke tests PASS; server tests skip under GOWORK=off. Commit hyperslop-cli dbb39e3. Phases 0-8 of the extraction are complete and verified; Phase 9 (publish) remains blocked on push/tag/proxy access.

### Related Files

- /home/manuel/workspaces/2026-07-29/split-cli/hyperslop-cli/cmd/hyperslop/smoke_test.go — full hyperslop authenticated e2e against the real datadrop server


## 2026-07-29

PR #1 takeover review: independently assessed the extraction, fixed all 16 inline findings with regression tests (hyperslop-cli 1871472; go-go-datadrop 7647177), enabled the GitHub dependency graph, corrected install/release scaffolding, and added code-review/01-pr-1-takeover-review.md. Full standalone/workspace tests, real-server smoke, lint, vet, gofmt, no-cycle, logcopter and GoReleaser validation pass.

### Related Files

- /home/manuel/workspaces/2026-07-29/split-cli/hyperslop-cli/pkg/cli/dataset/get.go — transactional verified downloads
- /home/manuel/workspaces/2026-07-29/split-cli/hyperslop-cli/pkg/cli/events/tail.go — resilient SSE following
- /home/manuel/workspaces/2026-07-29/split-cli/hyperslop-cli/ttmp/2026/07/29/HYPERSLOP-1--extract-customer-facing-cli-from-go-go-datadrop-into-hyperslop-cli/code-review/01-pr-1-takeover-review.md — takeover assessment and finding disposition


## 2026-07-29

Companion branch publish hygiene: first go-go-datadrop push exposed a 125,701,651-byte root datadrop binary accidentally tracked by Phase 4. Preserved a local backup ref, reconstructed the two affected unpushed commits without the blob, added /datadrop to .gitignore, retested, and pushed clean Phase 4 61b7a70 plus review fix 7647177.

### Related Files

- /home/manuel/workspaces/2026-07-29/split-cli/go-go-datadrop/.gitignore — prevents root CLI build artifacts from entering history


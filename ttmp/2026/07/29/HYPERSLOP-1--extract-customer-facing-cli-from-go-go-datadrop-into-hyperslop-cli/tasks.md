# Tasks

## TODO

- [x] Analysis & design: map go-go-datadrop CLI, design the split, write intern guide (this ticket's design doc + diary) <!-- t:5w9n -->
- [x] Phase 0: Rename hyperslop-cli scaffold (module -> github.com/hyperslop-systems/hyperslop-cli, cmd/hyperslop, .goreleaser, Makefile, logcopter area) <!-- t:cg7y -->
- [x] Phase 1: Move wire types (pkg/datadrop) + scope/role model (pkg/auth) + pkg/tabular into hyperslop-cli; add auth alias re-export in go-go-datadrop (DR-2) <!-- t:m1ic -->
- [x] Phase 2: Move pkg/client into hyperslop-cli; add StartDeviceAuthorization/PollDeviceToken client methods (DR-7) <!-- t:0kwf -->
- [x] Phase 3: Move shared CLI foundation (section/build/exit/rows/fields/whoami) into hyperslop-cli; parameterize AppName/ErrorPrefix (DR-3) <!-- t:1ayq -->
- [x] Phase 4: Move command groups (authcmd/drops/events/dataset/schemacmd) + customer help into hyperslop-cli; refactor device command onto client (DR-7) <!-- t:ko7i -->
- [x] Phase 5: Wire hyperslop main + root (customer-only tree, HYPERSLOP_* env) <!-- t:ekx5 -->
- [x] Phase 6: Rewire go-go-datadrop admin main/root to import customer registrars from hyperslop-cli (DR-4) <!-- t:bd94 -->
- [x] Phase 7: Server-side import-path hygiene; auth alias compile test; lint + govulncheck both modules <!-- t:lrk8 -->
- [x] Phase 8: Port smoke tests to hyperslop; add CI; validate exit-code + row contracts in both binaries <!-- t:cpgg -->
- [ ] Phase 9: Release hyperslop-cli v0.1.0; bump go-go-datadrop go.mod off the workspace replace (DR-8) <!-- t:9adl -->

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


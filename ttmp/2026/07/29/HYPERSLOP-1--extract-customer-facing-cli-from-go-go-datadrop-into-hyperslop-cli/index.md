---
Title: Extract customer-facing CLI from go-go-datadrop into hyperslop-cli
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
DocType: index
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: "Extract the customer/agent-facing CLI (drops, events, datasets, schemas, device auth, whoami) and its supporting library (wire types, HTTP client, scope/role model, tabular projection, glazed CLI foundation) out of go-go-datadrop into a new hyperslop-cli binary. The proprietary server and its admin CLI stay in go-go-datadrop and import the customer-facing packages back from hyperslop-cli so nothing is duplicated."
LastUpdated: 2026-07-29T11:31:44.914223189-04:00
WhatFor: "Onboarding an intern to understand the datadrop system and execute the CLI extraction in safe phases."
WhenToUse: "Read before touching the split between go-go-datadrop and hyperslop-cli. Follow the phased plan in the design doc §10."
---

# Extract customer-facing CLI from go-go-datadrop into hyperslop-cli

## Overview

`go-go-datadrop` is today one binary that is both the proprietary server
(`datadrop serve`) and a thin client of that server's HTTP API. This ticket
extracts the **client half** into a new, independently-released binary,
`hyperslop-cli` (binary `hyperslop`), so it can be given to agents and
customers without shipping the server. The admin `datadrop` CLI keeps the
operator commands (`serve`, `healthcheck`) and **imports** the customer verbs
from `hyperslop-cli`, so the two never duplicate or drift.

The hard constraint that drives the design: `go-go-datadrop` must depend on
`hyperslop-cli` (to import the customer commands), so the wire types
(`pkg/datadrop`), the scope/role model, and `pkg/tabular` must move **with** the
client into `hyperslop-cli` to avoid an import cycle.

**Status:** analysis/design complete (design doc + diary written); implementation
not started. Decision records DR-1 and DR-2 are `proposed` and await user
confirmation re: the AGENT.md "no adapters/shim" guideline (see design doc §12
open question #1).

## Deliverables

- `design-doc/01-…md` — the intern-grade analysis, design and implementation guide (85 KB).
- `reference/01-investigation-diary.md` — the evidence-gathering diary behind the design.

## Key Links

- **Related Files**: See frontmatter RelatedFiles field
- **External Sources**: See frontmatter ExternalSources field

## Status

Current status: **active**

## Topics

- cli
- architecture
- refactor
- datadrop
- hyperslop
- glazed
- agent-cli
- extraction

## Tasks

See [tasks.md](./tasks.md) for the current task list.

## Changelog

See [changelog.md](./changelog.md) for recent changes and decisions.

## Structure

- design/ - Architecture and design documents
- reference/ - Prompt packs, API contracts, context summaries
- playbooks/ - Command sequences and test procedures
- scripts/ - Temporary code and tooling
- various/ - Working notes and research
- archive/ - Deprecated or reference-only artifacts

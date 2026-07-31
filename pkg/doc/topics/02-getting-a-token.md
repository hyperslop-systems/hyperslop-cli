---
Title: Getting a token with device pairing
Slug: getting-a-token
Short: "How an agent obtains a scoped, revocable ddp_ token by browser approval, without ever handling an OIDC bearer token."
Topics:
- cli
- auth
- agent
Commands:
- auth device
Flags:
- scopes
- expires-in
- credential-file
SectionType: Tutorial
IsTopLevel: true
ShowPerDefault: true
---

The CLI never accepts a ZITADEL/OIDC access token as a datadrop data-plane
credential. Instead, an agent obtains a scoped, revocable local `ddp_` token
through a browser-approved device pairing flow (RFC-8628-style).

## The ceremony

```
agent (CLI)                 datadrop server                 human browser
   |  POST /v1/device/authorizations  ->|                        |
   |  <-- {device_code, user_code, url} |                        |
   |  print url + user_code to stderr   |                        |
   |  poll POST /v1/device/tokens       |  <-- sign in + verify   |
   |  (AuthorizationPending… retry)     |      code               |
   |  <-- {token: ddp_…}                |                        |
```

The token is printed **once** to stdout (so it can be captured) or written to a
`0600` credential file. Capture it straight into the connection environment
variable:

```bash
export HYPERSLOP_TOKEN="$(hyperslop auth device --name 'local coding agent' --scopes drops:read,drops:write,workbenches:read,workbenches:write --expires-in 24h)"
```

Or write an owner-only file and point the environment at it:

```bash
hyperslop auth device --credential-file ~/.config/datadrop/agent.token
export HYPERSLOP_TOKEN="$(cat ~/.config/datadrop/agent.token)"
```

(On the admin `datadrop` binary the variable is `DATADROP_TOKEN` and the command
is `datadrop auth device`; the flow is identical.)

## Scopes

Device tokens may carry only operational scopes:

- `drops:read` reads drop metadata and events.
- `drops:write` appends and modifies drop data.
- `datasets:write` uploads and modifies datasets.
- `workbenches:read` lists workbenches, reads snapshots, and follows revision
  streams.
- `workbenches:write` creates, replaces, mutates, and deletes workbenches.

Device tokens may never carry `admin`; the server rejects an `admin` request at
pairing time. A token narrows its owner's rights; it never carries rights of its
own, and revoking membership on a drop instantly narrows every token that owner
holds.

For a read-only workbench observer:

```bash
hyperslop auth device --scopes workbenches:read
```

For an agent that authors workbench layouts and tile content:

```bash
hyperslop auth device --scopes workbenches:read,workbenches:write
```

## Verify

```bash
hyperslop whoami
```

`whoami` answers the four questions a 403 cannot distinguish: is the token
valid, whose is it, what may it do, and is this server even running with user
accounts.

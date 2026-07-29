# Hyperslop CLI

`hyperslop` is the customer and agent CLI for a [Datadrop](https://github.com/go-go-golems/go-go-datadrop) server. It creates and queries event drops, tails SSE streams, manages JSON Schema, and publishes or retrieves immutable datasets.

Hyperslop talks to Datadrop through its public HTTP and SSE API. Point it at any Datadrop server for which you have a scoped Datadrop token.

## Install

### Go install

After the first release is published:

```bash
go install github.com/hyperslop-systems/hyperslop-cli/cmd/hyperslop@v0.1.0
hyperslop --help
```

The binary is installed to `$(go env GOPATH)/bin`. Add that directory to `PATH` if necessary.

### Build from source

```bash
git clone https://github.com/hyperslop-systems/hyperslop-cli.git
cd hyperslop-cli
GOWORK=off go build -o ./bin/hyperslop ./cmd/hyperslop
./bin/hyperslop --help
```

## Quick exercise against a real local server

This exercise uses an actual Datadrop server, SQLite database, content-addressed blob store, and local ZITADEL identity provider. It is not a mocked HTTP service. You need Go, Docker, [devctl](https://github.com/go-go-golems/devctl), and a browser for device approval.

### 1. Start Datadrop and its local identity provider

In a separate checkout of `go-go-datadrop`:

```bash
git clone https://github.com/go-go-golems/go-go-datadrop.git
cd go-go-datadrop

devctl up
# Wait until devctl reports:
# API: http://127.0.0.1:8080
# ZITADEL: http://localhost:17071
```

`devctl up` starts the host-development ZITADEL stack and runs the real Go server on port 8080. Leave it running while you use the CLI. Its persistent development database and credentials are local ignored files; it does not contact a shared production service.

If you prefer to supervise the pieces yourself, use the server repository's documented `make zitadel-up`, `make zitadel-env`, and `go run ./cmd/datadrop serve ...` flow. Do not start `datadrop serve` without its OIDC issuer, client credentials, external URL, and device-code pepper: the server intentionally fails closed on incomplete authentication configuration.

### 2. Pair the CLI in a browser

In another terminal, install or build `hyperslop`, then point it at the local server:

```bash
export HYPERSLOP_ADDR=http://127.0.0.1:8080

export HYPERSLOP_TOKEN="$(hyperslop auth device \
  --name 'local hyperslop exercise' \
  --scopes drops:read,drops:write,datasets:write \
  --expires-in 24h)"
```

The command prints a verification URL and short code to **stderr**. Open that URL, sign in to the local ZITADEL instance, approve the code, and return to the terminal. The one-time Datadrop token is emitted only on stdout and becomes `HYPERSLOP_TOKEN` through command substitution.

Use the Datadrop development-stack documentation if you need help signing in to the local identity provider. Never place an upstream OIDC bearer token in `HYPERSLOP_TOKEN`.

### 3. Create, write, query, tail, and export a real drop

```bash
hyperslop whoami --format json

hyperslop create greenhouse
hyperslop push greenhouse temperature=21.7 humidity=0.48
printf '{"temperature":22.8,"humidity":0.51}\n' | hyperslop push greenhouse --stdin

hyperslop query greenhouse --order asc --format jsonl
hyperslop export greenhouse --format csv > greenhouse.csv
cat greenhouse.csv
```

Tail uses JSONL by default because it can stream one complete event at a time:

```bash
# Terminal A
hyperslop tail greenhouse --follow

# Terminal B
hyperslop push greenhouse temperature=23.1 humidity=0.46
```

Stop Terminal A with Ctrl-C. Intentional termination of `tail --follow` exits successfully; interrupted finite work such as export or download exits nonzero so scripts do not mistake partial output for complete output.

### 4. Add a strict schema and publish a dataset

```bash
cat > reading.schema.json <<'JSON'
{
  "type": "object",
  "required": ["temperature"],
  "properties": {
    "temperature": {"type": "number"},
    "humidity": {"type": "number"}
  }
}
JSON

hyperslop schema put greenhouse --file reading.schema.json --mode strict
hyperslop schema show greenhouse --format jsonl --output-fields spec | jq '.spec'

printf 'station,temperature\nnorth,21.7\nsouth,22.8\n' > readings.csv
hyperslop dataset push greenhouse readings-2026 \
  --file readings.csv:data/readings.csv \
  --title 'Local greenhouse readings'

hyperslop dataset list greenhouse --format json
hyperslop dataset get greenhouse readings-2026 --output ./downloaded
cat ./downloaded/data/readings.csv
```

Dataset downloads verify SHA-256 digests by default. A whole-version download stages and verifies every file before publishing it, so a later failed transfer does not leave a mixed old/new directory. Pass `--no-verify` only when you explicitly accept that integrity check trade-off.

### 5. Clean up

```bash
unset HYPERSLOP_TOKEN
# In the go-go-datadrop checkout:
devctl down
```

## Core commands

| Command | Purpose |
|---|---|
| `hyperslop auth device` | Browser-approved device pairing; emits a scoped `ddp_` token once. |
| `hyperslop create`, `list`, `inspect` | Manage and inspect drops. |
| `hyperslop push`, `query`, `tail`, `export` | Write, read, stream, and export events. |
| `hyperslop schema put`, `schema show` | Manage JSON Schema validation for a stream. |
| `hyperslop dataset push`, `list`, `show`, `get`, `import`, `rm`, `gc` | Manage immutable datasets and materialize rows into streams. |
| `hyperslop whoami` | Inspect the configured server and credential. |

Every row-producing command supports `--format table|json|jsonl|csv|tsv|yaml` and `--output-fields`. JSONL is the streaming format for `tail --follow`.

## Configuration

| Environment variable | Meaning |
|---|---|
| `HYPERSLOP_ADDR` | Datadrop server base URL. Defaults to `http://localhost:8080`. |
| `HYPERSLOP_TOKEN` | Scoped Datadrop `ddp_` bearer token. |
| `HYPERSLOP_LOG_LEVEL` | Client log verbosity. |

Flags override environment variables. Never use an upstream OIDC access token as `HYPERSLOP_TOKEN`; use `hyperslop auth device` to mint a scoped Datadrop token.

## Script contracts

Exit codes are stable:

| Code | Meaning |
|---:|---|
| 0 | Success |
| 1 | Generic/runtime failure |
| 2 | Invalid invocation or flag usage |
| 3 | Authentication or authorization failure |
| 4 | Requested resource not found |
| 5 | Validation failure, including rejected strict-schema data |

For example:

```bash
hyperslop query greenhouse --format jsonl > events.jsonl
code=$?
if [ "$code" -ne 0 ]; then
  case "$code" in
    3) echo 'refresh or re-authorize the Datadrop token' >&2 ;;
    4) echo 'the drop does not exist' >&2 ;;
    *) echo 'query failed' >&2 ;;
  esac
fi
```

## Development and verification

```bash
GOWORK=off go test ./...
GOWORK=off go vet ./...
GOWORK=off golangci-lint run ./...
make logcopter-check

# In the split workspace, runs the compiled CLI against the actual Datadrop server.
go test ./cmd/hyperslop -run TestHyperslop -count=1 -v
```

The final command requires the sibling `go-go-datadrop` module from the split workspace. Under standalone `GOWORK=off` mode, only server-dependent acceptance tests skip; unit, client, parser, and command tests still run.

## License

See [LICENSE](LICENSE).

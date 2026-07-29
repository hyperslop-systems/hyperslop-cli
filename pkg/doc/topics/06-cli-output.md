---
Title: CLI structured output
Slug: cli-output
Short: "How Datadrop row-producing verbs select formats, project fields, cap serialization, and stream JSONL."
Topics:
- cli
- output
- rows
- streaming
Commands:
- query
- tail
- list
- inspect
- whoami
Flags:
- format
- output-fields
- max-output-rows
SectionType: GeneralTopic
IsTopLevel: true
ShowPerDefault: true
---

`hyperslop` commands that return structured data emit rows. Glazed v1.4 applies one
small output contract to those rows:

- `--format` selects serialization;
- `--output-fields` projects named columns;
- `--max-output-rows` caps serialization.

The default format is `table`. The supported formats are `table`, `json`,
`jsonl`, `csv`, `tsv`, and `yaml`.

```bash
hyperslop query greenhouse --format json
hyperslop query greenhouse --format csv --output-fields seq,time,data.temp_c
hyperslop list --format yaml
hyperslop whoami --format jsonl --output-fields user_id
```

## Row shape

Event commands project the envelope first and then flatten payload keys under
`data.*`. The names are the same names returned by the table endpoint and shown
by the web workbench.

```text
id
drop
stream
seq
time
received_at
source
type
subject
meta                    # present when the event carries provenance metadata
data.<payload-path>
```

`meta` is a nested JSON object; schema provenance is available as
`meta.schema_version` inside that object, not as a top-level `schema_version`
column. Payload columns are sorted after the envelope columns. Missing payload
values remain absent rather than changing the field name or row shape.

Nested payload objects use `.` as a path separator. A literal `.` or `\\` in a
JSON key is escaped with `\\`, so `{"a.b": 1, "a": {"b": 2}}` produces the two
distinct columns `data.a\\.b` and `data.a.b` rather than losing one value. An
empty key segment is encoded as `\\0`; a literal key `\\0` is distinct because
its backslash is escaped to `\\\\0`.

`--output-fields` projects these emitted columns in the requested order:

```bash
hyperslop query greenhouse \
  --output-fields seq,time,data.temp_c \
  --format jsonl
```

Projection does not reduce server work. Use command-specific source flags such
as `--limit`, `--from`, `--to`, and `--after` when the operation itself should
fetch less data.

## Streaming

JSONL is the streaming contract. It writes one compact JSON object per line and
does not require the command to finish before a reader can consume rows.

```bash
hyperslop tail greenhouse --follow --format jsonl |
  jq -c 'select(."data.temp_c" > 21)'
```

`tail` defaults to JSONL because `--follow` may remain open indefinitely. A
bounded tail may request a terminal table explicitly:

```bash
hyperslop tail greenhouse --format table
```

There is no generic `--stream` switch. `--drop-stream` selects a Datadrop stream
inside a drop; it is a domain input, not an output formatter setting.

## Output row cap

`--max-output-rows` prevents more than the requested number of rows from
reaching the serializer. Zero means unlimited.

```bash
hyperslop query greenhouse --format jsonl --max-output-rows 100
```

This is an output guard, not source pagination. Prefer the command's own
`--limit` when avoiding server or database work matters.

## Server-formatted export

`hyperslop export` is not a structured row command. Its `--format` value is sent
to the server, which streams already-formatted bytes. Use export when the
original nested envelope or server-owned CSV/NDJSON representation is required:

```bash
hyperslop export greenhouse --format ndjson
hyperslop export greenhouse --format csv
```

## Troubleshooting

| Symptom | Cause | Correction |
|---|---|---|
| `unknown flag: --output` | Glazed v1.4 removed the legacy output flag | Use `--format` |
| `unknown flag: --fields` | Projection was renamed | Use `--output-fields` |
| A JSON command waits before printing | JSON emits one array | Use `--format jsonl` for streaming |
| A projected payload field is absent | That row has no such payload key | Run without projection and inspect available columns |
| Work continues after the output cap | The cap applies to serialization | Use the command-specific `--limit` |

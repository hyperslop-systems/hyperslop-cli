package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/types"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

// The row shapes are a public API (DR-79). `datadrop query --output-fields seq` makes
// those names something scripts depend on, and renaming one is a breaking
// change that no compiler catches — so it is caught here instead.
//
// This test is a change detector on purpose. When it fails, the question is not
// "how do I make it pass" but "did I just break someone's script"; if the
// answer is yes and the rename is still right, the key list below is the one
// place to change and the changelog is the other.

// keysOf returns a row's keys in order.
func keysOf(row types.Row) []string {
	keys := make([]string, 0, row.Len())
	for pair := row.Oldest(); pair != nil; pair = pair.Next() {
		keys = append(keys, pair.Key)
	}
	return keys
}

func assertKeys(t *testing.T, what string, row types.Row, want []string) {
	t.Helper()

	got := keysOf(row)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s keys =\n  %s\nwant\n  %s",
			what, strings.Join(got, ","), strings.Join(want, ","))
	}
}

func TestRowForDropKeys(t *testing.T) {
	row := RowForDrop(datadrop.Drop{Name: "greenhouse"})
	assertKeys(t, "RowForDrop", row, []string{
		"name", "created_at", "retention", "public_read", "owner_id", "your_role",
	})
}

func TestRowForDropStatsKeys(t *testing.T) {
	row := RowForDropStats(datadrop.DropStats{
		Drop:    datadrop.Drop{Name: "greenhouse"},
		Streams: []string{"events"},
	})
	// The drop keys come first, in the same order RowForDrop emits them, so
	// `list --output-fields name,retention` and `inspect --output-fields name,retention` name
	// the same columns.
	assertKeys(t, "RowForDropStats", row, []string{
		"name", "created_at", "retention", "public_read", "owner_id", "your_role",
		"event_count", "last_seq", "last_event", "streams",
	})
}

func TestRowForEnvelopeKeys(t *testing.T) {
	event := datadrop.Envelope{
		ID:     "01J0",
		Drop:   "greenhouse",
		Stream: "events",
		Seq:    184,
		Time:   time.Date(2026, 7, 26, 18, 4, 11, 512_000_000, time.UTC),
		Data:   json.RawMessage(`{"temp_c":21.7,"location":{"lat":52.5}}`),
	}

	row, err := RowForEnvelope(event)
	if err != nil {
		t.Fatalf("RowForEnvelope: %v", err)
	}

	// The nine envelope columns are tabular.EnvelopeColumns, in that order, and
	// the payload follows flattened under "data." — the same projection the
	// server's /table endpoint returns, so a workbench field chip and
	// `--output-fields data.temp_c` name the same column (DR-83).
	assertKeys(t, "RowForEnvelope", row, []string{
		"id", "drop", "stream", "seq", "time", "received_at", "source", "type", "subject",
		"data.location.lat", "data.temp_c",
	})
}

// An event with no payload contributes no data columns at all, which is what
// makes `datadrop query --format csv` on an empty-payload stream produce a
// header rather than one column named "data".
func TestRowForEnvelopeWithoutPayload(t *testing.T) {
	row, err := RowForEnvelope(datadrop.Envelope{ID: "01J0"})
	if err != nil {
		t.Fatalf("RowForEnvelope: %v", err)
	}
	assertKeys(t, "RowForEnvelope (no payload)", row, []string{
		"id", "drop", "stream", "seq", "time", "received_at", "source", "type", "subject",
	})
}

// meta is absent from the table projection because a chart has no use for it,
// and present here because it is the provenance record of an imported event.
func TestRowForEnvelopeCarriesMeta(t *testing.T) {
	row, err := RowForEnvelope(datadrop.Envelope{
		ID:   "01J0",
		Meta: json.RawMessage(`{"dataset":"readings","row":1}`),
	})
	if err != nil {
		t.Fatalf("RowForEnvelope: %v", err)
	}
	assertKeys(t, "RowForEnvelope (with meta)", row, []string{
		"id", "drop", "stream", "seq", "time", "received_at", "source", "type", "subject",
		"meta",
	})

	meta, ok := row.Get("meta")
	if !ok {
		t.Fatal("meta is missing")
	}
	// Decoded, not raw bytes: --format json must emit a nested object rather
	// than a base64 byte slice.
	decoded, ok := meta.(map[string]any)
	if !ok {
		t.Fatalf("meta is %T, want a decoded object", meta)
	}
	if decoded["dataset"] != "readings" {
		t.Errorf("meta.dataset = %v, want readings", decoded["dataset"])
	}
}

// A page is projected in one call so that every row carries the same keys, even
// when one event's payload is missing a field another one has. Projecting event
// by event would give the first row three columns and the second row four.
func TestRowsForEnvelopesShareColumns(t *testing.T) {
	rows, err := RowsForEnvelopes([]datadrop.Envelope{
		{ID: "a", Data: json.RawMessage(`{"temp_c":21.7}`)},
		{ID: "b", Data: json.RawMessage(`{"temp_c":21.9,"humidity":0.48}`)},
	})
	if err != nil {
		t.Fatalf("RowsForEnvelopes: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("projected %d rows, want 2", len(rows))
	}

	for i, row := range rows {
		if _, ok := row.Get("data.humidity"); !ok && i == 1 {
			t.Errorf("row %d has no data.humidity", i)
		}
	}
	// Ordering is resolved over the whole page: envelope columns, then payload
	// columns sorted by name.
	assertKeys(t, "RowsForEnvelopes[1]", rows[1], []string{
		"id", "drop", "stream", "seq", "time", "received_at", "source", "type", "subject",
		"data.humidity", "data.temp_c",
	})
}

func TestRowForAppendResultKeys(t *testing.T) {
	row := RowForAppendResult(datadrop.AppendResult{ID: "01J0", Seq: 3})
	assertKeys(t, "RowForAppendResult", row, []string{
		"id", "drop", "stream", "seq", "received_at", "duplicate", "warnings",
	})
}

func TestRowForSchemaKeys(t *testing.T) {
	row := RowForSchema(datadrop.Schema{Drop: "greenhouse", Spec: json.RawMessage(`{}`)})
	assertKeys(t, "RowForSchema", row, []string{
		"drop", "stream", "version", "mode", "created_at", "spec",
	})
}

func TestRowForPutSchemaResultKeys(t *testing.T) {
	row := RowForPutSchemaResult(datadrop.PutSchemaResult{Drop: "greenhouse", Version: 1})
	assertKeys(t, "RowForPutSchemaResult", row, []string{
		"drop", "stream", "version", "mode",
	})
}

func TestRowForDatasetKeys(t *testing.T) {
	row := RowForDataset(datadrop.Dataset{
		Drop: "greenhouse", Name: "readings-2026",
		Versions: []datadrop.DatasetVersion{{Version: 3, FileCount: 4, TotalBytes: 900}},
	})
	assertKeys(t, "RowForDataset", row, []string{
		"drop", "name", "created_at", "versions",
		"latest_version", "latest_files", "latest_bytes",
	})

	// The latest_* columns summarize the newest version, which
	// ListDatasetVersions returns first.
	if v, _ := row.Get("latest_version"); v != 3 {
		t.Errorf("latest_version = %v, want 3", v)
	}
}

func TestRowForDatasetVersionKeys(t *testing.T) {
	row := RowForDatasetVersion(datadrop.DatasetVersion{Drop: "greenhouse", Version: 1})
	assertKeys(t, "RowForDatasetVersion", row, []string{
		"drop", "dataset", "version", "state", "file_count", "total_bytes",
		"created_at", "committed_at", "manifest",
	})
}

func TestRowForDeletedVersionKeys(t *testing.T) {
	row := RowForDeletedVersion("greenhouse", "readings", "1")
	assertKeys(t, "RowForDeletedVersion", row, []string{
		"drop", "dataset", "version", "deleted",
	})
}

func TestRowForImportResultKeys(t *testing.T) {
	row := RowForImportResult(datadrop.ImportResult{Drop: "greenhouse", Appended: 2})
	assertKeys(t, "RowForImportResult", row, []string{
		"drop", "dataset", "version", "path", "stream",
		"rows", "appended", "skipped", "truncated", "warnings",
	})
}

func TestRowForGCResultKeys(t *testing.T) {
	row := RowForGCResult(client.GCResult{Scanned: 3, Deleted: 1})
	assertKeys(t, "RowForGCResult", row, []string{
		"scanned", "referenced", "deleted", "freed_bytes",
	})
}

func TestRowForPrincipalKeys(t *testing.T) {
	row := RowForPrincipal("http://localhost:8080", client.Me{
		AuthMode:      "token",
		Authenticated: true,
		Kind:          "root",
		User:          &client.MeUser{ID: "u1", Email: "a@b.c", Name: "A"},
		Provider:      &client.MeProvider{Issuer: "https://idp"},
	})
	assertKeys(t, "RowForPrincipal", row, []string{
		"server", "auth_mode", "authenticated", "kind",
		"user_id", "email", "name", "token_id", "scopes", "issuer",
	})

	// The nested user is flattened into user_id/email/name rather than emitted
	// as an object, because `whoami --output-fields user_id` is the point.
	if v, _ := row.Get("user_id"); v != "u1" {
		t.Errorf("user_id = %v, want u1", v)
	}
}

// An anonymous principal must still produce every column, so that
// `whoami --format csv` has a stable header whether or not a credential was
// accepted.
func TestRowForPrincipalAnonymousKeepsColumns(t *testing.T) {
	row := RowForPrincipal("http://localhost:8080", client.Me{AuthMode: "oidc"})
	assertKeys(t, "RowForPrincipal (anonymous)", row, []string{
		"server", "auth_mode", "authenticated", "kind",
		"user_id", "email", "name", "token_id", "scopes", "issuer",
	})
}

// Timestamps use the one canonical format the store, the CSV export and the
// table projection already agree on. A second spelling here would make
// `datadrop list --format csv` and `datadrop export --format csv` disagree
// about what a date looks like.
func TestRowTimestampsAreCanonical(t *testing.T) {
	instant := time.Date(2026, 7, 20, 9, 14, 2, 0, time.UTC)
	row := RowForDrop(datadrop.Drop{Name: "greenhouse", CreatedAt: instant})

	got, _ := row.Get("created_at")
	if got != "2026-07-20T09:14:02.000Z" {
		t.Errorf("created_at = %v, want 2026-07-20T09:14:02.000Z", got)
	}
}

// An absent optional timestamp is an empty cell, not the year 1.
func TestOptionalTimestampsAreEmptyWhenAbsent(t *testing.T) {
	row := RowForDropStats(datadrop.DropStats{Drop: datadrop.Drop{Name: "x"}})
	if got, _ := row.Get("last_event"); got != "" {
		t.Errorf("last_event = %q, want an empty cell", got)
	}
}

// --token must be a fields.TypeSecret and not a fields.TypeString, and this is
// the one test in the repository with a security consequence.
//
// Every Glazed command gets glazed's command-settings section, which adds
// --print-parsed-fields; that flag dumps every resolved value together with the
// source it came from. Redaction happens in fields.RedactValue, which returns
// the value UNCHANGED unless the field's type reports IsSensitive() — and
// IsSensitive() is true for exactly one type. Declared TypeString, a bearer
// token is printed in full, three times: the value, the parse log, and the name
// of the environment variable it was read from.
//
// The cobra flag cannot be inspected for this: glazed registers TypeString and
// TypeSecret with the same flagSet.String call, so the distinction only exists
// in the field definition. That is why this test lives here and not beside the
// command-tree tests.
func TestClientTokenIsASecret(t *testing.T) {
	section, err := NewClientSection()
	if err != nil {
		t.Fatalf("NewClientSection: %v", err)
	}

	token, ok := section.GetDefinitions().Get("token")
	if !ok {
		t.Fatal("the client section has no token field")
	}
	if !token.Type.IsSensitive() {
		t.Errorf("--token is %s, which glazed does not redact; want %s",
			token.Type, fields.TypeSecret)
	}

	// --addr is not a secret, and should not be: "where did this address come
	// from" is the question --print-parsed-fields exists to answer.
	addr, ok := section.GetDefinitions().Get("addr")
	if !ok {
		t.Fatal("the client section has no addr field")
	}
	if addr.Type.IsSensitive() {
		t.Errorf("--addr is redacted, which defeats the point of --print-parsed-fields")
	}
}

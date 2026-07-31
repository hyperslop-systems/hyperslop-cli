package cli

import (
	"encoding/json"
	"time"

	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/jsondoc"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/tabular"
)

// The row shape is a public API (DR-79).
//
// The moment `datalab query --output-fields seq,time,data.temp_c` works, those names
// are something scripts depend on, and renaming one is a breaking change no
// compiler catches. So every response type gets exactly one projection
// function, they all live here, and rows_test.go pins the exact key set of
// each. That test is a change detector, and it earns its keep because the thing
// it detects is a contract rather than a detail.
//
// Two conventions hold throughout:
//
//   - Key names are the API's JSON names. `created_at`, not `created`;
//     `received_at`, not `received`. A caller who has read the HTTP API knows
//     the column names without being told twice.
//   - Timestamps are rendered with datalab.FormatTime, the canonical
//     fixed-width UTC RFC3339 the store, the CSV export and the table
//     projection already agree on.

// RowsForEnvelopes projects a page of events, flattening each payload into
// data.* columns.
//
// The flattening is not implemented here. It is tabular.FromEvents — the same
// projection the server's /table endpoint returns and therefore the same column
// names the web workbench puts on its field chips (DR-83). If the CLI invented
// its own flattener, `datalab query --output-fields data.temp_c` and the column the
// workbench names would be two different things that happen to look alike.
//
// Projecting the whole page in one call rather than event by event matters:
// tabular resolves the column set over the whole sample, so every returned row
// carries the same keys in the same order even when one event's payload is
// missing a field another one has.
func RowsForEnvelopes(events []datalab.Envelope) ([]types.Row, error) {
	table, err := tabular.FromEvents(tabular.SourceRef{Kind: tabular.KindStream}, events)
	if err != nil {
		return nil, errors.Wrap(err, "projecting events")
	}

	rows := make([]types.Row, 0, len(table.Rows))
	for i, projected := range table.Rows {
		row := types.NewRow()
		for _, field := range table.Fields {
			if value, ok := projected[field.Name]; ok {
				row.Set(field.Name, value)
			}
		}
		// meta is deliberately absent from the table projection — a chart has no
		// use for it — but it is the whole provenance record of an imported
		// event, so the CLI carries it. Decoded rather than passed as raw bytes:
		// a json.RawMessage renders as a byte slice in a table cell, and as a
		// nested object under --format json, which is the useful reading.
		if i < len(events) {
			if meta := decodeJSON(events[i].Meta); meta != nil {
				row.Set("meta", meta)
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// RowForEnvelope projects one event, for the streaming path where events arrive
// one at a time and there is no page to resolve columns over.
func RowForEnvelope(event datalab.Envelope) (types.Row, error) {
	rows, err := RowsForEnvelopes([]datalab.Envelope{event})
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, errors.Errorf("projecting one event produced %d rows", len(rows))
	}
	return rows[0], nil
}

// RowForDrop projects a drop listing entry.
func RowForDrop(d datalab.Drop) types.Row {
	return types.NewRow(
		types.MRP("name", d.Name),
		types.MRP("created_at", datalab.FormatTime(d.CreatedAt)),
		types.MRP("retention", d.Retention),
		types.MRP("public_read", d.PublicRead),
		types.MRP("owner_id", d.OwnerID),
		types.MRP("your_role", d.YourRole),
	)
}

// RowForDropStats projects the inspection view: a drop plus its counters.
//
// It is a superset of RowForDrop and starts with the same keys in the same
// order, so `datalab list --output-fields name,retention` and
// `datalab inspect X --output-fields name,retention` name the same things.
func RowForDropStats(s datalab.DropStats) types.Row {
	row := RowForDrop(s.Drop)
	row.Set("event_count", s.EventCount)
	row.Set("last_seq", s.LastSeq)
	row.Set("last_event", formatOptionalTime(s.LastEvent))
	row.Set("streams", s.Streams)
	return row
}

// RowForAppendResult projects what the ingest endpoint returned for one event.
func RowForAppendResult(r datalab.AppendResult) types.Row {
	return types.NewRow(
		types.MRP("id", r.ID),
		types.MRP("drop", r.Drop),
		types.MRP("stream", r.Stream),
		types.MRP("seq", r.Seq),
		types.MRP("received_at", datalab.FormatTime(r.ReceivedAt)),
		types.MRP("duplicate", r.Duplicate),
		types.MRP("warnings", len(r.Warnings)),
	)
}

// RowForSchema projects a stored schema version.
func RowForSchema(s datalab.Schema) types.Row {
	return types.NewRow(
		types.MRP("drop", s.Drop),
		types.MRP("stream", s.Stream),
		types.MRP("version", s.Version),
		types.MRP("mode", string(s.Mode)),
		types.MRP("created_at", datalab.FormatTime(s.CreatedAt)),
		types.MRP("spec", decodeJSON(s.Spec)),
	)
}

// RowForPutSchemaResult projects the response to registering a schema version.
func RowForPutSchemaResult(r datalab.PutSchemaResult) types.Row {
	return types.NewRow(
		types.MRP("drop", r.Drop),
		types.MRP("stream", r.Stream),
		types.MRP("version", r.Version),
		types.MRP("mode", string(r.Mode)),
	)
}

// RowForDataset projects a dataset listing entry.
//
// The latest_* columns summarize the newest version, because "how big is this
// dataset now" is the question a listing answers and reaching for it otherwise
// costs a second call. ListDatasetVersions returns newest first.
func RowForDataset(d datalab.Dataset) types.Row {
	row := types.NewRow(
		types.MRP("drop", d.Drop),
		types.MRP("name", d.Name),
		types.MRP("created_at", datalab.FormatTime(d.CreatedAt)),
		types.MRP("versions", len(d.Versions)),
	)

	var latestVersion, latestFiles int
	var latestBytes int64
	if len(d.Versions) > 0 {
		newest := d.Versions[0]
		latestVersion, latestFiles, latestBytes = newest.Version, newest.FileCount, newest.TotalBytes
	}
	row.Set("latest_version", latestVersion)
	row.Set("latest_files", latestFiles)
	row.Set("latest_bytes", latestBytes)
	return row
}

// RowForDatasetVersion projects one immutable dataset version.
func RowForDatasetVersion(v datalab.DatasetVersion) types.Row {
	return types.NewRow(
		types.MRP("drop", v.Drop),
		types.MRP("dataset", v.Dataset),
		types.MRP("version", v.Version),
		types.MRP("state", string(v.State)),
		types.MRP("file_count", v.FileCount),
		types.MRP("total_bytes", v.TotalBytes),
		types.MRP("created_at", datalab.FormatTime(v.CreatedAt)),
		types.MRP("committed_at", formatOptionalTime(v.CommittedAt)),
		types.MRP("manifest", decodeJSON(v.Manifest)),
	)
}

// RowForDeletedVersion projects the result of deleting a dataset version.
//
// A write verb that says what it wrote is composable; one that prints a
// sentence is not. `datalab dataset rm ... --format jsonl --output-fields version` is the reason
// this returns a row rather than a line of prose.
func RowForDeletedVersion(r datalab.DeleteDatasetVersionResult) types.Row {
	return types.NewRow(
		types.MRP("drop", r.Drop),
		types.MRP("dataset", r.Dataset),
		types.MRP("version", r.Version),
		types.MRP("deleted", r.Deleted),
	)
}

// RowForImportResult projects a materialization run.
func RowForImportResult(r datalab.ImportResult) types.Row {
	return types.NewRow(
		types.MRP("drop", r.Drop),
		types.MRP("dataset", r.Dataset),
		types.MRP("version", r.Version),
		types.MRP("path", r.Path),
		types.MRP("stream", r.Stream),
		types.MRP("rows", r.Rows),
		types.MRP("appended", r.Appended),
		types.MRP("skipped", r.Skipped),
		types.MRP("truncated", r.Truncated),
		types.MRP("warnings", r.WarningCount),
	)
}

// RowForGCResult projects a garbage-collection sweep.
func RowForGCResult(r client.GCResult) types.Row {
	return types.NewRow(
		types.MRP("scanned", r.Scanned),
		types.MRP("referenced", r.Referenced),
		types.MRP("deleted", r.Deleted),
		types.MRP("freed_bytes", r.FreedBytes),
	)
}

// RowForPrincipal projects what the configured credential resolves to.
//
// server is carried in the row rather than printed beside it because "which
// server did this answer come from" is the first thing a confused reader needs
// and the last thing they should have to reconstruct from their shell history.
func RowForPrincipal(server string, me client.Me) types.Row {
	row := types.NewRow(
		types.MRP("server", server),
		types.MRP("auth_mode", me.AuthMode),
		types.MRP("authenticated", me.Authenticated),
		types.MRP("kind", me.Kind),
	)

	var userID, email, name string
	if me.User != nil {
		userID, email, name = me.User.ID, me.User.Email, me.User.Name
	}
	row.Set("user_id", userID)
	row.Set("email", email)
	row.Set("name", name)
	row.Set("token_id", me.TokenID)
	row.Set("scopes", me.Scopes)

	issuer := ""
	if me.Provider != nil {
		issuer = me.Provider.Issuer
	}
	row.Set("issuer", issuer)
	return row
}

// formatOptionalTime renders an absent timestamp as an empty cell rather than
// as the zero instant, which would read as a real date in the year 1.
func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return datalab.FormatTime(*t)
}

// decodeJSON turns a stored JSON document into something a formatter can
// render, and nil when there is nothing there.
//
// A json.RawMessage handed straight to a row would print as a byte slice in a
// table cell. Decoding it means --format json emits a nested object and
// --format table emits a readable map.
func decodeJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	decoded, err := jsondoc.Value(raw)
	if err != nil {
		// Unparseable JSON from our own server is not worth an error path in a
		// projection; showing the bytes is more useful than showing nothing.
		return string(raw)
	}
	return decoded
}

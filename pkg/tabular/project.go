package tabular

import (
	"encoding/json"
	"io"
	"strconv"

	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

// FromEvents projects a page of envelopes into a table.
//
// It takes an already-fetched page rather than a query because the store's
// query path already owns ordering, time filters, cursors and the limit clamp
// (pkg/datadrop/query.go). Re-implementing any of that here would be a second
// answer to a question that already has one.
//
// Rows are returned in the order given. For a descending query that means
// newest first, which is wrong for a line chart — and is corrected in the
// browser, which sorts each line group along x anyway. Sorting in both places
// is how a chart ends up correct for only one of the two orderings.
func FromEvents(ref SourceRef, events []datadrop.Envelope) (Table, error) {
	b := newBuilder(ref, nil, EnvelopeColumns)

	var highest int64
	for _, event := range events {
		row := map[string]any{
			"id":          event.ID,
			"drop":        event.Drop,
			"stream":      event.Stream,
			"seq":         json.Number(strconv.FormatInt(event.Seq, 10)),
			"time":        canonicalTime(event.Time),
			"received_at": canonicalTime(event.ReceivedAt),
			"source":      event.Source,
			"type":        event.Type,
			"subject":     event.Subject,
		}
		// Flatten with an empty prefix and prepend DataPrefix afterwards, which
		// is exactly what the CSV export does when it builds its header. Doing
		// it the same way in the same order is what makes the two column sets
		// identical rather than merely similar.
		payload := map[string]any{}
		if err := FlattenValues("", event.Data, payload); err != nil {
			return Table{}, errors.Wrapf(err, "project event %s", event.ID)
		}
		for name, value := range payload {
			row[DataPrefix+name] = value
		}
		b.addRow(row)

		if event.Seq > highest {
			highest = event.Seq
		}
	}

	table := b.table(false, StrategyHead)
	table.NextAfter = highest
	return table, nil
}

// FromRows projects a row-oriented file into a table.
//
// props is the "properties" map of the dataset version's JSON Schema, or nil.
// When it names a column's type, that answer wins over anything observed in the
// values — see DR-2.
//
// The reader is consumed lazily and abandoned at the row cap, so a five-gigabyte
// file costs the bytes of the first `limit` rows and no more.
func FromRows(
	ref SourceRef, r io.Reader, format Format, limit int, props map[string]PropType,
) (Table, error) {
	if !format.Valid() {
		return Table{}, errors.Errorf(
			"unsupported row format %q: expected csv, ndjson, or json", string(format))
	}

	b := newBuilder(ref, escapedProperties(props), nil)

	opts := ReadOptions{
		MaxRows: ClampRows(limit),
		// A CSV author's column order carries meaning; preserve it rather than
		// alphabetizing the file's own layout away.
		OnHeader: func(columns []string) {
			escaped := make([]string, len(columns))
			for i, column := range columns {
				escaped[i] = escapeFlattenSegment(column)
			}
			b.setLeading(escaped)
		},
		TextColumns: textColumns(props),
	}

	truncated, err := ReadRows(r, format, opts, func(row int, payload json.RawMessage) error {
		values := map[string]any{}
		if err := FlattenValues("", payload, values); err != nil {
			return errors.Wrapf(err, "row %d", row)
		}
		b.addRow(values)
		return nil
	})
	if err != nil {
		return Table{}, err
	}

	return b.table(truncated, StrategyHead), nil
}

// escapedProperties maps schema metadata onto the same collision-free column
// names FlattenValues emits. CSV typing still receives the original names via
// textColumns because it runs before JSON-object flattening.
func escapedProperties(props map[string]PropType) map[string]PropType {
	if len(props) == 0 {
		return nil
	}
	out := make(map[string]PropType, len(props))
	for name, prop := range props {
		out[escapeFlattenSegment(name)] = prop
	}
	return out
}

// textColumns lists the columns a schema declares as strings.
//
// Typing them correctly is not enough on its own: a station_id of "001" is
// nominal either way, but if the reader has already turned it into the number 1
// then the leading zeros are gone and no downstream label can bring them back.
func textColumns(props map[string]PropType) map[string]struct{} {
	if len(props) == 0 {
		return nil
	}
	out := map[string]struct{}{}
	for name, prop := range props {
		if prop.Type == "string" {
			out[name] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

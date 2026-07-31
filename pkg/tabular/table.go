// Package tabular is the single projection from a datalab source to a table.
//
// A chart does not consume "a stream" or "a dataset". It consumes a table: an
// ordered list of named, typed columns plus rows. Both of datalab's storage
// shapes — the append-only event log and the immutable dataset version — reduce
// to that one type here, on the server, and nowhere else.
//
// The constraint that gives the package its value is that it is the *only*
// place the reduction happens. See DATADROP-3 guide §2 and DR-1: a second
// implementation in the browser would be a second answer to "what are the
// columns", with nothing forcing the two to agree.
//
// This package imports pkg/datalab and nothing else from this repository. In
// particular it does not import pkg/store, so its tests link no SQLite driver
// and run against literals.
package tabular

import "github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"

// FieldType is the visual-encoding type of a column.
//
// These three are not data types. They are the minimum a scale constructor
// needs to know: whether to build a continuous domain, a category band, or a
// time axis. The vocabulary is Wilkinson's, by way of Vega-Lite.
type FieldType string

const (
	// TypeQuantitative is a continuous magnitude: linear or log scale.
	TypeQuantitative FieldType = "q"
	// TypeNominal is an unordered category: band scale, discrete palette.
	TypeNominal FieldType = "n"
	// TypeTemporal is an instant: ordered, formatted as a date.
	TypeTemporal FieldType = "t"
)

// Valid reports whether t is one of the three encoding types.
func (t FieldType) Valid() bool {
	switch t {
	case TypeQuantitative, TypeNominal, TypeTemporal:
		return true
	default:
		return false
	}
}

// TypeSource records how a field's type was decided.
//
// This is not decoration. It is what lets a client say "typed from the dataset
// schema" rather than "guessed from 500 sampled values", which turns a wrong
// guess into a visible, reportable thing instead of a mystery. See guide §4.
type TypeSource string

const (
	// SourceSchema means the dataset version's JSON Schema named the type.
	SourceSchema TypeSource = "schema"
	// SourceEnvelope means the column is a fixed event-envelope field whose
	// type is a property of the envelope, not of any data.
	SourceEnvelope TypeSource = "envelope"
	// SourceValues means the type was inferred from the sampled values.
	SourceValues TypeSource = "values"
	// SourceDefault means there was nothing to go on.
	SourceDefault TypeSource = "default"
)

// Source kinds.
const (
	KindStream  = "stream"
	KindDataset = "dataset"
)

// Row-selection strategies, reported so a truncated table says not just that it
// was cut but which end it was cut from.
const (
	// StrategyHead returns the first N rows in source order.
	StrategyHead = "head"
	// StrategyLatest returns the N highest sequences — the right default for a
	// timeseries, which should open on the recent past rather than on the first
	// readings ever taken.
	StrategyLatest = "latest"
)

// Row budget. See guide §7 and DR-3.
const (
	// DefaultTableRows is what a caller gets without asking for a count.
	DefaultTableRows = 2_000

	// MaxStreamTableRows bounds event-stream windows. Streams can grow forever,
	// so they retain a deliberately smaller working set.
	MaxStreamTableRows = 50_000

	// MaxTableRows is the hard dataset-file cap. The browser no longer exposes
	// a row-budget control; a dataset view therefore loads up to one million
	// rows and reports truncation above that boundary.
	MaxTableRows = 1_000_000
)

// maxDistinctTracked bounds the per-column distinct-value set so that a column
// of unique identifiers cannot make the projection allocate proportionally to
// the row count. Past the bound Distinct reports the bound.
const maxDistinctTracked = 1_000

// SourceRef identifies where a table came from, precisely enough that a
// permalink can reopen it.
type SourceRef struct {
	Kind string `json:"kind"` // KindStream | KindDataset
	Drop string `json:"drop"`

	Stream string `json:"stream,omitempty"`

	Dataset string `json:"dataset,omitempty"`
	Version int    `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

// Field is one column.
type Field struct {
	Name         string     `json:"name"`
	Type         FieldType  `json:"type"`
	InferredFrom TypeSource `json:"inferred_from"`

	// Distinct is the number of distinct observed values, capped at
	// maxDistinctTracked. A client uses it to warn before it tries to draw a
	// legend with forty thousand entries.
	Distinct int `json:"distinct,omitempty"`

	// DistinctCapped reports that Distinct hit the tracking bound and is a
	// floor rather than a count.
	DistinctCapped bool `json:"distinct_capped,omitempty"`

	// NullCount is how many sampled rows had no value for this column.
	NullCount int `json:"null_count,omitempty"`
}

// Table is the single currency of the visualization layer.
//
// Rows are maps rather than positional slices. A columnar encoding would be
// smaller on the wire and is the right call at a hundred thousand rows; at the
// row budget this package actually ships the difference is a few hundred
// kilobytes, and the map form means an absent key is simply absent rather than
// needing a null sentinel. Revisit when a median response exceeds a couple of
// megabytes, not before.
type Table struct {
	Source SourceRef        `json:"source"`
	Fields []Field          `json:"fields"`
	Rows   []map[string]any `json:"rows"`

	RowCount int `json:"row_count"`

	// Truncated reports that the source held more rows than the budget allowed.
	// It is never omitted when true: a truncated chart that looks complete is a
	// wrong answer with a picture attached.
	Truncated bool `json:"truncated"`

	// Strategy names how the returned rows were selected.
	Strategy string `json:"strategy"`

	// NextAfter, for stream sources, is the cursor a live tail resumes from.
	NextAfter int64 `json:"next_after,omitempty"`
}

// Field returns the named column, if the table has it.
func (t Table) Field(name string) (Field, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// EnvelopeColumns are the fixed leading columns of any table projected from
// events, in order. Payload columns follow, prefixed "data." and sorted.
//
// The CSV export uses this same list for its header (see
// pkg/server/handlers_export.go), which is what makes "download this chart's
// data" produce a file whose columns match the chart.
var EnvelopeColumns = []string{
	"id", "drop", "stream", "seq", "time", "received_at", "source", "type", "subject",
}

// DataPrefix is prepended to every flattened payload column of an event table.
const DataPrefix = "data."

// envelopeTypes fixes the encoding type of each envelope column. These are
// properties of the envelope shape (pkg/datalab/event.go), not observations,
// so they are never inferred.
var envelopeTypes = map[string]FieldType{
	"id":          TypeNominal,
	"drop":        TypeNominal,
	"stream":      TypeNominal,
	"seq":         TypeQuantitative,
	"time":        TypeTemporal,
	"received_at": TypeTemporal,
	"source":      TypeNominal,
	"type":        TypeNominal,
	"subject":     TypeNominal,
}

// ClampRows applies the row budget: a non-positive request gets the default, an
// over-large one gets the cap.
func ClampRows(requested int) int {
	switch {
	case requested <= 0:
		return DefaultTableRows
	case requested > MaxTableRows:
		return MaxTableRows
	default:
		return requested
	}
}

// canonicalTime renders an envelope timestamp the way every other layer does.
var canonicalTime = datalab.FormatTime

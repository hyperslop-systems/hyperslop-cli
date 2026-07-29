package tabular

import (
	"sort"
)

// builder accumulates rows and per-column statistics, then resolves the field
// list once at the end.
//
// Resolving types at the end rather than per row is not an optimization: a
// column's type is a property of the whole sample, and deciding it from the
// first row would make the answer depend on row order.
type builder struct {
	ref   SourceRef
	props map[string]PropType

	// leading are columns emitted first, in this order: the envelope columns of
	// an event table, or the header of a CSV. Everything else is sorted by name
	// and appended, because Go map iteration order is not a column order.
	leading []string

	stats map[string]*columnStats
	rows  []map[string]any
}

func newBuilder(ref SourceRef, props map[string]PropType, leading []string) *builder {
	b := &builder{
		ref:     ref,
		props:   props,
		leading: append([]string(nil), leading...),
		stats:   map[string]*columnStats{},
	}
	// Seed the leading columns so they appear even in a table with zero rows.
	// An empty result that still shows its shape is more useful than an empty
	// result that shows nothing.
	for _, name := range b.leading {
		b.column(name)
	}
	return b
}

func (b *builder) column(name string) *columnStats {
	if stats, ok := b.stats[name]; ok {
		return stats
	}
	stats := newColumnStats()
	b.stats[name] = stats
	return stats
}

// setLeading replaces the leading column order. Used by the CSV path, which
// only learns the header after the reader has started.
func (b *builder) setLeading(columns []string) {
	b.leading = append([]string(nil), columns...)
	for _, name := range b.leading {
		b.column(name)
	}
}

// addRow records one row.
//
// It does not count absences here. A column discovered in row 900 would then
// show 0 nulls for the 899 rows that predate it, which is exactly backwards.
// NullCount is derived once at the end, from the total row count.
func (b *builder) addRow(row map[string]any) {
	for name, value := range row {
		b.column(name).observe(value)
	}
	b.rows = append(b.rows, row)
}

// table resolves the field list and returns the finished projection.
func (b *builder) table(truncated bool, strategy string) Table {
	names := b.orderedNames()

	fields := make([]Field, 0, len(names))
	for _, name := range names {
		stats := b.stats[name]

		var prop *PropType
		if b.props != nil {
			if p, ok := b.props[name]; ok {
				prop = &p
			}
		}

		fieldType, from := inferType(name, prop, stats)
		field := Field{Name: name, Type: fieldType, InferredFrom: from}
		if stats != nil {
			field.Distinct = len(stats.distinct)
			field.DistinctCapped = stats.distinctCapped
			// Absent, JSON null and empty string all mean "no usable value
			// here"; the count is over every row in the table, including those
			// that predate the column's first appearance.
			field.NullCount = len(b.rows) - stats.observed()
		}
		fields = append(fields, field)
	}

	rows := b.rows
	if rows == nil {
		// Marshal as [] rather than null: a client that iterates the rows of an
		// empty table should not have to special-case a missing array.
		rows = []map[string]any{}
	}

	return Table{
		Source:    b.ref,
		Fields:    fields,
		Rows:      rows,
		RowCount:  len(rows),
		Truncated: truncated,
		Strategy:  strategy,
	}
}

func (b *builder) orderedNames() []string {
	inLeading := make(map[string]struct{}, len(b.leading))
	names := make([]string, 0, len(b.stats))

	for _, name := range b.leading {
		if _, dup := inLeading[name]; dup {
			continue
		}
		if _, known := b.stats[name]; !known {
			continue
		}
		inLeading[name] = struct{}{}
		names = append(names, name)
	}

	rest := make([]string, 0, len(b.stats))
	for name := range b.stats {
		if _, leading := inLeading[name]; leading {
			continue
		}
		rest = append(rest, name)
	}
	sort.Strings(rest)

	return append(names, rest...)
}

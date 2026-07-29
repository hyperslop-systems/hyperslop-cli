package tabular

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

func TestCSVValueTyping(t *testing.T) {
	for input, want := range map[string]any{
		"21.7":                 json.Number("21.7"),
		"-3":                   json.Number("-3"),
		"1e3":                  json.Number("1e3"),
		"9007199254740993":     json.Number("9007199254740993"),
		"":                     "",
		"warm":                 "warm",
		"true":                 true,
		"false":                false,
		"2026-07-01T00:00:00Z": "2026-07-01T00:00:00Z",
		// Leading-zero integers are not JSON numbers. Preserve their lexical
		// form instead of silently rewriting an identifier.
		"001": "001",
		// ParseFloat accepts hex float syntax; JSON does not represent it, and
		// a value like this is far more likely to be an identifier.
		"0x1p-2": "0x1p-2",
	} {
		if got := CSVValue(input); got != want {
			t.Errorf("CSVValue(%q) = %v (%T), want %v (%T)", input, got, got, want, want)
		}
	}
}

func TestReadCSVRowsRejectsDuplicateEffectiveHeaders(t *testing.T) {
	for name, input := range map[string]string{
		"literal duplicate":       "a,a\n1,2\n",
		"generated and explicit":  ",column_1\n1,2\n",
		"explicit then generated": "column_2,\n1,2\n",
		"long row generated name": "column_2\n1,2\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ReadRows(
				strings.NewReader(input),
				FormatCSV,
				ReadOptions{MaxRows: 10},
				func(int, json.RawMessage) error { return nil },
			)
			if err == nil || !strings.Contains(err.Error(), "duplicate effective") {
				t.Fatalf("ReadRows error = %v, want duplicate effective column", err)
			}
		})
	}
}

func TestFromRowsPreservesCSVNumericLexemes(t *testing.T) {
	table, err := FromRows(
		SourceRef{},
		strings.NewReader("large,precise,exponent,identifier\n9007199254740993,0.1234567890123456789,1e3,001\n"),
		FormatCSV,
		10,
		nil,
	)
	if err != nil {
		t.Fatalf("FromRows: %v", err)
	}

	for column, want := range map[string]string{
		"large":    "9007199254740993",
		"precise":  "0.1234567890123456789",
		"exponent": "1e3",
	} {
		got, ok := table.Rows[0][column].(json.Number)
		if !ok || got.String() != want {
			t.Errorf("%s = %v (%T), want json.Number(%q)",
				column, table.Rows[0][column], table.Rows[0][column], want)
		}
	}
	if got := table.Rows[0]["identifier"]; got != "001" {
		t.Fatalf("identifier = %v (%T), want preserved string \"001\"", got, got)
	}
}

func TestFormatFromPath(t *testing.T) {
	for _, tc := range []struct {
		path, mediaType string
		want            Format
	}{
		{"data.csv", "", FormatCSV},
		{"data.ndjson", "", FormatNDJSON},
		{"data.jsonl", "", FormatNDJSON},
		{"data.json", "", FormatJSON},
		{"data", "text/csv", FormatCSV},
		{"data", "text/csv; charset=utf-8", FormatCSV},
		{"data", "Application/JSON; Charset=UTF-8", FormatJSON},
		{"data", "application/x-ndjson", FormatNDJSON},
		{"data", "application/json", FormatJSON},
		{"data", "", ""},
		{"data.txt", "", ""},
	} {
		if got := FormatFromPath(tc.path, tc.mediaType); got != tc.want {
			t.Errorf("FormatFromPath(%q, %q) = %q, want %q", tc.path, tc.mediaType, got, tc.want)
		}
	}
}

// Flattening produces the same column names for the string and the value form.
// The CSV export uses the first and the table projection uses the second, so a
// divergence here is a chart whose "download as CSV" has different columns.
func TestFlattenFormsAgreeOnColumnNames(t *testing.T) {
	payload := json.RawMessage(`{
		"temperature": 21.5,
		"location": {"lat": 52.1, "lon": 4.3},
		"tags": ["a", "b"],
		"note": null,
		"empty": {},
		"ok": true
	}`)

	strs := map[string]string{}
	if err := FlattenStrings("", payload, strs); err != nil {
		t.Fatalf("FlattenStrings: %v", err)
	}
	vals := map[string]any{}
	if err := FlattenValues("", payload, vals); err != nil {
		t.Fatalf("FlattenValues: %v", err)
	}

	if len(strs) != len(vals) {
		t.Fatalf("column counts differ: strings %d, values %d", len(strs), len(vals))
	}
	for name := range strs {
		if _, ok := vals[name]; !ok {
			t.Errorf("column %q present in the string form but not the value form", name)
		}
	}

	for name, want := range map[string]string{
		"temperature":  "21.5",
		"location.lat": "52.1",
		"location.lon": "4.3",
		"tags":         `["a","b"]`,
		"note":         "",
		"empty":        "{}",
		"ok":           "true",
	} {
		if got := strs[name]; got != want {
			t.Errorf("cell %q = %q, want %q", name, got, want)
		}
	}
}

func TestFlattenEscapesLiteralPathSeparatorsWithoutCollisions(t *testing.T) {
	payload := json.RawMessage(`{"a.b":1,"a":{"b":2},"a\\b":3,"":{"a":4},"\\0":5,"empty":null}`)

	values := map[string]any{}
	if err := FlattenValues("", payload, values); err != nil {
		t.Fatalf("FlattenValues: %v", err)
	}
	for key, want := range map[string]string{
		`a\.b`: "1", // literal key "a.b"
		`a.b`:  "2", // nested path a -> b
		`a\\b`: "3", // literal backslash is escaped too
		`\0.a`: "4", // empty key segment is explicit
		`\\0`:  "5", // literal "\\0" remains distinct from empty
	} {
		got, ok := values[key].(json.Number)
		if !ok || got.String() != want {
			t.Errorf("column %q = %v (%T), want json.Number(%s)", key, values[key], values[key], want)
		}
	}
	if value, present := values["empty"]; !present || value != nil {
		t.Fatalf("nested null = %v (present=%v), want an explicit nil leaf", value, present)
	}
	if len(values) != 6 {
		t.Fatalf("flattened columns = %v, want six collision-free paths", values)
	}

	stringsForm := map[string]string{}
	if err := FlattenStrings("", payload, stringsForm); err != nil {
		t.Fatalf("FlattenStrings: %v", err)
	}
	for key := range values {
		if _, ok := stringsForm[key]; !ok {
			t.Errorf("FlattenStrings omitted escaped column %q", key)
		}
	}
}

// A JSON number keeps every digit it arrived with. Round-tripping through
// float64 would quietly truncate a nanosecond timestamp or a large identifier.
func TestFlattenValuesPreserveNumericPrecision(t *testing.T) {
	vals := map[string]any{}
	if err := FlattenValues("", json.RawMessage(`{"n": 12345678901234567}`), vals); err != nil {
		t.Fatalf("FlattenValues: %v", err)
	}
	number, ok := vals["n"].(json.Number)
	if !ok {
		t.Fatalf("n is %T, want json.Number", vals["n"])
	}
	if number.String() != "12345678901234567" {
		t.Fatalf("n = %s, want 12345678901234567", number)
	}
}

// A top-level JSON null contributes no column; a nested one contributes an
// empty column. Both behaviours are load-bearing for the CSV export.
func TestFlattenTopLevelNullProducesNoColumn(t *testing.T) {
	vals := map[string]any{}
	if err := FlattenValues("", json.RawMessage(`null`), vals); err != nil {
		t.Fatalf("FlattenValues: %v", err)
	}
	if len(vals) != 0 {
		t.Fatalf("got %d columns, want 0: %v", len(vals), vals)
	}
}

func TestSchemaPropertiesAcceptsBothTypeSpellings(t *testing.T) {
	props := SchemaProperties(json.RawMessage(`{
		"type": "object",
		"properties": {
			"a": {"type": "number"},
			"b": {"type": ["null", "string"], "format": "date-time"},
			"c": {"type": ["null"]},
			"d": {}
		}
	}`))

	if got := props["a"].Type; got != "number" {
		t.Errorf("a.Type = %q, want number", got)
	}
	if got := props["b"].Type; got != "string" {
		t.Errorf("b.Type = %q, want string", got)
	}
	if got := props["b"].Format; got != "date-time" {
		t.Errorf("b.Format = %q, want date-time", got)
	}
	// A type list holding only "null" names nothing usable.
	if got := props["c"].Type; got != "" {
		t.Errorf("c.Type = %q, want empty", got)
	}
	if got := props["d"].Type; got != "" {
		t.Errorf("d.Type = %q, want empty", got)
	}
}

func TestSchemaPropertiesOnAbsentOrInvalidSchema(t *testing.T) {
	for name, raw := range map[string]string{
		"absent":     ``,
		"null":       `null`,
		"array":      `[1,2,3]`,
		"no props":   `{"type": "object"}`,
		"not a doc":  `not json`,
		"empty prop": `{"properties": {}}`,
	} {
		if got := SchemaProperties(json.RawMessage(raw)); got != nil {
			t.Errorf("%s: got %v, want nil", name, got)
		}
	}
}

// The whole of DR-2: the schema wins over the values, the values win over the
// default, and the field says which happened.
func TestInferencePrecedence(t *testing.T) {
	// A zero-padded identifier column. Sniffing calls it quantitative; the
	// schema calls it a string; the schema must win.
	rows := "station_id,reading\n01,3\n02,4\n03,5\n"
	schema := json.RawMessage(`{"properties": {"station_id": {"type": "string"}}}`)

	table, err := FromRows(
		SourceRef{Kind: KindDataset, Drop: "lab"},
		strings.NewReader(rows), FormatCSV, 10, SchemaProperties(schema))
	if err != nil {
		t.Fatalf("FromRows: %v", err)
	}

	station, ok := table.Field("station_id")
	if !ok {
		t.Fatal("station_id column is missing")
	}
	if station.Type != TypeNominal || station.InferredFrom != SourceSchema {
		t.Fatalf("station_id = (%s, %s), want (n, schema)", station.Type, station.InferredFrom)
	}

	reading, ok := table.Field("reading")
	if !ok {
		t.Fatal("reading column is missing")
	}
	if reading.Type != TypeQuantitative || reading.InferredFrom != SourceValues {
		t.Fatalf("reading = (%s, %s), want (q, values)", reading.Type, reading.InferredFrom)
	}
}

func TestInferenceFromValues(t *testing.T) {
	for _, tc := range []struct {
		name     string
		csv      string
		column   string
		wantType FieldType
		wantFrom TypeSource
	}{
		{
			name:     "all numbers",
			csv:      "v\n1\n2\n3\n",
			column:   "v",
			wantType: TypeQuantitative, wantFrom: SourceValues,
		},
		{
			name:     "all booleans",
			csv:      "v\ntrue\nfalse\ntrue\n",
			column:   "v",
			wantType: TypeNominal, wantFrom: SourceValues,
		},
		{
			name:     "timestamps",
			csv:      "v\n2026-07-01T00:00:00Z\n2026-07-02T00:00:00Z\n2026-07-03T00:00:00Z\n",
			column:   "v",
			wantType: TypeTemporal, wantFrom: SourceValues,
		},
		{
			// Four dates and one label: 80% exactly, which is not > 80%.
			name:     "timestamps below the tolerance",
			csv:      "v\n2026-07-01\n2026-07-02\n2026-07-03\n2026-07-04\nunknown\n",
			column:   "v",
			wantType: TypeNominal, wantFrom: SourceValues,
		},
		{
			// Five dates and one label: 83%, which clears the tolerance.
			name:     "timestamps above the tolerance",
			csv:      "v\n2026-07-01\n2026-07-02\n2026-07-03\n2026-07-04\n2026-07-05\nunknown\n",
			column:   "v",
			wantType: TypeTemporal, wantFrom: SourceValues,
		},
		{
			// Two dates is below the minimum sample; one date out of one is
			// 100% and means nothing.
			name:     "too few values to call temporal",
			csv:      "v\n2026-07-01\n2026-07-02\n",
			column:   "v",
			wantType: TypeNominal, wantFrom: SourceValues,
		},
		{
			name:     "mixed numbers and words",
			csv:      "v\n1\nwarm\n3\n",
			column:   "v",
			wantType: TypeNominal, wantFrom: SourceValues,
		},
		{
			name:     "nothing observed",
			csv:      "v\n\n\n",
			column:   "v",
			wantType: TypeNominal, wantFrom: SourceDefault,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table, err := FromRows(SourceRef{}, strings.NewReader(tc.csv), FormatCSV, 100, nil)
			if err != nil {
				t.Fatalf("FromRows: %v", err)
			}
			field, ok := table.Field(tc.column)
			if !ok {
				t.Fatalf("column %q is missing; fields are %+v", tc.column, table.Fields)
			}
			if field.Type != tc.wantType || field.InferredFrom != tc.wantFrom {
				t.Fatalf("(%s, %s), want (%s, %s)",
					field.Type, field.InferredFrom, tc.wantType, tc.wantFrom)
			}
		})
	}
}

// Epoch seconds are quantitative until a human says otherwise: guessing that a
// column of counts is a date turns a readable scatter into an unreadable axis.
func TestInstantRecognitionIsStrict(t *testing.T) {
	for _, s := range []string{
		"2026-13-45",
		"1753372800",
		"July 24, 2026",
		"2026/07/24",
		"",
	} {
		if isInstant(s) {
			t.Errorf("isInstant(%q) = true, want false", s)
		}
	}
	for _, s := range []string{
		"2026-07-24",
		"2026-07-24T15:04:05Z",
		"2026-07-24T15:04:05.123Z",
		"2026-07-24T15:04:05+02:00",
	} {
		if !isInstant(s) {
			t.Errorf("isInstant(%q) = false, want true", s)
		}
	}
}

func TestFromRowsEscapesCSVHeadersAndSchemaPropertiesConsistently(t *testing.T) {
	table, err := FromRows(
		SourceRef{}, strings.NewReader("location.lat,value\\path\nnorth,001\n"), FormatCSV, 10,
		map[string]PropType{
			"location.lat": {Type: "string"},
			"value\\path":  {Type: "string"},
		},
	)
	if err != nil {
		t.Fatalf("FromRows: %v", err)
	}
	want := []string{`location\.lat`, `value\\path`}
	if len(table.Fields) != len(want) {
		t.Fatalf("fields = %+v, want %v", table.Fields, want)
	}
	for i, name := range want {
		if table.Fields[i].Name != name {
			t.Errorf("field %d = %q, want %q", i, table.Fields[i].Name, name)
		}
		if table.Fields[i].InferredFrom != SourceSchema {
			t.Errorf("field %q inferred from %s, want schema", name, table.Fields[i].InferredFrom)
		}
		if _, ok := table.Rows[0][name]; !ok {
			t.Errorf("row has no value for escaped field %q: %v", name, table.Rows[0])
		}
	}
	if len(table.Rows[0]) != 2 {
		t.Fatalf("row contains duplicate escaped/unescaped columns: %v", table.Rows[0])
	}
	if got := table.Rows[0][`value\\path`]; got != "001" {
		t.Fatalf("schema-declared text value = %v (%T), want preserved 001", got, got)
	}
}

func TestFromRowsPreservesCSVHeaderOrder(t *testing.T) {
	// Alphabetically these sort z, m, a — the opposite of the file's order, so
	// a sorted field list is unmistakable.
	table, err := FromRows(SourceRef{}, strings.NewReader("z,m,a\n1,2,3\n"), FormatCSV, 10, nil)
	if err != nil {
		t.Fatalf("FromRows: %v", err)
	}
	want := []string{"z", "m", "a"}
	if len(table.Fields) != len(want) {
		t.Fatalf("got %d fields, want %d", len(table.Fields), len(want))
	}
	for i, name := range want {
		if table.Fields[i].Name != name {
			t.Fatalf("field %d = %q, want %q", i, table.Fields[i].Name, name)
		}
	}
}

func TestFromRowsTruncates(t *testing.T) {
	var b strings.Builder
	b.WriteString("v\n")
	for i := 0; i < 20; i++ {
		b.WriteString("1\n")
	}

	table, err := FromRows(SourceRef{}, strings.NewReader(b.String()), FormatCSV, 5, nil)
	if err != nil {
		t.Fatalf("FromRows: %v", err)
	}
	if !table.Truncated {
		t.Fatal("Truncated is false after stopping at the cap")
	}
	if table.RowCount != 5 {
		t.Fatalf("RowCount = %d, want 5", table.RowCount)
	}
	if table.Strategy != StrategyHead {
		t.Fatalf("Strategy = %q, want %q", table.Strategy, StrategyHead)
	}
}

// Exactly-at-the-cap must not be reported as truncated: a file with five rows
// read with a cap of five was read completely.
func TestFromRowsExactlyAtTheCapIsNotTruncated(t *testing.T) {
	table, err := FromRows(SourceRef{}, strings.NewReader("v\n1\n2\n3\n4\n5\n"), FormatCSV, 5, nil)
	if err != nil {
		t.Fatalf("FromRows: %v", err)
	}
	if table.Truncated {
		t.Fatal("Truncated is true for a file that ended exactly at the cap")
	}
	if table.RowCount != 5 {
		t.Fatalf("RowCount = %d, want 5", table.RowCount)
	}
}

func TestFromRowsRejectsUnterminatedOrTrailingJSONArrayData(t *testing.T) {
	for name, input := range map[string]string{
		"missing closing bracket": `[1,2`,
		"trailing garbage":        `[1,2] garbage`,
		"second document":         `[1,2] []`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := FromRows(SourceRef{}, strings.NewReader(input), FormatJSON, 10, nil); err == nil {
				t.Fatalf("FromRows accepted %q", input)
			}
		})
	}
}

func TestFromRowsNDJSONAndJSONAgree(t *testing.T) {
	ndjson := `{"a":1,"b":"x"}
{"a":2,"b":"y"}
`
	jsonArray := `[{"a":1,"b":"x"},{"a":2,"b":"y"}]`

	fromND, err := FromRows(SourceRef{}, strings.NewReader(ndjson), FormatNDJSON, 10, nil)
	if err != nil {
		t.Fatalf("ndjson: %v", err)
	}
	fromArray, err := FromRows(SourceRef{}, strings.NewReader(jsonArray), FormatJSON, 10, nil)
	if err != nil {
		t.Fatalf("json: %v", err)
	}

	if len(fromND.Fields) != len(fromArray.Fields) {
		t.Fatalf("field counts differ: %d vs %d", len(fromND.Fields), len(fromArray.Fields))
	}
	for i := range fromND.Fields {
		if fromND.Fields[i] != fromArray.Fields[i] {
			t.Fatalf("field %d differs: %+v vs %+v", i, fromND.Fields[i], fromArray.Fields[i])
		}
	}
	if fromND.RowCount != 2 || fromArray.RowCount != 2 {
		t.Fatalf("row counts: ndjson %d, json %d", fromND.RowCount, fromArray.RowCount)
	}
}

func TestFromRowsEmptyFile(t *testing.T) {
	for _, tc := range []struct {
		format Format
		body   string
	}{
		{FormatCSV, ""},
		{FormatNDJSON, ""},
		{FormatJSON, ""},
		{FormatJSON, "[]"},
	} {
		table, err := FromRows(SourceRef{}, strings.NewReader(tc.body), tc.format, 10, nil)
		if err != nil {
			t.Fatalf("%s %q: %v", tc.format, tc.body, err)
		}
		if table.RowCount != 0 {
			t.Errorf("%s %q: RowCount = %d, want 0", tc.format, tc.body, table.RowCount)
		}
		if table.Rows == nil {
			t.Errorf("%s %q: Rows is nil, want an empty slice", tc.format, tc.body)
		}
	}
}

func TestFromRowsRejectsANonArrayJSONDocument(t *testing.T) {
	_, err := FromRows(SourceRef{}, strings.NewReader(`{"a":1}`), FormatJSON, 10, nil)
	if err == nil {
		t.Fatal("a top-level object was accepted as a JSON array file")
	}
}

func TestFromEventsColumnOrderAndTypes(t *testing.T) {
	at := time.Date(2026, 7, 24, 15, 4, 5, 0, time.UTC)
	events := []datadrop.Envelope{
		{
			SpecVersion: datadrop.SpecVersion,
			ID:          "01J",
			Drop:        "lab",
			Stream:      "temps",
			Seq:         41,
			Time:        at,
			ReceivedAt:  at,
			Data:        json.RawMessage(`{"temp_c": 21.5, "station": "north"}`),
		},
		{
			ID: "01K", Drop: "lab", Stream: "temps", Seq: 42, Time: at, ReceivedAt: at,
			Data: json.RawMessage(`{"temp_c": 22.0, "station": "south", "note": "recalibrated"}`),
		},
	}

	table, err := FromEvents(SourceRef{Kind: KindStream, Drop: "lab", Stream: "temps"}, events)
	if err != nil {
		t.Fatalf("FromEvents: %v", err)
	}

	for i, want := range EnvelopeColumns {
		if table.Fields[i].Name != want {
			t.Fatalf("field %d = %q, want %q", i, table.Fields[i].Name, want)
		}
	}

	payload := table.Fields[len(EnvelopeColumns):]
	wantPayload := []string{"data.note", "data.station", "data.temp_c"}
	if len(payload) != len(wantPayload) {
		t.Fatalf("got %d payload fields, want %d: %+v", len(payload), len(wantPayload), payload)
	}
	for i, want := range wantPayload {
		if payload[i].Name != want {
			t.Fatalf("payload field %d = %q, want %q", i, payload[i].Name, want)
		}
	}

	for name, want := range map[string]FieldType{
		"seq": TypeQuantitative, "time": TypeTemporal, "received_at": TypeTemporal,
		"id": TypeNominal, "data.temp_c": TypeQuantitative, "data.station": TypeNominal,
	} {
		field, ok := table.Field(name)
		if !ok {
			t.Fatalf("field %q is missing", name)
		}
		if field.Type != want {
			t.Errorf("%s type = %s, want %s", name, field.Type, want)
		}
	}

	// Envelope columns are typed by the envelope's shape, never by observation.
	seq, _ := table.Field("seq")
	if seq.InferredFrom != SourceEnvelope {
		t.Errorf("seq inferred from %s, want envelope", seq.InferredFrom)
	}

	// A payload key only the second event had is missing from the first row,
	// not null-filled, and is counted as one null.
	note, _ := table.Field("data.note")
	if note.NullCount != 1 {
		t.Errorf("data.note NullCount = %d, want 1", note.NullCount)
	}
	if _, present := table.Rows[0]["data.note"]; present {
		t.Error("data.note was materialized into a row that did not have it")
	}

	if table.NextAfter != 42 {
		t.Errorf("NextAfter = %d, want 42", table.NextAfter)
	}
}

// An empty result still reports its shape: nine envelope columns and no rows.
func TestFromEventsWithNoEvents(t *testing.T) {
	table, err := FromEvents(SourceRef{Kind: KindStream, Drop: "lab"}, nil)
	if err != nil {
		t.Fatalf("FromEvents: %v", err)
	}
	if len(table.Fields) != len(EnvelopeColumns) {
		t.Fatalf("got %d fields, want %d", len(table.Fields), len(EnvelopeColumns))
	}
	if table.RowCount != 0 || table.Rows == nil {
		t.Fatalf("RowCount = %d, Rows = %v", table.RowCount, table.Rows)
	}
	if table.NextAfter != 0 {
		t.Fatalf("NextAfter = %d, want 0", table.NextAfter)
	}
}

// Timestamps in a table are rendered exactly as the database and the CSV export
// render them, so the browser sees one format rather than three.
func TestFromEventsUsesTheCanonicalTimeFormat(t *testing.T) {
	at := time.Date(2026, 7, 24, 15, 4, 5, 100_000_000, time.UTC)
	table, err := FromEvents(SourceRef{}, []datadrop.Envelope{{ID: "x", Time: at, ReceivedAt: at}})
	if err != nil {
		t.Fatalf("FromEvents: %v", err)
	}
	if got := table.Rows[0]["time"]; got != "2026-07-24T15:04:05.100Z" {
		t.Fatalf("time = %v, want 2026-07-24T15:04:05.100Z", got)
	}
}

func TestClampRows(t *testing.T) {
	for input, want := range map[int]int{
		0:                  DefaultTableRows,
		-1:                 DefaultTableRows,
		10:                 10,
		MaxTableRows:       MaxTableRows,
		MaxTableRows + 1:   MaxTableRows,
		MaxTableRows * 100: MaxTableRows,
	} {
		if got := ClampRows(input); got != want {
			t.Errorf("ClampRows(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestDistinctTrackingIsCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString("v\n")
	for i := 0; i < maxDistinctTracked+50; i++ {
		b.WriteString("id-")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString(itoaTest(i))
		b.WriteString("\n")
	}

	table, err := FromRows(SourceRef{}, strings.NewReader(b.String()), FormatCSV, MaxTableRows, nil)
	if err != nil {
		t.Fatalf("FromRows: %v", err)
	}
	field, _ := table.Field("v")
	if field.Distinct != maxDistinctTracked {
		t.Fatalf("Distinct = %d, want %d", field.Distinct, maxDistinctTracked)
	}
	if !field.DistinctCapped {
		t.Fatal("DistinctCapped is false after exceeding the tracking bound")
	}
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var out []byte
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	return string(out)
}

// Typing the column correctly is not enough. A zero-padded identifier that the
// reader has already turned into a number has lost its padding permanently, so
// the schema has to reach the reader and not just the field list.
func TestSchemaStringColumnsKeepTheirText(t *testing.T) {
	rows := "station_id,reading\n001,3\n002,4\n003,5\n"
	schema := json.RawMessage(`{"properties": {"station_id": {"type": "string"}}}`)

	table, err := FromRows(SourceRef{}, strings.NewReader(rows), FormatCSV, 10,
		SchemaProperties(schema))
	if err != nil {
		t.Fatalf("FromRows: %v", err)
	}

	if got := table.Rows[0]["station_id"]; got != "001" {
		t.Fatalf("station_id = %v (%T), want the string \"001\"", got, got)
	}
	// The neighbouring column still gets typed, so this is a targeted exception
	// rather than a switch that turns typing off.
	if got, ok := table.Rows[0]["reading"].(json.Number); !ok || got.String() != "3" {
		t.Fatalf("reading = %v (%T), want the number 3", table.Rows[0]["reading"], table.Rows[0]["reading"])
	}
}

// Without a schema the reader still types valid JSON numbers, but it must not
// destroy a non-JSON lexical form such as a zero-padded identifier.
func TestWithoutASchemaPreservesNonJSONNumericLexemes(t *testing.T) {
	table, err := FromRows(SourceRef{}, strings.NewReader("v\n001\n002\n"), FormatCSV, 10, nil)
	if err != nil {
		t.Fatalf("FromRows: %v", err)
	}
	if got := table.Rows[0]["v"]; got != "001" {
		t.Fatalf("v = %v (%T), want the preserved string \"001\"", got, got)
	}
}

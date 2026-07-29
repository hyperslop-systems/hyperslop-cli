package tabular

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"io"
	"mime"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// Format is a row-oriented file format this package can read.
type Format string

const (
	// FormatCSV is a header row followed by data rows.
	FormatCSV Format = "csv"
	// FormatNDJSON is one JSON document per line.
	FormatNDJSON Format = "ndjson"
	// FormatJSON is a single top-level JSON array of objects.
	FormatJSON Format = "json"
)

// Valid reports whether f names a readable format.
func (f Format) Valid() bool {
	switch f {
	case FormatCSV, FormatNDJSON, FormatJSON:
		return true
	default:
		return false
	}
}

// FormatFromPath guesses the row format from a logical path or a media type.
// It returns the empty Format when it cannot tell, which callers must treat as
// "ask the client to say" rather than as a default.
func FormatFromPath(logicalPath, mediaType string) Format {
	lower := strings.ToLower(logicalPath)
	switch {
	case strings.HasSuffix(lower, ".csv"):
		return FormatCSV
	case strings.HasSuffix(lower, ".ndjson"), strings.HasSuffix(lower, ".jsonl"):
		return FormatNDJSON
	case strings.HasSuffix(lower, ".json"):
		return FormatJSON
	}

	parsedMediaType := strings.TrimSpace(mediaType)
	if parsedMediaType != "" {
		if baseType, _, err := mime.ParseMediaType(parsedMediaType); err == nil {
			parsedMediaType = baseType
		}
	}
	switch strings.ToLower(parsedMediaType) {
	case "text/csv":
		return FormatCSV
	case "application/x-ndjson", "application/jsonl":
		return FormatNDJSON
	case "application/json":
		return FormatJSON
	}
	return ""
}

// RowEmitter receives one parsed row. row is 1-based.
type RowEmitter func(row int, payload json.RawMessage) error

// ReadOptions configures a row read.
type ReadOptions struct {
	// MaxRows bounds the read. Reaching it stops the read and reports
	// truncation rather than erroring, because a bounded sample of a large file
	// is a useful answer and a 500 is not.
	MaxRows int

	// OnHeader, when set, receives the CSV header before any row is emitted.
	// It exists so that a caller can preserve the author's column order, which
	// is lost once each row becomes a JSON object.
	OnHeader func(columns []string)

	// TextColumns names CSV columns whose cells must be kept as written.
	//
	// CSV has no types, so the reader guesses — and its guess destroys data when
	// the column is a zero-padded identifier: "001" becomes the number 1 and the
	// padding is gone for good. A schema that declares the column a string is
	// the producer saying so, and it must reach the reader, because by the time
	// the row is a JSON object the original text no longer exists.
	TextColumns map[string]struct{}
}

// ReadRows dispatches on format. It never buffers the whole input: every reader
// here is streaming, because a dataset file can be gigabytes and the blob store
// exists precisely so that it never has to be resident.
func ReadRows(r io.Reader, format Format, opts ReadOptions, emit RowEmitter) (bool, error) {
	if opts.MaxRows <= 0 {
		opts.MaxRows = DefaultTableRows
	}
	switch format {
	case FormatCSV:
		return ReadCSVRows(r, opts, emit)
	case FormatNDJSON:
		return ReadNDJSONRows(r, opts, emit)
	case FormatJSON:
		return ReadJSONArrayRows(r, opts, emit)
	default:
		return false, errors.Errorf("unsupported row format %q: expected csv, ndjson, or json", string(format))
	}
}

// ReadCSVRows turns each data row into a JSON object keyed by the header.
func ReadCSVRows(body io.Reader, opts ReadOptions, emit RowEmitter) (bool, error) {
	// Bound each underlying read to one physical line. encoding/csv wraps its
	// input in an internal bufio.Reader; without this boundary it may prefetch
	// the record after MaxRows into an inaccessible buffer, forcing us to decode
	// that record merely to distinguish exact-cap from truncated input.
	source := newPhysicalLineReader(body)
	reader := csv.NewReader(source)
	// Rows are allowed to differ in length; a short or long row is a data
	// problem to report, not a parse error that aborts the file.
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true

	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil // an empty file reads zero rows
		}
		return false, errors.Wrap(err, "read CSV header")
	}

	columns := make([]string, len(header))
	copy(columns, header)
	for i, name := range columns {
		if strings.TrimSpace(name) == "" {
			columns[i] = "column_" + strconv.Itoa(i+1)
		}
	}
	if err := validateCSVColumns(columns); err != nil {
		return false, err
	}
	if opts.OnHeader != nil {
		opts.OnHeader(columns)
	}

	row := 0
	for {
		if row >= opts.MaxRows {
			return source.hasAdditionalCSVRecord(), nil
		}
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, errors.Wrapf(err, "read CSV row %d", row+1)
		}
		row++

		payload := map[string]any{}
		for i, value := range record {
			name := "column_" + strconv.Itoa(i+1)
			if i < len(columns) {
				name = columns[i]
			}
			if _, exists := payload[name]; exists {
				return false, errors.Errorf(
					"CSV row %d has duplicate effective column %q", row, name)
			}
			if _, isText := opts.TextColumns[name]; isText {
				payload[name] = value
			} else {
				payload[name] = CSVValue(value)
			}
		}

		encoded, err := json.Marshal(payload)
		if err != nil {
			return false, errors.Wrapf(err, "encode CSV row %d", row)
		}
		if err := emit(row, encoded); err != nil {
			return false, err
		}
	}
}

type physicalLineReader struct {
	reader      *bufio.Reader
	pending     []byte
	terminalErr error
}

func newPhysicalLineReader(source io.Reader) *physicalLineReader {
	return &physicalLineReader{reader: bufio.NewReader(source)}
}

func (r *physicalLineReader) Read(dst []byte) (int, error) {
	if len(r.pending) == 0 {
		if r.terminalErr != nil {
			err := r.terminalErr
			r.terminalErr = nil
			return 0, err
		}
		line, err := r.reader.ReadBytes('\n')
		r.pending = line
		r.terminalErr = err
		if len(r.pending) == 0 {
			terminalErr := r.terminalErr
			r.terminalErr = nil
			return 0, terminalErr
		}
	}
	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *physicalLineReader) hasAdditionalCSVRecord() bool {
	// encoding/csv ignores physically empty lines, but spaces and tabs form a
	// real one-field record. Scan only to the first byte other than CR/LF; do
	// not parse the out-of-budget record.
	for _, b := range r.pending {
		if b != '\r' && b != '\n' {
			return true
		}
	}
	if r.terminalErr != nil {
		return r.terminalErr != io.EOF
	}
	var one [1]byte
	for {
		n, err := r.reader.Read(one[:])
		if n > 0 && one[0] != '\r' && one[0] != '\n' {
			return true
		}
		if err != nil {
			return err != io.EOF
		}
	}
}

// hasNonWhitespace intentionally treats a read error as "more data" once the
// row budget is satisfied. The sample is already complete; failing because the
// out-of-budget suffix reset its connection would violate the same boundary as
// decoding a malformed suffix. Marking the result truncated is conservative.
func hasNonWhitespace(source io.Reader) bool {
	var one [1]byte
	for {
		n, err := source.Read(one[:])
		if n > 0 && !isJSONWhitespace(one[0]) {
			return true
		}
		if err != nil {
			return err != io.EOF
		}
	}
}

func isJSONWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func validateCSVColumns(columns []string) error {
	seen := make(map[string]int, len(columns))
	for i, name := range columns {
		if previous, exists := seen[name]; exists {
			return errors.Errorf(
				"CSV header columns %d and %d have duplicate effective name %q",
				previous+1, i+1, name)
		}
		seen[name] = i
	}
	return nil
}

// CSVValue types a CSV cell.
//
// CSV has no types, so a number that reads as a number becomes one and
// everything else stays a string. Without this every field would be a string
// and a JSON Schema declaring `"type": "number"` could never be satisfied.
func CSVValue(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	// json.Valid rejects leading-zero integers, hexadecimal floats, NaN, and
	// infinities. Keeping the accepted token as json.Number preserves every
	// digit rather than rounding through float64 before the row is marshaled.
	if isJSONNumber(trimmed) {
		return json.Number(trimmed)
	}
	switch strings.ToLower(trimmed) {
	case "true":
		return true
	case "false":
		return false
	}
	return value
}

func isJSONNumber(value string) bool {
	if value == "" || (value[0] != '-' && (value[0] < '0' || value[0] > '9')) {
		return false
	}
	return json.Valid([]byte(value))
}

// ReadNDJSONRows treats each non-empty physical line as exactly one JSON
// document. Concatenated documents and pretty-printed multi-line objects are
// rejected: accepting them would make a format named newline-delimited assign
// misleading record numbers during imports.
func ReadNDJSONRows(body io.Reader, opts ReadOptions, emit RowEmitter) (bool, error) {
	reader := bufio.NewReader(body)
	row := 0
	for {
		if row >= opts.MaxRows {
			// Once the sample is complete, detect only whether another nonblank
			// record begins. Do not allocate or parse that possibly huge line.
			return hasNonWhitespace(reader), nil
		}
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return false, errors.Wrapf(err, "read NDJSON record %d", row+1)
		}
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			if errors.Is(err, io.EOF) {
				return false, nil
			}
			continue
		}
		if !json.Valid([]byte(trimmed)) {
			return false, errors.Errorf("decode NDJSON record %d: expected exactly one JSON document on one line", row+1)
		}
		row++
		if err := emit(row, json.RawMessage(trimmed)); err != nil {
			return false, err
		}
		if errors.Is(err, io.EOF) {
			return false, nil
		}
	}
}

// ReadJSONArrayRows streams a single top-level JSON array.
//
// Deliberately token-driven rather than json.Unmarshal on the whole document:
// a dataset file that happens to be a JSON array is no smaller than one that is
// NDJSON, and reading it into memory would reintroduce exactly the problem the
// content-addressed blob store exists to avoid.
func ReadJSONArrayRows(body io.Reader, opts ReadOptions, emit RowEmitter) (bool, error) {
	decoder := json.NewDecoder(body)

	opening, err := decoder.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil // an empty file reads zero rows
		}
		return false, errors.Wrap(err, "read JSON array")
	}
	if delim, ok := opening.(json.Delim); !ok || delim != '[' {
		return false, errors.New("expected a top-level JSON array")
	}

	row := 0
	for decoder.More() {
		if row >= opts.MaxRows {
			// More has established that another array element exists without
			// decoding it. Return immediately even if that element is malformed
			// or enormous: it is outside the requested sample.
			return true, nil
		}
		var payload json.RawMessage
		if err := decoder.Decode(&payload); err != nil {
			return false, errors.Wrapf(err, "decode JSON element %d", row+1)
		}
		row++

		if err := emit(row, payload); err != nil {
			return false, err
		}
	}

	closing, err := decoder.Token()
	if err != nil {
		return false, errors.Wrap(err, "read JSON array closing delimiter")
	}
	if delim, ok := closing.(json.Delim); !ok || delim != ']' {
		return false, errors.New("expected JSON array closing delimiter")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return false, errors.New("unexpected data after JSON array")
		}
		return false, errors.Wrap(err, "read after JSON array")
	}
	return false, nil
}

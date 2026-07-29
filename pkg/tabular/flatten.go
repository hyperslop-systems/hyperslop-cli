package tabular

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/pkg/errors"
)

// Flattening rules, pinned so that two consumers agree:
//
//   - nested objects use dotted paths ("location.lat");
//   - arrays and any value that cannot be spread across columns are emitted as
//     compact JSON;
//   - a missing value is empty, not the string "null";
//   - an empty object is the two-character token "{}" rather than nothing, so
//     that a key which exists but holds no fields still produces a column.
//
// These rules moved here from pkg/server/handlers_export.go unchanged. The CSV
// export and the table projection now call the same walker, which is the
// mechanism — not the hope — that keeps their column sets identical.

// leafFunc receives one flattened leaf. path is the dotted path; value is the
// decoded JSON value (numbers arrive as json.Number).
type leafFunc func(path string, value any) error

// flatten decodes raw and walks it into leaves.
//
// A zero-length document produces no leaves at all, which is how an event with
// no payload contributes no data columns.
func flatten(prefix string, raw json.RawMessage, emit leafFunc) error {
	if len(raw) == 0 {
		return nil
	}

	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	// Keep 1e9 and 12345678901234567 exact rather than round-tripping through
	// float64, which would silently lose the last digits of a nanosecond
	// timestamp or a large identifier.
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return errors.Wrap(err, "decode payload")
	}
	return flattenValue(prefix, value, emit)
}

func flattenValue(prefix string, value any, emit leafFunc) error {
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 && prefix != "" {
			return emit(prefix, typed)
		}
		for key, child := range typed {
			childPrefix := key
			if prefix != "" {
				childPrefix = prefix + "." + key
			}
			if err := flattenValue(childPrefix, child, emit); err != nil {
				return err
			}
		}
		return nil

	case nil:
		// A JSON null is an absent value. At the top level it contributes no
		// column at all; nested, it contributes an empty one.
		if prefix == "" {
			return nil
		}
		return emit(prefix, nil)

	default:
		// Scalars, arrays, and anything else are leaves.
		return emit(prefix, typed)
	}
}

// FlattenStrings walks raw into cells suitable for a CSV row.
func FlattenStrings(prefix string, raw json.RawMessage, out map[string]string) error {
	return flatten(prefix, raw, func(path string, value any) error {
		cell, err := Cell(value)
		if err != nil {
			return errors.Wrapf(err, "encode value at %q", path)
		}
		out[path] = cell
		return nil
	})
}

// FlattenValues walks raw into typed cells suitable for a table row.
//
// Values keep their JSON types: a number stays a json.Number (which marshals
// back as a bare numeric literal), a bool stays a bool. This is the whole point
// of not re-deriving types in the browser — the type information is already
// here and is simply carried through.
func FlattenValues(prefix string, raw json.RawMessage, out map[string]any) error {
	return flatten(prefix, raw, func(path string, value any) error {
		out[path] = value
		return nil
	})
}

// Cell renders a flattened value as a CSV cell.
func Cell(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case bool:
		return strconv.FormatBool(typed), nil
	case json.Number:
		return typed.String(), nil
	case map[string]any:
		if len(typed) == 0 {
			return "{}", nil
		}
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return "", errors.Wrap(err, "encode value")
	}
	return string(encoded), nil
}

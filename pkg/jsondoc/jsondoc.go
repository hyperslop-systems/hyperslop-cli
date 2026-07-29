// Package jsondoc decodes arbitrary user/server JSON without converting
// numbers through float64. It is the single boundary for dynamic JSON that may
// later be re-encoded; typed wire structs can continue using encoding/json.
package jsondoc

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/pkg/errors"
)

// Decode decodes exactly one JSON document into dst. Numbers stored behind an
// interface are json.Number values, preserving their original decimal text.
func Decode(raw []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON documents")
		}
		return errors.Wrap(err, "trailing JSON data")
	}
	return nil
}

// Value decodes exactly one arbitrary JSON document losslessly.
func Value(raw []byte) (any, error) {
	var value any
	if err := Decode(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

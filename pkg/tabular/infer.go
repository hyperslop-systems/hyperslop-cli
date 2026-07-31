package tabular

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"
)

// PropType is the part of a JSON Schema property this package interprets.
//
// Deliberately two strings rather than a schema object: widening it means
// freezing more of JSON Schema's shape into the projection, and the only
// questions being asked are "is this a number" and "is this an instant".
type PropType struct {
	Type   string
	Format string
}

// SchemaProperties extracts the top-level "properties" map of a JSON Schema
// document, reduced to the fields inference uses.
//
// A document that is not an object, has no properties, or is absent yields nil,
// which means "nothing to go on" rather than an error: a dataset without a
// schema is a legal, ordinary dataset.
func SchemaProperties(raw json.RawMessage) map[string]PropType {
	if len(raw) == 0 {
		return nil
	}

	var doc struct {
		Properties map[string]struct {
			Type   json.RawMessage `json:"type"`
			Format string          `json:"format"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Properties) == 0 {
		return nil
	}

	props := make(map[string]PropType, len(doc.Properties))
	for name, prop := range doc.Properties {
		props[name] = PropType{Type: firstNonNullType(prop.Type), Format: prop.Format}
	}
	return props
}

// firstNonNullType resolves JSON Schema's two spellings of a type.
//
// `"type": "string"` and `"type": ["string", "null"]` both mean "a string, and
// possibly absent". Taking the first non-null entry gives the same answer for
// both, which is what a caller asking "is this column numeric" wants.
func firstNonNullType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single
	}

	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return ""
	}
	for _, candidate := range many {
		if candidate != "null" && candidate != "" {
			return candidate
		}
	}
	return ""
}

// temporalRatio is the share of non-missing values that must parse as an
// instant before a column is called temporal.
//
// It is a tolerance for dirty data, not a confidence measure: a column of dates
// with a few "unknown" entries should still get a time axis. The threshold and
// the minimum sample below are carried over from the reference artifact
// (pbui-gog.jsx:334), where they were arrived at empirically.
const temporalRatio = 0.8

// temporalMinimum is the smallest sample that may be called temporal. Below it
// the ratio is meaningless — one date out of one value is 100%.
const temporalMinimum = 3

// columnStats accumulates what inference needs from the sampled values.
type columnStats struct {
	numbers   int
	bools     int
	strings   int
	others    int
	temporals int

	distinct       map[string]struct{}
	distinctCapped bool
}

func newColumnStats() *columnStats {
	return &columnStats{distinct: map[string]struct{}{}}
}

// observe records one cell.
//
// A JSON null and an empty string are both treated as missing. Conflating them
// is a real loss of information, but the alternative — an empty CSV cell being
// a nominal category named "" that shows up in every legend — is worse, and CSV
// cannot distinguish the two anyway.
func (c *columnStats) observe(value any) {
	switch typed := value.(type) {
	case nil:
		return
	case string:
		if typed == "" {
			return
		}
		c.strings++
		if isInstant(typed) {
			c.temporals++
		}
		c.track("string\x00" + typed)
		return
	case json.Number:
		c.numbers++
		c.track("number\x00" + typed.String())
		return
	case float64:
		c.numbers++
		c.track("number\x00" + formatFloat(typed))
		return
	case bool:
		c.bools++
		if typed {
			c.track("bool\x00true")
		} else {
			c.track("bool\x00false")
		}
		return
	default:
		c.others++
		if encoded, err := json.Marshal(typed); err == nil {
			c.track(fmt.Sprintf("%T\x00%s", typed, encoded))
		}
	}
}

func (c *columnStats) track(key string) {
	if c.distinctCapped {
		return
	}
	if _, seen := c.distinct[key]; seen {
		return
	}
	if len(c.distinct) >= maxDistinctTracked {
		c.distinctCapped = true
		return
	}
	c.distinct[key] = struct{}{}
}

func (c *columnStats) observed() int {
	return c.numbers + c.bools + c.strings + c.others
}

// isInstant reports whether s is an RFC3339 timestamp.
//
// Strict on purpose. A regex that accepts 2026-13-45 would type a column of
// malformed dates as temporal and produce an axis nobody can read, and a
// heuristic that accepts bare integers as Unix epochs would turn a perfectly
// good scatter plot of counts into a date axis. A column of epoch seconds is
// quantitative until a human says otherwise.
func isInstant(s string) bool {
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return true
	}
	// The canonical datalab rendering is RFC3339 with fixed millisecond width,
	// which time.RFC3339 already accepts; this second attempt covers a
	// date-only value, which it does not.
	if _, err := time.Parse(datalab.TimeFormat, s); err == nil {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// inferType decides a column's encoding type and records how.
//
// Precedence is schema, then observation, then default. It is the whole of DR-2
// in fifteen lines: the producer may already have written the answer down, and
// a sniffer that overrules them does so silently.
func inferType(name string, prop *PropType, stats *columnStats) (FieldType, TypeSource) {
	if fixed, ok := envelopeTypes[name]; ok {
		return fixed, SourceEnvelope
	}

	if prop != nil {
		if t, ok := schemaType(*prop); ok {
			return t, SourceSchema
		}
	}

	if stats == nil || stats.observed() == 0 {
		return TypeNominal, SourceDefault
	}

	observed := stats.observed()
	switch {
	case stats.numbers == observed:
		return TypeQuantitative, SourceValues
	case stats.bools == observed:
		return TypeNominal, SourceValues
	case observed >= temporalMinimum && float64(stats.temporals)/float64(observed) > temporalRatio:
		return TypeTemporal, SourceValues
	default:
		return TypeNominal, SourceValues
	}
}

// schemaType maps a JSON Schema type onto an encoding type. It reports false
// when the schema says nothing usable, so the caller falls through to
// observation rather than defaulting to nominal on an unrecognized keyword.
func schemaType(prop PropType) (FieldType, bool) {
	switch prop.Type {
	case "number", "integer":
		return TypeQuantitative, true
	case "boolean":
		return TypeNominal, true
	case "string":
		switch prop.Format {
		case "date-time", "date", "time":
			return TypeTemporal, true
		}
		return TypeNominal, true
	default:
		// "object", "array", an absent type, or a keyword this package does not
		// interpret. Fall through.
		return "", false
	}
}

func formatFloat(f float64) string {
	encoded, err := json.Marshal(f)
	if err != nil {
		return ""
	}
	return string(encoded)
}

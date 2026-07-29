package datadrop

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/pkg/errors"
)

var retentionRE = regexp.MustCompile(`^[1-9][0-9]*[smhdwy]$`)

// Mode is how strictly a stream's payloads are validated.
//
// The upstream OpenDrop design names four modes (open, suggest, warn, strict);
// v0.1 stores two. The mapping, also recorded in the README:
//
//	(no schema registered) → upstream "open"    — accept any valid JSON
//	ModePermissive         → upstream "warn"    — accept, attach warnings
//	ModeStrict             → upstream "strict"  — reject with 422
//	(not implemented)      → upstream "suggest" — schema inference
type Mode string

const (
	ModeStrict     Mode = "strict"
	ModePermissive Mode = "permissive"
)

// ParseMode validates a mode string.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeStrict:
		return ModeStrict, nil
	case ModePermissive:
		return ModePermissive, nil
	case "":
		return ModeStrict, nil
	default:
		return "", errors.Errorf("invalid schema mode %q: expected %q or %q", s, ModeStrict, ModePermissive)
	}
}

// Schema is one immutable version of a stream's payload contract.
//
// Versions are never rewritten: registering a schema always creates
// MAX(version)+1, and the highest version is the active one. v0.1 does not
// implement multi-version acceptance or compatibility classification.
type Schema struct {
	Drop      string    `json:"drop"`
	Stream    string    `json:"stream"`
	Version   int       `json:"version"`
	Mode      Mode      `json:"mode"`
	CreatedAt time.Time `json:"created_at"`

	// Spec is the JSON Schema document exactly as submitted. It is stored and
	// returned byte-for-byte so that x-drop-* extension keywords — which the
	// validator ignores — survive a round trip.
	Spec json.RawMessage `json:"spec"`
}

// PutSchemaResult is the PUT /v1/drops/{name}/schemas/{stream} response.
type PutSchemaResult struct {
	Drop    string `json:"drop"`
	Stream  string `json:"stream"`
	Version int    `json:"version"`
	Mode    Mode   `json:"mode"`
}

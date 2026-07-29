// Package datadrop holds the domain types shared by the store, the HTTP
// server, and the client: drops, event envelopes, schemas, queries, and audit
// records.
//
// Keeping these in one leaf package (rather than in pkg/store) means the
// client can speak the same vocabulary as the server without importing the
// persistence layer.
package datadrop

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	// SpecVersion is the CloudEvents spec version we emit. See DR-4.
	SpecVersion = "1.0"

	// DefaultStream is the stream every drop has without being asked.
	// Principle §6.1: the simple path must remain simple.
	DefaultStream = "events"

	// DefaultType is the CloudEvents `type` assigned when a producer does not
	// supply one.
	DefaultType = "io.datadrop.event"
)

// Envelope is one event: the CloudEvents-compatible metadata wrapper plus the
// producer's payload.
//
// Time and ReceivedAt are deliberately distinct (guide §4.3): Time is when the
// producer observed the data, ReceivedAt is when this server durably stored
// it. An edge device that buffers while offline reports an old Time and a
// recent ReceivedAt, and both matter.
type Envelope struct {
	SpecVersion string `json:"specversion"`

	// ID is unique across the instance. Clients may supply it; resending the
	// same ID is an idempotent no-op rather than a duplicate (guide §9.4).
	ID string `json:"id"`

	Source  string `json:"source,omitempty"`
	Type    string `json:"type,omitempty"`
	Subject string `json:"subject,omitempty"`

	// Time is the producer's observation time. Defaults to ReceivedAt.
	Time time.Time `json:"time"`

	// Data is the payload, stored as submitted (modulo whitespace).
	Data json.RawMessage `json:"data"`

	// Meta carries server annotations: schema version, validation warnings.
	Meta json.RawMessage `json:"meta,omitempty"`

	// The remaining fields are server-assigned; client values are ignored.
	Drop       string    `json:"drop,omitempty"`
	Stream     string    `json:"stream,omitempty"`
	Seq        int64     `json:"seq,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

// EventMeta is the structured shape of Envelope.Meta that this server writes.
// Producers may submit arbitrary JSON in `meta`; anything they send is merged
// under the "producer" key so server annotations cannot be spoofed.
type EventMeta struct {
	SchemaVersion int             `json:"schema_version,omitempty"`
	Warnings      []Violation     `json:"warnings,omitempty"`
	Producer      json.RawMessage `json:"producer,omitempty"`
}

// Violation is one schema validation failure, located by JSON Pointer.
type Violation struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// AppendResult is what the ingest endpoint returns.
type AppendResult struct {
	ID         string      `json:"id"`
	Drop       string      `json:"drop"`
	Stream     string      `json:"stream"`
	Seq        int64       `json:"seq"`
	ReceivedAt time.Time   `json:"received_at"`
	Warnings   []Violation `json:"warnings,omitempty"`

	// Duplicate is true when this ID had already been appended, so the caller
	// is seeing the original event rather than a new one. Not serialized: the
	// HTTP status (200 vs 201) carries it on the wire.
	Duplicate bool `json:"-"`
}

// nameRE constrains drop and stream names to something safe in a URL path, a
// filename, and a CSV column header.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// ValidateName checks a drop or stream name. The `kind` argument only shapes
// the error message.
func ValidateName(kind, name string) error {
	if !nameRE.MatchString(name) {
		return errors.Errorf(
			"invalid %s name %q: must match %s", kind, name, nameRE.String())
	}
	return nil
}

// NormalizeStream applies the default-stream rule.
func NormalizeStream(stream string) string {
	if s := strings.TrimSpace(stream); s != "" {
		return s
	}
	return DefaultStream
}

// TimeFormat is the canonical timestamp representation used everywhere in
// datadrop: RFC3339, UTC, fixed-width millisecond precision.
//
// The fixed width matters. time.RFC3339Nano strips trailing zeros, which would
// make "…:05.100Z" and "…:05.1Z" different strings that compare incorrectly in
// a lexicographic range query.
//
// It lives in this package rather than in pkg/store because three layers need
// it — the database, the CSV export, and the table projection — and a timestamp
// format that two of them agree on is a bug waiting for the third.
const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

// FormatTime renders t in the canonical representation.
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeFormat)
}

// ParseTime parses a timestamp written by FormatTime.
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(TimeFormat, s)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "parse timestamp %q", s)
	}
	return t.UTC(), nil
}

// StreamInfo describes one stream within a drop.
//
// Streams are created implicitly by appending to them, so this is derived from
// stream_heads rather than from a registry: there is no moment at which a
// stream is declared.
type StreamInfo struct {
	Stream string `json:"stream"`

	// Sequence is the high-water mark of allocated sequences.
	Sequence int64 `json:"sequence"`

	// EventCount is how many events are currently stored. It can be lower than
	// Sequence — a retention sweep lowers the count and must never lower the
	// head — and the divergence is worth showing rather than hiding.
	EventCount int64 `json:"event_count"`

	// LastReceivedAt is the ingest time of the newest stored event, absent when
	// the stream holds none.
	LastReceivedAt *time.Time `json:"last_received_at,omitempty"`
}

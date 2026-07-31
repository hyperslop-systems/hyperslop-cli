package datalab

import (
	"time"

	"github.com/pkg/errors"
)

// Order is the sort direction of a query result.
type Order string

const (
	OrderAsc  Order = "asc"
	OrderDesc Order = "desc"
)

// ParseOrder validates a sort direction. Empty defaults to descending, which
// makes the bare query a "latest N".
func ParseOrder(s string) (Order, error) {
	switch Order(s) {
	case OrderAsc:
		return OrderAsc, nil
	case OrderDesc, "":
		return OrderDesc, nil
	default:
		return "", errors.Errorf("invalid order %q: expected %q or %q", s, OrderAsc, OrderDesc)
	}
}

// TimeField selects which of an event's two timestamps a range filter applies
// to. It is interpolated into SQL, so it must never be anything but one of
// these constants — see ParseTimeField.
type TimeField string

const (
	// TimeFieldTime filters on the producer's observation time.
	TimeFieldTime TimeField = "time"
	// TimeFieldReceivedAt filters on the server's ingest time.
	TimeFieldReceivedAt TimeField = "received_at"
)

// ParseTimeField is the allowlist that keeps EventQuery.TimeField safe to
// interpolate into a SQL statement. Every other query parameter is bound.
func ParseTimeField(s string) (TimeField, error) {
	switch TimeField(s) {
	case TimeFieldTime, "":
		return TimeFieldTime, nil
	case TimeFieldReceivedAt:
		return TimeFieldReceivedAt, nil
	default:
		return "", errors.Errorf("invalid time field %q: expected %q or %q",
			s, TimeFieldTime, TimeFieldReceivedAt)
	}
}

// Query limits. The server clamps regardless of what a client asks for.
const (
	DefaultLimit = 50
	MaxLimit     = 1000
)

// EventQuery selects a window of a stream.
//
// From/To compose with the directional sequence bounds. After is an ascending
// lower bound; Before is a descending upper bound.
type EventQuery struct {
	Drop   string
	Stream string

	// After returns only events with a sequence strictly greater than this.
	// Zero disables the filter. This is the cursor form used by tail/SSE.
	After int64
	// Before returns only events with a sequence strictly less than this.
	// It is valid only with descending order.
	Before int64

	// From is an inclusive lower bound, To an exclusive upper bound, both on
	// TimeField. Zero values disable the respective bound.
	From      time.Time
	To        time.Time
	TimeField TimeField

	Limit int
	Order Order

	// LimitCap bounds Limit after the default is applied. Zero selects
	// MaxLimit.
	//
	// It exists because a page of envelopes and a table projection are
	// different products with different failure modes: a thousand envelopes is
	// a generous JSON page, and a thousand points is a sparse chart. The cap is
	// set by the handler, never parsed from the URL, so a client cannot raise
	// its own ceiling.
	LimitCap int
}

// Normalize applies defaults and clamps, and validates the fields that are not
// bound parameters. Call it before handing a query to the store.
func (q *EventQuery) Normalize() error {
	if err := ValidateName("drop", q.Drop); err != nil {
		return err
	}

	q.Stream = NormalizeStream(q.Stream)
	if err := ValidateName("stream", q.Stream); err != nil {
		return err
	}

	if q.TimeField == "" {
		q.TimeField = TimeFieldTime
	}
	if _, err := ParseTimeField(string(q.TimeField)); err != nil {
		return err
	}

	if q.Order == "" {
		q.Order = OrderDesc
	}
	if _, err := ParseOrder(string(q.Order)); err != nil {
		return err
	}
	if q.After > 0 && q.Before > 0 {
		return errors.New("event query cannot set both after and before")
	}
	if q.After > 0 && q.Order != OrderAsc {
		return errors.New("event query after requires ascending order")
	}
	if q.Before > 0 && q.Order != OrderDesc {
		return errors.New("event query before requires descending order")
	}

	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	ceiling := q.LimitCap
	if ceiling <= 0 {
		ceiling = MaxLimit
	}
	if q.Limit > ceiling {
		q.Limit = ceiling
	}

	if !q.From.IsZero() && !q.To.IsZero() && !q.To.After(q.From) {
		return errors.Errorf("invalid time range: --to (%s) must be after --from (%s)",
			q.To.Format(time.RFC3339), q.From.Format(time.RFC3339))
	}
	return nil
}

// QueryResult is the GET /v1/drops/{name}/events response.
type QueryResult struct {
	Drop   string     `json:"drop"`
	Stream string     `json:"stream"`
	Count  int        `json:"count"`
	Events []Envelope `json:"events"`

	// Exactly one directional cursor is present for a non-empty page.
	NextAfter  int64 `json:"next_after,omitempty"`
	NextBefore int64 `json:"next_before,omitempty"`
}

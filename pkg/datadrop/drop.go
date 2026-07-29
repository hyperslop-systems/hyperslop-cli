package datadrop

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// Drop is a named destination: the unit of naming, sharing, and export.
type Drop struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`

	// Retention is a duration string such as "90d". It is stored and reported
	// but NOT enforced in v0.1 — nothing deletes events. Documented in the
	// README so an operator is not misled into thinking it is a live policy.
	Retention string `json:"retention,omitempty"`

	// PublicRead exempts read endpoints on this drop from bearer auth.
	PublicRead bool `json:"public_read"`

	// OwnerID is the user who owns this drop, or empty for an unowned one.
	//
	// Every drop created before DATADROP-5 is unowned, and stays that way until
	// someone claims it: assigning an owner at migration time would be a silent
	// grant of access to data (DR-25). Unowned is not unprotected — only the
	// root principal, or PublicRead, opens such a drop.
	OwnerID string `json:"owner_id,omitempty"`

	// YourRole is the calling principal's effective role, filled in by the HTTP
	// layer and never stored. It exists so the UI can grey out an action it
	// knows will 403 rather than offering it and failing — the same principle
	// as a disabled menu entry showing the rule instead of hiding it.
	YourRole string `json:"your_role,omitempty"`
}

// DropStats is the inspection view of a drop: metadata plus cheap counters.
type DropStats struct {
	Drop
	EventCount int64      `json:"event_count"`
	LastSeq    int64      `json:"last_seq"`
	LastEvent  *time.Time `json:"last_event,omitempty"`
	Streams    []string   `json:"streams"`
}

// CreateDropRequest is the POST /v1/drops body.
type CreateDropRequest struct {
	Name       string `json:"name"`
	Retention  string `json:"retention,omitempty"`
	PublicRead bool   `json:"public_read,omitempty"`
}

// ValidateRetention checks the retention syntax without committing to
// enforcement semantics. Accepted: a positive integer followed by one of
// s, m, h, d, w, y. Empty means "keep forever".
func ValidateRetention(retention string) error {
	retention = strings.TrimSpace(retention)
	if retention == "" {
		return nil
	}
	if !retentionRE.MatchString(retention) {
		return errors.Errorf(
			"invalid retention %q: expected a positive count followed by s, m, h, d, w, or y (e.g. \"90d\")",
			retention)
	}
	return nil
}

// AuditRecord is one entry in the append-only write log.
type AuditRecord struct {
	ID     int64           `json:"id,omitempty"`
	TS     time.Time       `json:"ts"`
	Actor  string          `json:"actor,omitempty"`
	Action string          `json:"action"`
	Drop   string          `json:"drop,omitempty"`
	Detail json.RawMessage `json:"detail,omitempty"`
}

// Audit actions. These strings end up in the audit_log table, so they are part
// of the operator-visible contract.
const (
	ActionDropCreate  = "drop.create"
	ActionEventAppend = "event.append"
	ActionSchemaPut   = "schema.put"
)

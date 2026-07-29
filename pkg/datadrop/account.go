package datadrop

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pkg/errors"
)

// User is a person, as datadrop knows them.
//
// Identity itself lives in the OIDC provider: it owns the password, the MFA
// factors, the email verification and the registration form. What is here is
// the local record that ownership, membership, tokens and audit rows point at
// — plus a cache of the two claims the UI needs to show something (guide §5.4).
type User struct {
	ID string `json:"id"`
	// Issuer and Subject together identify the person at the provider. Subject
	// alone is not unique across issuers.
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
	// Email and Name are refreshed on every sign-in and are NOT authoritative.
	// Nothing may look a user up by email.
	Email      string    `json:"email,omitempty"`
	Name       string    `json:"name,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	// Disabled locks an account out of datadrop without touching the identity
	// provider — because "this account is uploading garbage" is a datadrop
	// problem, not an identity problem.
	Disabled bool `json:"disabled,omitempty"`
}

// DisplayName is what the UI shows. It never returns empty: the whole interface
// treats a user's name as a display string, and a blank chip is a bug report.
func (u User) DisplayName() string {
	if name := strings.TrimSpace(u.Name); name != "" {
		return name
	}
	if email := strings.TrimSpace(u.Email); email != "" {
		if local, _, found := strings.Cut(email, "@"); found && local != "" {
			return local
		}
		return email
	}
	return "(unnamed)"
}

// Session is a browser's credential: a row referenced by an opaque cookie.
//
// The cookie value itself is never stored — ID is its SHA-256 — so a dump of
// the database does not hand over live sessions.
type Session struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	UserAgent  string    `json:"user_agent,omitempty"`
	IP         string    `json:"ip,omitempty"`
	// IDToken is kept solely to pass as id_token_hint to the provider's
	// end-session endpoint. It carries PII and is never serialised to a client,
	// which is why it has no JSON tag pointing anywhere useful.
	IDToken string `json:"-"`
}

// Expired reports whether the session has passed its absolute deadline or been
// idle too long.
//
// Both checks live here rather than only in the sweeper. A sweeper that is also
// the enforcement mechanism means a paused process is an authorization bypass.
//
// A non-positive idle disables the idle check — which is a fail-open default,
// so the server config supplies a real one rather than letting a zero value
// mean "no limit" by accident (see server.Config.OIDC.SessionIdle). The
// absolute deadline has no such escape and is always enforced.
func (s Session) Expired(now time.Time, idle time.Duration) bool {
	if !now.Before(s.ExpiresAt) {
		return true
	}
	return idle > 0 && now.Sub(s.LastSeenAt) > idle
}

// APIToken is a machine credential belonging to a user.
//
// The secret is not a field. It exists for the length of one HTTP response and
// is never recoverable afterwards (guide §9.5); what is stored is its hash,
// which lives in the store layer and is deliberately absent from this type so
// it cannot be serialised by accident.
type APIToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Scopes     []Scope    `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// Live reports whether the token would authenticate right now.
func (t APIToken) Live(now time.Time) bool {
	if t.RevokedAt != nil {
		return false
	}
	return t.ExpiresAt == nil || now.Before(*t.ExpiresAt)
}

// Member is one row of a drop's access list.
type Member struct {
	Drop    string    `json:"drop"`
	UserID  string    `json:"user_id"`
	Role    Role      `json:"role"`
	AddedAt time.Time `json:"added_at"`
	AddedBy string    `json:"added_by,omitempty"`
	// User is filled in by the listing endpoints so the UI can render a name
	// without a second round trip. It is never written.
	User *User `json:"user,omitempty"`
}

// AuthFlow is a sign-in in progress: one authorization redirect's state.
type AuthFlow struct {
	State     string
	Nonce     string
	Verifier  string
	ReturnTo  string
	CreatedAt time.Time
}

// CreateTokenRequest is the POST /v1/me/tokens body.
type CreateTokenRequest struct {
	Name   string  `json:"name"`
	Scopes []Scope `json:"scopes"`
	// ExpiresIn is a retention-style duration such as "90d". Empty means the
	// token does not expire, which is a choice the UI makes the user make
	// rather than a default it applies quietly.
	ExpiresIn string `json:"expires_in,omitempty"`
}

// CreateTokenResponse is the ONLY response in the entire API that carries a
// token secret. Everything else returns APIToken, which structurally cannot.
type CreateTokenResponse struct {
	APIToken
	Token string `json:"token"`
}

// SetMemberRequest is the PUT /v1/drops/{name}/members/{userId} body.
type SetMemberRequest struct {
	Role Role `json:"role"`
}

// Audit actions for account operations. These strings are part of the
// operator-visible contract, like the ones in drop.go and dataset.go.
const (
	ActionUserCreate    = "user.create"
	ActionSessionCreate = "session.create"
	ActionSessionDelete = "session.delete"
	ActionTokenCreate   = "token.create"
	ActionTokenRevoke   = "token.revoke"
	ActionDropClaim     = "drop.claim"
	ActionMemberSet     = "member.set"
	ActionMemberRemove  = "member.remove"
)

// ValidateTokenName checks the human-facing label on a token.
//
// Deliberately not ValidateName: a token name is prose a person types to
// recognise it later ("ci ingest, staging"), not an identifier that appears in
// a URL, so the drop-name grammar would be gratuitously strict.
func ValidateTokenName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("a token name is required: it is how you will recognise it later")
	}
	characterCount := utf8.RuneCountInString(trimmed)
	if characterCount > 100 {
		return errors.Errorf("token name is %d characters, maximum 100", characterCount)
	}
	if strings.ContainsAny(trimmed, "\n\r\t") {
		return errors.New("a token name must be a single line")
	}
	return nil
}

// ParseExpiresIn turns "90d" into an absolute deadline, or nil for "never".
//
// It reuses the retention grammar (ValidateRetention) so that there is one
// duration syntax in the product rather than two that differ in an edge case
// nobody remembers.
func ParseExpiresIn(raw string, now time.Time) (*time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	if err := ValidateRetention(trimmed); err != nil {
		return nil, errors.Wrap(err, "invalid expires_in")
	}
	d, err := retentionDuration(trimmed)
	if err != nil {
		return nil, err
	}
	deadline := now.Add(d)
	return &deadline, nil
}

// retentionDuration converts a validated retention string to a duration.
//
// Days, weeks and years are fixed multiples of 24 hours rather than calendar
// arithmetic. For a credential deadline that is the right call: "90d" must mean
// the same interval everywhere, and a token that expires an hour early or late
// across a daylight-saving boundary is a support ticket with no explanation.
func retentionDuration(retention string) (time.Duration, error) {
	if err := ValidateRetention(retention); err != nil {
		return 0, err
	}
	count, err := strconv.ParseInt(retention[:len(retention)-1], 10, 64)
	if err != nil {
		return 0, errors.Wrapf(err, "invalid duration %q", retention)
	}
	unit := map[byte]time.Duration{
		's': time.Second,
		'm': time.Minute,
		'h': time.Hour,
		'd': 24 * time.Hour,
		'w': 7 * 24 * time.Hour,
		'y': 365 * 24 * time.Hour,
	}[retention[len(retention)-1]]
	if unit == 0 {
		return 0, errors.Errorf("invalid duration unit in %q", retention)
	}
	if count > math.MaxInt64/int64(unit) {
		return 0, errors.Errorf("duration %q exceeds the maximum representable interval", retention)
	}
	return time.Duration(count) * unit, nil
}

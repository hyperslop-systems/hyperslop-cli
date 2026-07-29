package datadrop

import (
	"sort"
	"strings"

	"github.com/pkg/errors"
)

// Scope is what an API token is permitted to do.
//
// NOT to be confused with an OAuth scope. Those (openid, email, profile) are
// what we ask Zitadel for; these are what a credential may do here. The two
// never mix and never appear in the same field. See the user-accounts guide §3.
//
// Scope lives in the datadrop wire-types package because the wire types embed it
// (a DeviceAuthorization carries requested scopes; an APIToken carries its
// scopes; a Member carries a Role). Keeping the model here makes pkg/datadrop a
// pure leaf with no dependency on any auth package, which is what lets the
// customer CLI (hyperslop-cli) own the wire contract without importing the
// proprietary server.
type Scope string

const (
	ScopeDropsRead     Scope = "drops:read"
	ScopeDropsWrite    Scope = "drops:write"
	ScopeDatasetsWrite Scope = "datasets:write"
	ScopeAdmin         Scope = "admin"
)

// AllScopes is every scope, in the order a UI should offer them: least
// dangerous first.
//
// Four, and resist adding a fifth without a concrete need. Every scope is
// something a person must understand while creating a token, and a form with
// fifteen checkboxes gets "select all" clicked every time — at which point
// scopes have made things worse rather than better.
var AllScopes = []Scope{ScopeDropsRead, ScopeDropsWrite, ScopeDatasetsWrite, ScopeAdmin}

// ScopeSet is a set of scopes.
type ScopeSet map[Scope]struct{}

// NewScopeSet builds a set from scopes, ignoring duplicates.
func NewScopeSet(scopes ...Scope) ScopeSet {
	set := make(ScopeSet, len(scopes))
	for _, s := range scopes {
		set[s] = struct{}{}
	}
	return set
}

// FullScopeSet is everything. Sessions carry it: a human at a browser acts with
// their full rights, and a per-session scope restriction is a feature nobody
// asked for.
func FullScopeSet() ScopeSet { return NewScopeSet(AllScopes...) }

// Has reports membership.
//
// ScopeAdmin implies every other scope. Without that, an admin token would need
// all four boxes ticked to do anything, and the box labelled "admin" would be
// the one that does the least — which is a UI that teaches the wrong model.
func (s ScopeSet) Has(scope Scope) bool {
	if _, ok := s[ScopeAdmin]; ok {
		return true
	}
	_, ok := s[scope]
	return ok
}

// Slice returns the scopes in AllScopes order, for stable output.
func (s ScopeSet) Slice() []Scope {
	out := make([]Scope, 0, len(s))
	for _, known := range AllScopes {
		if _, ok := s[known]; ok {
			out = append(out, known)
		}
	}
	// Anything not in AllScopes — a scope written by a newer version and read
	// by an older one — is preserved rather than dropped, so a downgrade does
	// not silently widen a token by forgetting what it was limited to.
	extra := make([]string, 0)
	for scope := range s {
		if !isKnownScope(scope) {
			extra = append(extra, string(scope))
		}
	}
	sort.Strings(extra)
	for _, e := range extra {
		out = append(out, Scope(e))
	}
	return out
}

// String renders the set as it is stored: space-separated, in a stable order.
func (s ScopeSet) String() string {
	parts := make([]string, 0, len(s))
	for _, scope := range s.Slice() {
		parts = append(parts, string(scope))
	}
	return strings.Join(parts, " ")
}

// ParseScopes reads the stored representation.
//
// Unknown scopes are preserved rather than rejected (see Slice), but an empty
// set is an error: a token that can do nothing is far more likely to be a bug
// in whatever wrote it than a deliberate choice, and failing loudly at parse
// time beats a credential that mysteriously 403s everywhere.
func ParseScopes(raw string) (ScopeSet, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil, errors.New("auth: a credential must carry at least one scope")
	}
	set := make(ScopeSet, len(fields))
	for _, f := range fields {
		set[Scope(f)] = struct{}{}
	}
	return set, nil
}

// ValidateScopes rejects anything not in AllScopes. Used when minting, where a
// typo must not become a token that silently permits nothing.
func ValidateScopes(scopes []Scope) error {
	if len(scopes) == 0 {
		return errors.New("auth: at least one scope is required")
	}
	for _, s := range scopes {
		if !isKnownScope(s) {
			return errors.Errorf("auth: unknown scope %q (known: %s)", s, joinScopes(AllScopes))
		}
	}
	return nil
}

// ValidateDeviceScopes permits only operational data scopes. Device tokens may
// not mint credentials or administer users/members, so the broad admin scope is
// prohibited even though an interactive user token may carry it.
func ValidateDeviceScopes(scopes []Scope) error {
	if err := ValidateScopes(scopes); err != nil {
		return err
	}
	for _, scope := range scopes {
		if scope == ScopeAdmin {
			return errors.New("auth: device credentials may not carry admin")
		}
	}
	return nil
}

func isKnownScope(s Scope) bool {
	for _, known := range AllScopes {
		if known == s {
			return true
		}
	}
	return false
}

func joinScopes(scopes []Scope) string {
	parts := make([]string, len(scopes))
	for i, s := range scopes {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}

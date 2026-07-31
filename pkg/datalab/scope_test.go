package datalab

import "testing"

func TestScopeAdminImplies(t *testing.T) {
	admin := NewScopeSet(ScopeAdmin)
	for _, scope := range AllScopes {
		if !admin.Has(scope) {
			t.Errorf("admin does not imply %s; a token labelled admin would do the least", scope)
		}
	}
}

func TestScopeRoundTrip(t *testing.T) {
	set := NewScopeSet(ScopeDatasetsWrite, ScopeDropsRead)
	// Stable order, weakest first, regardless of insertion order.
	if got := set.String(); got != "drops:read datasets:write" {
		t.Errorf("String() = %q", got)
	}
	parsed, err := ParseScopes(set.String())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.String() != set.String() {
		t.Errorf("round trip: %q != %q", parsed.String(), set.String())
	}
}

func TestParseScopesPreservesUnknown(t *testing.T) {
	// A scope written by a newer version and read by an older one must not be
	// silently dropped: dropping it would widen the token by forgetting what it
	// was limited to.
	parsed, err := ParseScopes("drops:read future:scope")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !parsed.Has("future:scope") {
		t.Error("an unknown scope was dropped on parse")
	}
	if parsed.Has(ScopeDropsWrite) {
		t.Error("an unknown scope was treated as a grant")
	}
}

func TestParseScopesRejectsEmpty(t *testing.T) {
	if _, err := ParseScopes("   "); err == nil {
		t.Error("an empty scope string parsed; a credential that can do nothing is a bug in whatever wrote it")
	}
}

func TestValidateScopesRejectsTypos(t *testing.T) {
	if err := ValidateScopes([]Scope{"drops:wrote"}); err == nil {
		t.Error("a typo'd scope was accepted at mint time")
	}
	if err := ValidateScopes(nil); err == nil {
		t.Error("minting with no scopes was accepted")
	}
	if err := ValidateScopes(AllScopes); err != nil {
		t.Errorf("every known scope should validate: %v", err)
	}
}

func TestValidateDeviceScopesRejectsAdmin(t *testing.T) {
	if err := ValidateDeviceScopes([]Scope{ScopeAdmin}); err == nil {
		t.Error("a device credential was allowed to carry admin")
	}
	if err := ValidateDeviceScopes([]Scope{
		ScopeDropsRead,
		ScopeDropsWrite,
		ScopeWorkbenchesRead,
		ScopeWorkbenchesWrite,
	}); err != nil {
		t.Errorf("operational scopes should validate for a device credential: %v", err)
	}
}

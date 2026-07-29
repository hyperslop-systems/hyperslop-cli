package datadrop

import "testing"

func TestRoleOrdering(t *testing.T) {
	cases := []struct {
		have, required Role
		want           bool
	}{
		{RoleAdmin, RoleReader, true},
		{RoleAdmin, RoleAdmin, true},
		{RoleWriter, RoleReader, true},
		{RoleWriter, RoleAdmin, false},
		{RoleReader, RoleWriter, false},
		{RoleNone, RoleReader, false},
		// RoleNone is the absence of a role, so it satisfies nothing — not even
		// itself. Without the rank > 0 guard, AtLeast(RoleNone) would be true
		// for a stranger and every read would open.
		{RoleNone, RoleNone, false},
		{RoleAdmin, Role("owner"), false},
		{Role("owner"), RoleReader, false},
		{Role("owner"), Role("owner"), false},
	}
	for _, c := range cases {
		if got := c.have.AtLeast(c.required); got != c.want {
			t.Errorf("Role(%q).AtLeast(%q) = %v, want %v", c.have, c.required, got, c.want)
		}
	}
}

func TestParseRole(t *testing.T) {
	for _, r := range AssignableRoles {
		got, err := ParseRole(string(r))
		if err != nil || got != r {
			t.Errorf("ParseRole(%q) = %q, %v; want %q, nil", r, got, err, r)
		}
	}
	if _, err := ParseRole("overlord"); err == nil {
		t.Error("an unknown role was accepted")
	}
}

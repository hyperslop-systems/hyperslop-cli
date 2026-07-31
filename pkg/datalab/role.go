package datalab

import "github.com/pkg/errors"

// Role is what a user may do to one drop.
//
// The unit of sharing is the drop, because drop.go has called it "the unit of
// naming, sharing, and export" since v0.1: there is no per-dataset or
// per-stream role.
//
// Like Scope, Role lives in the datalab wire-types package because the wire
// types embed it (a Member carries a Role; a SetMemberRequest carries one). The
// per-request authorization decision that turns a Role and a Scope into an
// allow/deny (EffectiveRole, Authorize, DropACL) is server-only and stays with
// the proprietary backend.
type Role string

const (
	// RoleNone is the absence of a role. It is not stored; it is what an
	// effective-role computation returns when a principal has no relationship
	// to a drop.
	RoleNone   Role = ""
	RoleReader Role = "reader"
	RoleWriter Role = "writer"
	RoleAdmin  Role = "admin"
)

// AssignableRoles is what may appear in drop_members, weakest first.
var AssignableRoles = []Role{RoleReader, RoleWriter, RoleAdmin}

// rank orders roles so that "at least this role" is a comparison.
func (r Role) rank() int {
	switch r {
	case RoleReader:
		return 1
	case RoleWriter:
		return 2
	case RoleAdmin:
		return 3
	case RoleNone:
		return 0
	default:
		// An unknown role from a newer version: treated as no role at all,
		// never as a grant.
		return 0
	}
}

// AtLeast reports whether r is required or stronger. Both roles must be known:
// treating an unknown required role as rank zero would let every valid role
// satisfy a requirement introduced by a newer server, which is fail-open.
func (r Role) AtLeast(required Role) bool {
	have, need := r.rank(), required.rank()
	return have > 0 && need > 0 && have >= need
}

// Valid reports whether r may be stored in drop_members.
func (r Role) Valid() bool {
	for _, known := range AssignableRoles {
		if known == r {
			return true
		}
	}
	return false
}

// ParseRole validates a role coming from a request body or a database column.
func ParseRole(raw string) (Role, error) {
	r := Role(raw)
	if !r.Valid() {
		return RoleNone, errors.Errorf(
			"auth: unknown role %q (known: reader, writer, admin)", raw)
	}
	return r, nil
}

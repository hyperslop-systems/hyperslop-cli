package client

import (
	"context"
	"net/http"
)

// Me is the GET /v1/me response: what the configured credential resolves to.
//
// It mirrors server.MeResponse. Duplicated rather than imported so the client
// does not depend on the server package for one struct — the same reason the
// rest of pkg/client speaks pkg/datadrop and not pkg/store.
type Me struct {
	AuthMode      string   `json:"auth_mode"`
	Authenticated bool     `json:"authenticated"`
	Kind          string   `json:"kind"`
	Scopes        []string `json:"scopes"`
	TokenID       string   `json:"token_id"`

	User     *MeUser     `json:"user"`
	Provider *MeProvider `json:"provider"`
}

// MeUser is the account behind a session or a user-scoped token.
type MeUser struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// MeProvider names the identity provider in OIDC mode.
type MeProvider struct {
	Issuer string `json:"issuer"`
}

// Whoami reports the identity, kind and scopes of the configured credential.
//
// It answers the four questions a 403 cannot distinguish between: is the token
// valid, whose is it, what may it do, and is this server even running with user
// accounts.
//
// Unlike the previous hand-rolled request in pkg/cli, this goes through the
// client's own do(), so an unreachable server or a rejected credential arrives
// as the same *APIError every other verb produces — which is what lets the exit
// code mapping treat it like every other verb.
func (c *Client) Whoami(ctx context.Context) (Me, error) {
	var me Me
	err := c.doJSON(ctx, http.MethodGet, "/v1/me", nil, nil, &me)
	return me, err
}

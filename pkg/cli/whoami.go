package cli

import (
	"context"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"

	"github.com/go-go-golems/glazed/pkg/cmds/values"
)

// WhoamiCommand reports what the configured credential resolves to.
//
// Twenty lines, and it is the first thing anyone runs when a credential does
// not work. Without it the only diagnosis available is a 403 from an endpoint
// that cannot say which of "wrong token", "wrong user", "missing scope" or
// "not a member" it meant.
//
// It lives in package cli rather than in a group subpackage because it is the
// only verb in its group, and a one-file directory that exists to satisfy a
// naming rule is worse than the rule.
type WhoamiCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &WhoamiCommand{}

// NewWhoamiCommand builds `datalab whoami`.
func NewWhoamiCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := NewClientSection()
	if err != nil {
		return nil, err
	}

	return &WhoamiCommand{cmds.NewCommandDescription(
		"whoami",
		cmds.WithShort("Show who the current credential authenticates as"),
		cmds.WithLong(RenderAppText(strings.TrimSpace(`
Report the identity, kind and scopes of the configured credential.

Answers the four questions a 403 cannot distinguish between: is the token
valid, whose is it, what may it do, and is this server even running with user
accounts.

    {{app}} whoami
    {{app}} whoami --format json
    {{app}} whoami --format jsonl --output-fields user_id

An unauthenticated answer is a row like any other, with authenticated=false —
not an error. "This server accepted no credential" is information; a script
that wants it to be fatal can request JSONL and test the field with jq -e.
`))),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

// RunIntoGlazeProcessor emits the one row.
func (c *WhoamiCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s, err := ClientSettingsFrom(vals)
	if err != nil {
		return err
	}

	api, err := ClientFrom(vals)
	if err != nil {
		return err
	}

	me, err := api.Whoami(ctx)
	if err != nil {
		return err
	}
	return gp.AddRow(ctx, RowForPrincipal(strings.TrimRight(s.Addr, "/"), me))
}

package drops

import (
	"context"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// ListCommand lists the drops the credential can see.
type ListCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &ListCommand{}

// NewListCommand builds `datalab list`.
func NewListCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &ListCommand{cmds.NewCommandDescription(
		"list",
		cmds.WithShort("List drops"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
List the drops this credential can see, one row per drop.

The output uses Glazed's compact structured-output contract:

    {{app}} list
    {{app}} list --format json
    {{app}} list --format csv --output-fields name,created_at
    {{app}} list --format jsonl --output-fields name
`))),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

// RunIntoGlazeProcessor emits one row per drop.
//
// It returns errors rather than exiting on them. The exit-code mapping is
// applied by ddcli.WithExitCodes at registration, so a verb body stays ordinary
// Go and cannot forget it.
func (c *ListCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	found, err := api.ListDrops(ctx)
	if err != nil {
		return err
	}

	for _, drop := range found {
		if err := gp.AddRow(ctx, ddcli.RowForDrop(drop)); err != nil {
			return err
		}
	}
	return nil
}

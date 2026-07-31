package uicmd

import (
	"context"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

type ListCommand struct{ *cmds.CommandDescription }

var _ cmds.GlazeCommand = &ListCommand{}

func NewListCommand() (cmds.Command, error) {
	output, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}
	return &ListCommand{cmds.NewCommandDescription(
		"list",
		cmds.WithShort("List your PBUI workbenches"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
List workbench identity, name, revision, and update time.

    {{app}} ui list
    {{app}} ui list --format json
`))),
		cmds.WithSections(output, clientSection),
	)}, nil
}

func (c *ListCommand) RunIntoGlazeProcessor(
	ctx context.Context,
	vals *values.Values,
	processor middlewares.Processor,
) error {
	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}
	summaries, err := api.ListWorkbenches(ctx)
	if err != nil {
		return err
	}
	for _, summary := range summaries {
		if err := processor.AddRow(ctx, rowForSummary(summary)); err != nil {
			return err
		}
	}
	return nil
}

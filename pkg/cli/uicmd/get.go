package uicmd

import (
	"context"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"
	workbenchv1 "github.com/hyperslop-systems/pbui/gen/go/hyperslop/pbui/workbench/v1"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
)

type GetCommand struct{ *cmds.CommandDescription }

var _ cmds.GlazeCommand = &GetCommand{}

func NewGetCommand() (cmds.Command, error) {
	output, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}
	return &GetCommand{cmds.NewCommandDescription(
		"get",
		cmds.WithShort("Get a complete PBUI workbench snapshot"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Get the complete normalized workbench graph and its current revision.

    {{app}} ui get workbench-production --format json
`))),
		cmds.WithArguments(
			fields.New("workbench", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("workbench ID")),
		),
		cmds.WithSections(output, clientSection),
	)}, nil
}

type getSettings struct {
	WorkbenchID string `glazed:"workbench"`
}

func (c *GetCommand) RunIntoGlazeProcessor(
	ctx context.Context,
	vals *values.Values,
	processor middlewares.Processor,
) error {
	settings := &getSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return err
	}
	row, err := runResource(ctx, vals, func(ctx context.Context, api *client.Client) (
		*workbenchv1.WorkbenchResource, error,
	) {
		return api.GetWorkbench(ctx, settings.WorkbenchID)
	})
	if err != nil {
		return err
	}
	return processor.AddRow(ctx, row)
}

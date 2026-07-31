package uicmd

import (
	"context"
	"strconv"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"
	"github.com/go-go-golems/glazed/pkg/types"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

type DeleteCommand struct{ *cmds.CommandDescription }

var _ cmds.GlazeCommand = &DeleteCommand{}

func NewDeleteCommand() (cmds.Command, error) {
	output, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}
	return &DeleteCommand{cmds.NewCommandDescription(
		"delete",
		cmds.WithShort("Conditionally delete a workbench"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Delete a workbench only when --revision is still current.

    {{app}} ui delete workbench-scratch --revision 3
`))),
		cmds.WithArguments(workbenchArgument()),
		cmds.WithFlags(revisionField()),
		cmds.WithSections(output, clientSection),
	)}, nil
}

type deleteSettings struct {
	WorkbenchID string `glazed:"workbench"`
	Revision    string `glazed:"revision"`
}

func (c *DeleteCommand) RunIntoGlazeProcessor(
	ctx context.Context,
	vals *values.Values,
	processor middlewares.Processor,
) error {
	settings := &deleteSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return err
	}
	revision, err := parseRevision(settings.Revision)
	if err != nil {
		return err
	}
	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}
	if err := api.DeleteWorkbench(ctx, settings.WorkbenchID, revision); err != nil {
		return err
	}
	return processor.AddRow(ctx, types.NewRow(
		types.MRP("id", settings.WorkbenchID),
		types.MRP("deleted", true),
		types.MRP("revision", strconv.FormatUint(revision, 10)),
	))
}

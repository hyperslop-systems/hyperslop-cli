package uicmd

import (
	"context"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"
	workbenchv1 "github.com/hyperslop-systems/pbui/gen/go/hyperslop/pbui/workbench/v1"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
)

type ReplaceCommand struct{ *cmds.CommandDescription }

var _ cmds.GlazeCommand = &ReplaceCommand{}

func NewReplaceCommand() (cmds.Command, error) {
	output, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}
	return &ReplaceCommand{cmds.NewCommandDescription(
		"replace",
		cmds.WithShort("Conditionally replace a complete workbench snapshot"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Replace a complete workbench only when --revision is still current.

    {{app}} ui replace workbench-production --revision 8 --file workbench.json
`))),
		cmds.WithArguments(workbenchArgument()),
		cmds.WithFlags(fileField(), revisionField(), requestIDField()),
		cmds.WithSections(output, clientSection),
	)}, nil
}

func (c *ReplaceCommand) RunIntoGlazeProcessor(
	ctx context.Context,
	vals *values.Values,
	processor middlewares.Processor,
) error {
	settings, err := decodeFileSettings(vals)
	if err != nil {
		return err
	}
	revision, err := parseRevision(settings.Revision)
	if err != nil {
		return err
	}
	var document workbenchv1.WorkbenchDocument
	if err := decodeProtoFile(settings.File, &document); err != nil {
		return err
	}
	row, err := runResource(ctx, vals, func(ctx context.Context, api *client.Client) (
		*workbenchv1.WorkbenchResource, error,
	) {
		return api.ReplaceWorkbench(
			ctx, settings.WorkbenchID, revision, &document, settings.RequestID,
		)
	})
	if err != nil {
		return err
	}
	return processor.AddRow(ctx, row)
}

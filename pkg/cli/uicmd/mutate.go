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

type MutateCommand struct{ *cmds.CommandDescription }

var _ cmds.GlazeCommand = &MutateCommand{}

func NewMutateCommand() (cmds.Command, error) {
	output, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}
	return &MutateCommand{cmds.NewCommandDescription(
		"mutate",
		cmds.WithShort("Apply a typed PBUI mutation batch"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Atomically apply MutationBatch protobuf JSON when --revision is still current.
One batch can create documents and views, split or replace placements, and
rename or delete workspaces.

    {{app}} ui mutate workbench-production --revision 8 --file mutations.json
    cat mutations.json | {{app}} ui mutate workbench-production --revision 8 --file -
`))),
		cmds.WithArguments(workbenchArgument()),
		cmds.WithFlags(fileField(), revisionField(), requestIDField()),
		cmds.WithSections(output, clientSection),
	)}, nil
}

func (c *MutateCommand) RunIntoGlazeProcessor(
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
	var batch workbenchv1.MutationBatch
	if err := decodeProtoFile(settings.File, &batch); err != nil {
		return err
	}
	row, err := runResource(ctx, vals, func(ctx context.Context, api *client.Client) (
		*workbenchv1.WorkbenchResource, error,
	) {
		return api.MutateWorkbench(
			ctx, settings.WorkbenchID, revision, &batch, settings.RequestID,
		)
	})
	if err != nil {
		return err
	}
	return processor.AddRow(ctx, row)
}

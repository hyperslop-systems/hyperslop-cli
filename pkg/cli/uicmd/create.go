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
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
)

type CreateCommand struct{ *cmds.CommandDescription }

var _ cmds.GlazeCommand = &CreateCommand{}

func NewCreateCommand() (cmds.Command, error) {
	output, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}
	return &CreateCommand{cmds.NewCommandDescription(
		"create",
		cmds.WithShort("Create a PBUI workbench from a snapshot file"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Create a workbench at revision 1 from canonical WorkbenchDocument protobuf JSON.
The request ID is generated when omitted and can be supplied for explicit retry.

    {{app}} ui create --file workbench.json
    {{app}} ui create --file workbench.json --request-id setup-dashboard-1
`))),
		cmds.WithFlags(
			fields.New("file", fields.TypeString,
				fields.WithHelp(`WorkbenchDocument JSON path, or "-" for stdin`)),
			fields.New("request-id", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("stable idempotency key; generated when omitted")),
		),
		cmds.WithSections(output, clientSection),
	)}, nil
}

type createSettings struct {
	File      string `glazed:"file"`
	RequestID string `glazed:"request-id"`
}

func (c *CreateCommand) RunIntoGlazeProcessor(
	ctx context.Context,
	vals *values.Values,
	processor middlewares.Processor,
) error {
	settings := &createSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return err
	}
	if settings.File == "" {
		return errors.New(`--file is required (use "-" to read from stdin)`)
	}
	var document workbenchv1.WorkbenchDocument
	if err := decodeProtoFile(settings.File, &document); err != nil {
		return err
	}
	row, err := runResource(ctx, vals, func(ctx context.Context, api *client.Client) (
		*workbenchv1.WorkbenchResource, error,
	) {
		return api.CreateWorkbench(ctx, &document, settings.RequestID)
	})
	if err != nil {
		return err
	}
	return processor.AddRow(ctx, row)
}

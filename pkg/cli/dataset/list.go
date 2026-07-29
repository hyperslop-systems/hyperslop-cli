package dataset

import (
	"context"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// ListCommand lists a drop's datasets.
type ListCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &ListCommand{}

// NewListCommand builds `datadrop dataset list DROP`.
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
		cmds.WithShort("List a drop's datasets"),
		cmds.WithLong(strings.TrimSpace(`
List the datasets in a drop, one row each, with a summary of the newest version.

    datadrop dataset list greenhouse
    datadrop dataset list greenhouse --format json
    datadrop dataset list greenhouse --sort-by -latest_bytes
`)),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop to list datasets of")),
		),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type listSettings struct {
	Drop string `glazed:"drop"`
}

// RunIntoGlazeProcessor emits one row per dataset.
func (c *ListCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &listSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	found, err := api.ListDatasets(ctx, s.Drop)
	if err != nil {
		return err
	}

	for _, d := range found {
		if err := gp.AddRow(ctx, ddcli.RowForDataset(d)); err != nil {
			return err
		}
	}
	return nil
}

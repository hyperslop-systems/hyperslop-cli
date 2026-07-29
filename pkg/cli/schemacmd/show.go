package schemacmd

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

// ShowCommand shows the active schema for a stream.
type ShowCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &ShowCommand{}

// NewShowCommand builds `datadrop schema show DROP`.
func NewShowCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &ShowCommand{cmds.NewCommandDescription(
		"show",
		cmds.WithShort("Show the active schema for a stream"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Show the highest — that is, the active — schema version for a stream.

The spec column holds the JSON Schema document exactly as it was submitted.
Extract the column from the structured result before using it as a schema file:

    {{app}} schema show greenhouse --format jsonl --output-fields spec | jq '.spec' > reading.schema.json
    {{app}} schema put another-drop --file reading.schema.json --mode strict
`))),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop to read the schema of")),
		),
		cmds.WithFlags(ddcli.DropStreamField()),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type showSettings struct {
	Drop   string `glazed:"drop"`
	Stream string `glazed:"drop-stream"`
}

// RunIntoGlazeProcessor emits the one row.
func (c *ShowCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &showSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	found, err := api.GetSchema(ctx, s.Drop, s.Stream)
	if err != nil {
		return err
	}
	return gp.AddRow(ctx, ddcli.RowForSchema(found))
}

package drops

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

// InspectCommand shows one drop's metadata and counters.
type InspectCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &InspectCommand{}

// NewInspectCommand builds `datalab inspect DROP`.
//
// It returns one row rather than an indented JSON object, which looks like
// ceremony and is not. It makes --format json mean the same thing here as it
// does for list — an array, not a bare object, so a script does not have to
// know which verb it called. The structured output projection also makes
// `datalab inspect X --output-fields event_count` possible without jq.
func NewInspectCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &InspectCommand{cmds.NewCommandDescription(
		"inspect",
		cmds.WithShort("Show a drop's metadata and counters"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Show one drop's metadata alongside its cheap counters: how many events it
holds, the highest sequence allocated, when the last event arrived, and which
streams exist.

    {{app}} inspect greenhouse
    {{app}} inspect greenhouse --format json
    {{app}} inspect greenhouse --format jsonl --output-fields event_count
`))),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop to inspect")),
		),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type inspectSettings struct {
	Drop string `glazed:"drop"`
}

// RunIntoGlazeProcessor emits the one row.
func (c *InspectCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &inspectSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	stats, err := api.GetDrop(ctx, s.Drop)
	if err != nil {
		return err
	}
	return gp.AddRow(ctx, ddcli.RowForDropStats(stats))
}

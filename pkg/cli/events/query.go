package events

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
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

// QueryCommand reads a window of a stream.
type QueryCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &QueryCommand{}

// NewQueryCommand builds `datadrop query DROP`.
func NewQueryCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &QueryCommand{cmds.NewCommandDescription(
		"query",
		cmds.WithShort("Query events from a drop"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Query events from a drop by count, sequence cursor, or time range.

    {{app}} query greenhouse --limit 25
    {{app}} query greenhouse --from 2026-07-01T00:00:00Z --to 2026-07-02T00:00:00Z
    {{app}} query greenhouse --after 18440 --order asc

Each event is one row. The envelope fields come first and the payload follows,
flattened into data.* columns — the same column names the web workbench shows,
because both come from the same projection:

    {{app}} query greenhouse --limit 3 --output-fields seq,time,data.temp_c
    {{app}} query greenhouse --format csv --output-fields seq,data.temp_c
    {{app}} query greenhouse --format jsonl

For the original nested envelope rather than a flattened row, use
'{{app}} export --format ndjson'.
`))),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop to read")),
		),
		cmds.WithFlags(rangeFields(datadrop.DefaultLimit)...),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type querySettings struct {
	Drop string `glazed:"drop"`
	rangeSettings
}

// RunIntoGlazeProcessor emits one row per event.
func (c *QueryCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &querySettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	q, err := s.query(s.Drop)
	if err != nil {
		return err
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	result, err := api.Query(ctx, q)
	if err != nil {
		return err
	}
	return emitEvents(ctx, gp, result.Events)
}

// emitEvents projects a page and adds every row.
//
// Every event in this package goes through it, so there is exactly one
// flattening (guide §19, "two flatteners"): a verb that built its own row would
// make --output-fields data.temp_c work on query and return empty on tail.
func emitEvents(ctx context.Context, gp middlewares.Processor, events []datadrop.Envelope) error {
	rows, err := ddcli.RowsForEnvelopes(events)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := gp.AddRow(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

package uicmd

import (
	"context"
	"strconv"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"
	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// StreamCommand emits workbench revision invalidations until its context is
// cancelled.
type StreamCommand struct{ *cmds.CommandDescription }

var _ cmds.GlazeCommand = &StreamCommand{}

func NewStreamCommand() (cmds.Command, error) {
	output, err := settings.NewStructuredOutputSection(
		schema.WithDefaults(map[string]interface{}{"format": "jsonl"}),
	)
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}
	return &StreamCommand{cmds.NewCommandDescription(
		"stream",
		cmds.WithShort("Stream PBUI workbench revision invalidations"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Subscribe to committed revisions for one workbench. The server immediately
emits its durable current revision when it is newer than --after, then emits
future commits. Events are invalidations: use 'ui get' to read each complete
authoritative snapshot.

The command defaults to JSONL because the stream is unbounded. Ctrl-C ends a
healthy stream with exit status zero.

    {{app}} ui stream workbench-production
    {{app}} ui stream workbench-production --after 7
`))),
		cmds.WithArguments(workbenchArgument()),
		cmds.WithFlags(
			fields.New("after", fields.TypeString,
				fields.WithDefault("0"),
				fields.WithHelp("emit only revisions newer than this unsigned revision")),
		),
		cmds.WithSections(output, clientSection),
	)}, nil
}

type streamSettings struct {
	WorkbenchID string `glazed:"workbench"`
	After       string `glazed:"after"`
}

func (c *StreamCommand) RunIntoGlazeProcessor(
	ctx context.Context,
	vals *values.Values,
	processor middlewares.Processor,
) error {
	s := &streamSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	output := &settings.StructuredOutputSettings{}
	if err := vals.DecodeSectionInto(settings.StructuredOutputSlug, output); err != nil {
		return err
	}
	if output.Format != settings.OutputJSONL {
		return errors.Errorf("ui stream requires --format jsonl, got %s", output.Format)
	}
	after, err := parseAfterRevision(s.After)
	if err != nil {
		return err
	}
	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}
	events, errs, err := api.StreamWorkbench(ctx, s.WorkbenchID, after)
	if err != nil {
		return err
	}
	for events != nil || errs != nil {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if err := processor.AddRow(ctx, types.NewRow(
				types.MRP("workbench_id", event.WorkbenchID),
				types.MRP("revision", strconv.FormatUint(event.Revision, 10)),
			)); err != nil {
				return err
			}
		case streamErr, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if streamErr != nil {
				return streamErr
			}
		}
	}
	return nil
}

func parseAfterRevision(value string) (uint64, error) {
	revision, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errors.Errorf("--after must be an unsigned integer, got %q", value)
	}
	return revision, nil
}

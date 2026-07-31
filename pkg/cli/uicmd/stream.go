package uicmd

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"
	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
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
	return followWorkbenchStream(ctx, processor, api, s.WorkbenchID, after)
}

func parseAfterRevision(value string) (uint64, error) {
	revision, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errors.Errorf("--after must be an unsigned integer, got %q", value)
	}
	return revision, nil
}

const (
	workbenchReconnectInitial = time.Second
	workbenchReconnectMaximum = 30 * time.Second
)

func followWorkbenchStream(
	ctx context.Context,
	processor middlewares.Processor,
	api *client.Client,
	workbenchID string,
	after uint64,
) error {
	return followWorkbenchStreamWithWait(
		ctx, processor, api, workbenchID, after, waitForWorkbenchReconnect,
	)
}

func followWorkbenchStreamWithWait(
	ctx context.Context,
	processor middlewares.Processor,
	api *client.Client,
	workbenchID string,
	after uint64,
	waitFor func(context.Context, time.Duration) error,
) error {
	reconnectDelay := workbenchReconnectInitial
	for {
		events, errs, err := api.StreamWorkbench(ctx, workbenchID, after)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var apiErr *client.APIError
			if errors.As(err, &apiErr) {
				return err
			}
			if err := waitFor(ctx, reconnectDelay); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			reconnectDelay = nextWorkbenchReconnectDelay(reconnectDelay)
			continue
		}

		sawEvent := false
		var streamErr error
		for events != nil || errs != nil {
			select {
			case <-ctx.Done():
				return nil
			case event, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				if event.Revision <= after {
					continue
				}
				if err := processor.AddRow(ctx, types.NewRow(
					types.MRP("workbench_id", event.WorkbenchID),
					types.MRP("revision", strconv.FormatUint(event.Revision, 10)),
				)); err != nil {
					return err
				}
				after = event.Revision
				sawEvent = true
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				if err != nil {
					streamErr = err
				}
			}
		}

		if ctx.Err() != nil {
			return nil
		}
		if streamErr != nil {
			var readErr *client.StreamReadError
			if !errors.As(streamErr, &readErr) {
				return streamErr
			}
		}
		if sawEvent {
			reconnectDelay = workbenchReconnectInitial
		}
		if err := waitFor(ctx, reconnectDelay); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		reconnectDelay = nextWorkbenchReconnectDelay(reconnectDelay)
	}
}

func waitForWorkbenchReconnect(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextWorkbenchReconnectDelay(current time.Duration) time.Duration {
	if current >= workbenchReconnectMaximum/2 {
		return workbenchReconnectMaximum
	}
	return current * 2
}

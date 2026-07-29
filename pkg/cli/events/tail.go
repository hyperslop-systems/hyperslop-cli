package events

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

// tailDefaultLimit is how much recent history a tail shows before following.
const tailDefaultLimit = 10

// TailCommand shows the most recent events and optionally follows new ones.
type TailCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &TailCommand{}

// NewTailCommand builds `datadrop tail DROP`.
//
// Glazed v1.4 has one explicit streaming wire format: JSONL. Defaulting tail
// to it ensures each event is flushed as one complete object, including when
// --follow keeps the command alive indefinitely. A caller can still request
// the bounded table formatter explicitly with --format table.
func NewTailCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection(
		schema.WithDefaults(map[string]interface{}{"format": "jsonl"}),
	)
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &TailCommand{cmds.NewCommandDescription(
		"tail",
		cmds.WithShort("Show the most recent events, optionally following new ones"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Show the most recent events in a drop.

With --follow, the command subscribes to the drop's SSE stream and prints new
events as they are appended, resuming from the last sequence it saw if the
connection drops. Ctrl-C ends it and exits 0. Because the live feed resumes by
sequence only, --follow cannot be combined with --after, --before, --from, or
--to; use a bounded tail/query for those filters.

    {{app}} tail greenhouse
    {{app}} tail greenhouse --follow --output-fields time,data.temp_c

tail defaults to --format jsonl, the Glazed v1.4 streaming contract. Each event
is flushed as one compact JSON object per line, including while --follow keeps
the command alive. Request the default table format explicitly for a bounded
tail when a terminal table is more useful.

    {{app}} tail greenhouse --format table
    {{app}} tail greenhouse --follow --format jsonl
`))),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop to read")),
		),
		cmds.WithFlags(append(rangeFields(tailDefaultLimit),
			fields.New("follow", fields.TypeBool,
				fields.WithShortFlag("f"),
				fields.WithDefault(false),
				fields.WithHelp("stream new events as they arrive")),
		)...),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type tailSettings struct {
	Drop   string `glazed:"drop"`
	Follow bool   `glazed:"follow"`
	rangeSettings
}

// RunIntoGlazeProcessor emits the recent page and then, with --follow, one row
// per event as it arrives.
func (c *TailCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &tailSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	output := &settings.StructuredOutputSettings{}
	if err := vals.DecodeSectionInto(settings.StructuredOutputSlug, output); err != nil {
		return err
	}
	if err := s.validateFollow(output.Format); err != nil {
		return err
	}

	q, err := s.tailQuery()
	if err != nil {
		return err
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	recent, err := api.Query(ctx, q)
	if err != nil {
		return err
	}

	ordered := reverse(recent.Events)
	if err := emitEvents(ctx, gp, ordered); err != nil {
		return err
	}

	if !s.Follow {
		return nil
	}

	cursor := int64(0)
	if len(ordered) > 0 {
		cursor = ordered[len(ordered)-1].Seq
	}
	return followStream(ctx, gp, api, s.Drop, s.Stream, cursor)
}

// validateFollow enforces every invariant of the unbounded execution mode in
// one place: the SSE endpoint can preserve only a sequence cursor, and only
// JSONL has streaming formatter semantics in Glazed v1.4.
func (s *tailSettings) validateFollow(format settings.OutputFormat) error {
	if !s.Follow {
		return nil
	}
	if format != settings.OutputJSONL {
		return errors.Errorf("--follow requires --format jsonl, got %s", format)
	}
	var incompatible []string
	if s.After > 0 {
		incompatible = append(incompatible, "--after")
	}
	if s.Before > 0 {
		incompatible = append(incompatible, "--before")
	}
	if s.From != "" {
		incompatible = append(incompatible, "--from")
	}
	if s.To != "" {
		incompatible = append(incompatible, "--to")
	}
	if len(incompatible) > 0 {
		return errors.Errorf("--follow cannot be combined with %s: the live stream resumes by sequence cursor only",
			strings.Join(incompatible, ", "))
	}
	return nil
}

// tailQuery fixes the newest-first retrieval order before Normalize validates
// cursor/order combinations. Setting it afterwards could turn a valid
// after+ascending request into an invalid after+descending request sent to the
// server. Tail is intrinsically newest-first; the page is reversed for output.
func (s *tailSettings) tailQuery() (datadrop.EventQuery, error) {
	s.Order = string(datadrop.OrderDesc)
	return s.query(s.Drop)
}

// followStream tails the SSE feed, reconnecting from the last sequence it saw.
//
// Resumption needs exactly one piece of state — the cursor — which is what
// makes the server's hub allowed to drop slow subscribers: a disconnect is
// recoverable, never lossy.
//
// The context is already wired to SIGINT and SIGTERM by glazed's cobra builder,
// so Ctrl-C cancels it here. A cancelled context is the user stopping something
// that was working, so it returns nil rather than an error.
const (
	streamReconnectInitial = time.Second
	streamReconnectMaximum = 30 * time.Second
)

func followStream(
	ctx context.Context, gp middlewares.Processor, api *client.Client,
	drop, stream string, cursor int64,
) error {
	return followStreamWithWait(ctx, gp, api, drop, stream, cursor, waitForReconnect)
}

func followStreamWithWait(
	ctx context.Context, gp middlewares.Processor, api *client.Client,
	drop, stream string, cursor int64,
	waitFor func(context.Context, time.Duration) error,
) error {
	reconnectDelay := streamReconnectInitial
	for {
		frames, errs, err := api.Stream(ctx, drop, stream, cursor)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// HTTP responses such as 401/404 are permanent until the caller
			// changes configuration; retrying them forever would hide a useful
			// error. Transport failures are transient and use the same bounded
			// reconnect policy as a clean proxy/server EOF.
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
			reconnectDelay = nextReconnectDelay(reconnectDelay)
			continue
		}

		disconnectedByReset := false
		sawFrame := false
		for frame := range frames {
			sawFrame = true
			switch frame.Name {
			case "reset":
				// The server evicted us for falling behind. Resume from the
				// cursor it reported rather than starting over.
				fmt.Fprintf(os.Stderr, "note: stream reset (%s); resuming from seq %d\n",
					frame.Reset.Reason, frame.Reset.Cursor)
				if frame.Reset.Cursor > cursor {
					cursor = frame.Reset.Cursor
				}
				disconnectedByReset = true

			default:
				if frame.Envelope.Seq <= cursor {
					continue
				}
				row, err := ddcli.RowForEnvelope(frame.Envelope)
				if err != nil {
					return err
				}
				if err := gp.AddRow(ctx, row); err != nil {
					return err
				}
				cursor = frame.Envelope.Seq
			}
		}

		if streamErr := <-errs; streamErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			var readErr *client.StreamReadError
			if !errors.As(streamErr, &readErr) {
				// Malformed frame JSON and projection failures are protocol/data
				// errors. Reconnecting would replay the same bad frame forever.
				return streamErr
			}
			if sawFrame {
				reconnectDelay = streamReconnectInitial
			}
			if err := waitFor(ctx, reconnectDelay); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			reconnectDelay = nextReconnectDelay(reconnectDelay)
			continue
		}
		if ctx.Err() != nil {
			return nil
		}
		if disconnectedByReset {
			// A reset is an explicit request to reconnect immediately.
			reconnectDelay = streamReconnectInitial
			continue
		}

		// A clean EOF is not a successful end for --follow. Proxies and load
		// balancers routinely close idle SSE responses; return-to-shell here
		// would silently miss every later event. Resume from cursor with bounded
		// backoff to avoid a tight loop when an endpoint closes immediately.
		if sawFrame {
			reconnectDelay = streamReconnectInitial
		}
		if err := waitFor(ctx, reconnectDelay); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		reconnectDelay = nextReconnectDelay(reconnectDelay)
	}
}

func waitForReconnect(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextReconnectDelay(current time.Duration) time.Duration {
	if current >= streamReconnectMaximum/2 {
		return streamReconnectMaximum
	}
	return current * 2
}

func reverse(events []datadrop.Envelope) []datadrop.Envelope {
	reversed := make([]datadrop.Envelope, len(events))
	for i, e := range events {
		reversed[len(events)-1-i] = e
	}
	return reversed
}

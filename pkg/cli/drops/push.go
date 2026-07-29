package drops

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

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

// PushCommand appends events to a drop.
type PushCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &PushCommand{}

// NewPushCommand builds `datadrop push DROP [key=value ...]`.
//
// The variadic tail is a fields.TypeStringList argument and must be the last
// one declared; only one list argument is allowed.
func NewPushCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &PushCommand{cmds.NewCommandDescription(
		"push",
		cmds.WithShort("Append an event to a drop"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Append an event to a drop.

Values in key=value pairs are parsed as JSON when possible, so temperature=21.7
becomes the number 21.7 rather than the string "21.7". Use --string to force
string interpretation.

Payloads can also be piped in:

    printf '{"temperature":22.8}' | {{app}} push greenhouse --stdin
    cat readings.ndjson | {{app}} push greenhouse --stdin --ndjson

Each accepted event is one row, so an NDJSON push reports every sequence it
allocated:

    cat readings.ndjson | {{app}} push greenhouse --stdin --ndjson --format jsonl --output-fields seq
`))),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop to append to")),
			fields.New("pairs", fields.TypeStringList,
				fields.WithIsArgument(true),
				fields.WithDefault([]string{}),
				fields.WithHelp("key=value pairs forming the payload")),
		),
		cmds.WithFlags(
			fields.New(ddcli.DropStreamFlag, fields.TypeString,
				fields.WithDefault(datadrop.DefaultStream),
				fields.WithHelp("stream within the drop (was --stream before v0.2)")),
			fields.New("stdin", fields.TypeBool,
				fields.WithDefault(false),
				fields.WithHelp("read the payload from stdin instead of key=value arguments")),
			fields.New("ndjson", fields.TypeBool,
				fields.WithDefault(false),
				fields.WithHelp("with --stdin, treat input as newline-delimited JSON (one event per line)")),
			fields.New("string", fields.TypeStringList,
				fields.WithDefault([]string{}),
				fields.WithHelp("key=value pair whose value is always treated as a string; repeatable")),
			fields.New("source", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("CloudEvents source, e.g. device:sensor-7")),
			fields.New("type", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("CloudEvents type, e.g. io.datadrop.reading.v1")),
			fields.New("subject", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("CloudEvents subject, e.g. greenhouse/zone-a")),
		),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type pushSettings struct {
	Drop      string   `glazed:"drop"`
	Pairs     []string `glazed:"pairs"`
	Stream    string   `glazed:"drop-stream"`
	Stdin     bool     `glazed:"stdin"`
	NDJSON    bool     `glazed:"ndjson"`
	Strings   []string `glazed:"string"`
	Source    string   `glazed:"source"`
	EventType string   `glazed:"type"`
	Subject   string   `glazed:"subject"`
}

// RunIntoGlazeProcessor appends one event per payload and emits one row per
// accepted event.
func (c *PushCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &pushSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	if s.Stdin && len(s.Pairs) > 0 {
		return errors.New("--stdin cannot be combined with key=value arguments")
	}
	if s.NDJSON && !s.Stdin {
		return errors.New("--ndjson requires --stdin")
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	overrides := envelopeOverrides{
		source: s.Source, eventType: s.EventType, subject: s.Subject,
	}

	if s.Stdin {
		return pushFromStdin(ctx, gp, api, s, overrides)
	}
	if len(s.Pairs) == 0 {
		return errors.New("provide key=value arguments or --stdin")
	}

	payload, err := payloadFromFields(s.Pairs, s.Strings)
	if err != nil {
		return err
	}
	return pushOne(ctx, gp, api, s, payload, overrides)
}

// envelopeOverrides are the CloudEvents attributes a pusher can set.
type envelopeOverrides struct {
	source    string
	eventType string
	subject   string
}

func (o envelopeOverrides) empty() bool {
	return o.source == "" && o.eventType == "" && o.subject == ""
}

// wrap builds a full envelope around a payload when any attribute was set.
// Otherwise the payload is sent bare, which keeps the simple path simple.
func (o envelopeOverrides) wrap(payload json.RawMessage) (json.RawMessage, bool, error) {
	if o.empty() {
		return payload, false, nil
	}

	envelope := map[string]any{
		"specversion": datadrop.SpecVersion,
		"data":        payload,
	}
	if o.source != "" {
		envelope["source"] = o.source
	}
	if o.eventType != "" {
		envelope["type"] = o.eventType
	}
	if o.subject != "" {
		envelope["subject"] = o.subject
	}

	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, false, errors.Wrap(err, "encode envelope")
	}
	return encoded, true, nil
}

func pushOne(
	ctx context.Context, gp middlewares.Processor, api *client.Client,
	s *pushSettings, payload json.RawMessage, overrides envelopeOverrides,
) error {
	body, isEnvelope, err := overrides.wrap(payload)
	if err != nil {
		return err
	}

	result, err := api.Push(ctx, s.Drop, s.Stream, body, isEnvelope)
	if err != nil {
		return err
	}

	// Warnings and the duplicate note are diagnostics; the row is the result.
	// Keeping them on separate streams means the output stays pipeable.
	for _, warning := range result.Warnings {
		location := warning.Path
		if location == "" {
			location = "(root)"
		}
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", location, warning.Message)
	}
	if result.Duplicate {
		fmt.Fprintf(os.Stderr,
			"note: event %s already existed; no new event was appended\n", result.ID)
	}

	return gp.AddRow(ctx, ddcli.RowForAppendResult(result))
}

// pushFromStdin sends either one JSON document or a stream of NDJSON lines.
func pushFromStdin(
	ctx context.Context, gp middlewares.Processor, api *client.Client,
	s *pushSettings, overrides envelopeOverrides,
) error {
	if !s.NDJSON {
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			return errors.Wrap(err, "read stdin")
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			return errors.New("stdin is empty")
		}
		return pushOne(ctx, gp, api, s, body, overrides)
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	pushed := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := pushOne(ctx, gp, api, s, json.RawMessage(line), overrides); err != nil {
			return errors.Wrapf(err, "line %d", pushed+1)
		}
		pushed++
	}
	if err := scanner.Err(); err != nil {
		return errors.Wrap(err, "read stdin")
	}

	fmt.Fprintf(os.Stderr, "pushed %d events\n", pushed)
	return nil
}

// payloadFromFields turns key=value arguments into a JSON object.
//
// Each value is parsed as JSON when it is valid JSON, and treated as a string
// otherwise — so temperature=21.7 is a number, note=hello is a string, and
// tags=["a","b"] is an array. --string forces the string reading when the
// heuristic guesses wrong.
func payloadFromFields(pairs, stringPairs []string) (json.RawMessage, error) {
	payload := map[string]any{}

	for _, field := range stringPairs {
		key, value, err := splitField(field)
		if err != nil {
			return nil, err
		}
		payload[key] = value
	}

	for _, field := range pairs {
		key, value, err := splitField(field)
		if err != nil {
			return nil, err
		}

		var parsed any
		if json.Valid([]byte(value)) && json.Unmarshal([]byte(value), &parsed) == nil {
			payload[key] = parsed
		} else {
			payload[key] = value
		}
	}

	encoded, err := json.Marshal(payload)
	return encoded, errors.Wrap(err, "encode payload")
}

func splitField(field string) (string, string, error) {
	key, value, found := strings.Cut(field, "=")
	if !found || key == "" {
		return "", "", errors.Errorf("invalid field %q: expected key=value", field)
	}
	return key, value, nil
}

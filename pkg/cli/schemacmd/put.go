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
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

// PutCommand registers a new schema version for a stream.
type PutCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &PutCommand{}

// NewPutCommand builds `datadrop schema put DROP`.
func NewPutCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &PutCommand{cmds.NewCommandDescription(
		"put",
		cmds.WithShort("Register a new schema version for a stream"),
		cmds.WithLong(strings.TrimSpace(`
Register a new JSON Schema version for a stream.

Each put creates a new immutable version; the highest version is the active
one. Mode "strict" rejects invalid payloads with 422, which the CLI reports as
exit code 5; mode "permissive" accepts them and attaches warnings to the stored
event.

    datadrop schema put greenhouse --file reading.schema.json --mode strict
    cat reading.schema.json | datadrop schema put greenhouse --file -
`)),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop whose stream is being constrained")),
		),
		cmds.WithFlags(
			ddcli.DropStreamField(),
			fields.New("file", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp(`path to the JSON Schema document, or "-" for stdin (required)`)),
			fields.New("mode", fields.TypeChoice,
				fields.WithChoices(string(datadrop.ModeStrict), string(datadrop.ModePermissive)),
				fields.WithDefault(string(datadrop.ModeStrict)),
				fields.WithHelp("validation mode")),
		),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type putSettings struct {
	Drop   string `glazed:"drop"`
	Stream string `glazed:"drop-stream"`
	File   string `glazed:"file"`
	Mode   string `glazed:"mode"`
}

// RunIntoGlazeProcessor registers the version and emits it.
func (c *PutCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &putSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	if s.File == "" {
		return errors.New(`--file is required (use "-" to read from stdin)`)
	}
	mode, err := datadrop.ParseMode(s.Mode)
	if err != nil {
		return err
	}
	spec, err := ddcli.ReadSpec(s.File)
	if err != nil {
		return err
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	result, err := api.PutSchema(ctx, s.Drop, s.Stream, spec, mode)
	if err != nil {
		return err
	}
	return gp.AddRow(ctx, ddcli.RowForPutSchemaResult(result))
}

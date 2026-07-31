package drops

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"
)

// CreateCommand creates a drop.
type CreateCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &CreateCommand{}

// NewCreateCommand builds `datalab create NAME`.
func NewCreateCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &CreateCommand{cmds.NewCommandDescription(
		"create",
		cmds.WithShort("Create a drop"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Create a named drop.

A drop is the unit of naming, sharing and export. It always has a default
stream called "events".

    {{app}} create greenhouse
    {{app}} create greenhouse --retention 90d --public-read
    {{app}} create greenhouse --format jsonl --output-fields name

The created drop is emitted as one row, so a script can pipe it onward instead
of parsing a sentence.
`))),
		cmds.WithArguments(
			fields.New("name", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop to create")),
		),
		cmds.WithFlags(
			fields.New("public-read", fields.TypeBool,
				fields.WithDefault(false),
				fields.WithHelp("allow unauthenticated reads of this drop")),
			fields.New("retention", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp(`retention window, e.g. "90d" (stored but not enforced in v0.1)`)),
		),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type createSettings struct {
	Name       string `glazed:"name"`
	PublicRead bool   `glazed:"public-read"`
	Retention  string `glazed:"retention"`
}

// RunIntoGlazeProcessor creates the drop and emits it.
func (c *CreateCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &createSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	created, err := api.CreateDrop(ctx, datalab.CreateDropRequest{
		Name:       s.Name,
		Retention:  s.Retention,
		PublicRead: s.PublicRead,
	})
	if err != nil {
		return err
	}

	if s.Retention != "" {
		// Say so once, at the point of surprise, rather than only in the
		// README: an operator who sets a retention and sees no deletions must
		// not conclude the server is broken.
		//
		// On stderr, because it is a diagnostic and the row is the result.
		fmt.Fprintf(os.Stderr,
			"note: retention %q is recorded but not enforced in v0.1; nothing is deleted\n",
			s.Retention)
	}

	return gp.AddRow(ctx, ddcli.RowForDrop(created))
}

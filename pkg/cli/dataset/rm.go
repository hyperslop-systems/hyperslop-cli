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
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// RmCommand deletes a dataset version.
type RmCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &RmCommand{}

// NewRmCommand builds `datadrop dataset rm DROP DATASET`.
func NewRmCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &RmCommand{cmds.NewCommandDescription(
		"rm",
		cmds.WithShort("Delete a dataset version"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Delete a dataset version.

The version's file records are removed. The bytes themselves are left in place,
because other versions may share them; unreferenced bytes are reclaimed by
'{{app}} dataset gc'.

    {{app}} dataset rm greenhouse readings-2026 --version 1
`))),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop holding the dataset")),
			fields.New("dataset", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the dataset name")),
		),
		cmds.WithFlags(
			fields.New("version", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp(`version to delete: a number or "latest" (required)`)),
		),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type rmSettings struct {
	Drop    string `glazed:"drop"`
	Dataset string `glazed:"dataset"`
	Version string `glazed:"version"`
}

// RunIntoGlazeProcessor deletes the version and says which one.
func (c *RmCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &rmSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	if s.Version == "" {
		return errors.New("--version is required; deleting a whole dataset is not supported")
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	deleted, err := api.DeleteDatasetVersion(ctx, s.Drop, s.Dataset, s.Version)
	if err != nil {
		return err
	}
	return gp.AddRow(ctx, ddcli.RowForDeletedVersion(deleted))
}

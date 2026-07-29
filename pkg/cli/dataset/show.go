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

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// ShowCommand shows a dataset or one of its versions.
type ShowCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &ShowCommand{}

// NewShowCommand builds `datadrop dataset show DROP DATASET`.
//
// Without --version it emits one row per version of the dataset, which is what
// makes `--sort-by -version` and `--output-fields version,file_count,total_bytes`
// useful; with --version it emits that one version. Both shapes are the same
// row type, so a script does not branch.
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
		cmds.WithShort("Show a dataset or one of its versions"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Show a dataset's versions, or one named version.

    {{app}} dataset show greenhouse readings-2026
    {{app}} dataset show greenhouse readings-2026 --version latest
    {{app}} dataset show greenhouse readings-2026 --output-fields version,file_count,total_bytes
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
				fields.WithHelp(`version to show: a number or "latest" (default: every version)`)),
		),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type showSettings struct {
	Drop    string `glazed:"drop"`
	Dataset string `glazed:"dataset"`
	Version string `glazed:"version"`
}

// RunIntoGlazeProcessor emits one row per version.
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

	if s.Version != "" {
		found, err := api.GetDatasetVersion(ctx, s.Drop, s.Dataset, s.Version)
		if err != nil {
			return err
		}
		return gp.AddRow(ctx, ddcli.RowForDatasetVersion(found))
	}

	found, err := api.GetDataset(ctx, s.Drop, s.Dataset)
	if err != nil {
		return err
	}
	for _, version := range found.Versions {
		if err := gp.AddRow(ctx, ddcli.RowForDatasetVersion(version)); err != nil {
			return err
		}
	}
	return nil
}

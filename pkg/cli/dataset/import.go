package dataset

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
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"
)

// ImportCommand materializes a dataset file's rows into an event stream.
type ImportCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &ImportCommand{}

// NewImportCommand builds `datalab dataset import DROP DATASET`.
func NewImportCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &ImportCommand{cmds.NewCommandDescription(
		"import",
		cmds.WithShort("Materialize a dataset file's rows into an event stream"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Materialize a dataset file's rows into an event stream.

Each row becomes one event carrying provenance back to the dataset version, the
file, the row number, and the digest of the exact bytes it came from.

Event identifiers are derived from (digest, row), so re-running an interrupted
import resumes rather than duplicating: rows already imported are reported as
skipped.

    {{app}} dataset import greenhouse readings-2026 --path data/readings.csv
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
				fields.WithHelp(`version to import from: a number or "latest" (default)`)),
			fields.New("path", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("file within the dataset to import (required)")),
			ddcli.DropStreamField(),
			// --row-format and not --format: this verb also carries --output
			// from the Glazed section, and two flags that both read as "what
			// shape is the data" on one command is the confusion the
			// --format/--output rule exists to prevent. The name also matches
			// what the flag actually selects, which is how pkg/tabular reads
			// the file's rows.
			fields.New("row-format", fields.TypeChoice,
				fields.WithChoices("", "csv", "ndjson"),
				fields.WithDefault(""),
				fields.WithHelp("how to read the file's rows: csv or ndjson (default: inferred from the file name; was --format before v0.2)")),
			fields.New("max-rows", fields.TypeInteger,
				fields.WithDefault(0),
				fields.WithHelp("maximum rows to import (default: the server's limit)")),
			fields.New("strict", fields.TypeBool,
				fields.WithDefault(false),
				fields.WithHelp("reject the import if any row fails the dataset schema")),
		),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type importSettings struct {
	Drop    string `glazed:"drop"`
	Dataset string `glazed:"dataset"`
	Version string `glazed:"version"`
	Path    string `glazed:"path"`
	Stream  string `glazed:"drop-stream"`
	Format  string `glazed:"row-format"`
	MaxRows int    `glazed:"max-rows"`
	Strict  bool   `glazed:"strict"`
}

// RunIntoGlazeProcessor runs the import and emits its summary.
func (c *ImportCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &importSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	if s.Path == "" {
		return errors.New("--path is required: name the file within the dataset")
	}
	if s.Version == "" {
		s.Version = datalab.LatestVersion
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	result, err := api.ImportDataset(ctx, s.Drop, s.Dataset, s.Version,
		s.Path, s.Stream, s.Format, s.MaxRows, s.Strict)
	if err != nil {
		return err
	}

	for _, warning := range result.Warnings {
		location := warning.Path
		if location == "" {
			location = "(root)"
		}
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", location, warning.Message)
	}
	if result.Truncated {
		fmt.Fprintf(os.Stderr,
			"note: stopped at %s; pass --max-rows to raise it\n", importLimitDescription(s.MaxRows))
	}

	return gp.AddRow(ctx, ddcli.RowForImportResult(result))
}

func importLimitDescription(maxRows int) string {
	if maxRows > 0 {
		return fmt.Sprintf("the %d-row limit", maxRows)
	}
	return "the server's row limit"
}

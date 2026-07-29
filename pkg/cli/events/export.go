package events

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

// ExportCommand streams the server's canonical export.
//
// It is a WriterCommand and not a GlazeCommand (DR-75), which is the
// classification easiest to get wrong here, because an export produces exactly
// the kind of tabular data Glazed exists for. Look at what the command does: it
// opens a response body and copies it. The formatting happens on the server, in
// pkg/tabular, which is also what `curl` and the web UI get.
//
// Turning it into rows would fetch NDJSON, parse it, and re-serialise it as CSV
// on the client. That moves the export format definition from one place to two,
// so `curl …/export?format=csv` and `datadrop export --format csv` can
// disagree; it loses streaming, because io.Copy is constant-memory over a 400 MB
// export and a table formatter buffers every row to compute column widths; and
// it gains nothing a caller wants, since someone running `datadrop export` is
// asking for the server's canonical export rather than for a client-side view
// of it.
type ExportCommand struct {
	*cmds.CommandDescription
}

var _ cmds.WriterCommand = &ExportCommand{}

// NewExportCommand builds `datadrop export DROP`.
func NewExportCommand() (cmds.Command, error) {
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &ExportCommand{cmds.NewCommandDescription(
		"export",
		cmds.WithShort("Export a drop's events in an open format"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Export a drop's events as CSV, NDJSON, or JSON.

    {{app}} export greenhouse --format csv > readings.csv
    {{app}} export greenhouse --format ndjson --from 2026-07-23T00:00:00Z

--format names a SERVER-side format. The bytes are produced by the server and
streamed through untouched, so this is the same file 'curl .../export?format=csv'
downloads and the same one the web UI's download button produces.

Unlike row-producing verbs, export does not mount Glazed's structured-output
section. Its --format field is an operation input sent to the server rather than
a client-side serializer choice.

Use this rather than 'query --format jsonl' when you want the original nested
envelope, or when the export is large enough that buffering it would matter.
`))),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop to export")),
		),
		cmds.WithFlags(append(rangeFields(datadrop.MaxLimit),
			fields.New("format", fields.TypeChoice,
				fields.WithChoices("csv", "ndjson", "json"),
				fields.WithDefault("ndjson"),
				fields.WithHelp("server-side export format")),
			fields.New("output-file", fields.TypeString,
				fields.WithShortFlag("o"),
				fields.WithDefault(""),
				fields.WithHelp("write to a file instead of stdout")),
		)...),
		cmds.WithSections(clientSection),
	)}, nil
}

type exportSettings struct {
	Drop       string `glazed:"drop"`
	Format     string `glazed:"format"`
	OutputFile string `glazed:"output-file"`
	rangeSettings
}

// RunIntoWriter copies the server's bytes through.
func (c *ExportCommand) RunIntoWriter(
	ctx context.Context, vals *values.Values, w io.Writer,
) error {
	s := &exportSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	// Export is always a chronological replay. Set its order before the shared
	// range builder normalizes directional cursors.
	s.Order = string(datadrop.OrderAsc)
	q, err := s.query(s.Drop)
	if err != nil {
		return err
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	body, err := api.Export(ctx, s.Drop, s.Format, q)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	sink := w
	if s.OutputFile != "" {
		file, err := os.Create(s.OutputFile)
		if err != nil {
			return errors.Wrapf(err, "create %s", s.OutputFile)
		}
		defer func() { _ = file.Close() }()
		sink = file
	}

	_, err = io.Copy(sink, body)
	return errors.Wrap(err, "write export")
}

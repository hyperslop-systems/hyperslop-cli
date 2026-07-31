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

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// GCCommand deletes stored bytes that no dataset references.
type GCCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &GCCommand{}

// NewGCCommand builds `datalab dataset gc`.
func NewGCCommand() (cmds.Command, error) {
	glazedSection, err := settings.NewStructuredOutputSection()
	if err != nil {
		return nil, err
	}
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &GCCommand{cmds.NewCommandDescription(
		"gc",
		cmds.WithShort("Delete stored bytes that no dataset references"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Delete stored bytes that no dataset version references.

Deleting a dataset version leaves its bytes in place, because other versions may
share them. This sweep reclaims the ones nothing references any more.

Blobs younger than the minimum age are never deleted: a file uploaded into a
draft that has not yet recorded its metadata row is momentarily unreferenced,
and the age check is what keeps a sweep from destroying an in-flight upload.

    {{app}} dataset gc
    {{app}} dataset gc --min-age-seconds 3600 --format jsonl --output-fields freed_bytes
`))),
		cmds.WithFlags(
			fields.New("min-age-seconds", fields.TypeInteger,
				fields.WithDefault(0),
				fields.WithHelp("minimum age before an unreferenced blob may be deleted (default: the server's grace period)")),
		),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type gcSettings struct {
	MinAgeSeconds int `glazed:"min-age-seconds"`
}

// RunIntoGlazeProcessor runs the sweep and emits its summary.
func (c *GCCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &gcSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	result, err := api.GarbageCollect(ctx, s.MinAgeSeconds)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr,
		"scanned %d blob(s), %d referenced, deleted %d, freed %s\n",
		result.Scanned, result.Referenced, result.Deleted, ddcli.HumanBytes(result.FreedBytes))

	return gp.AddRow(ctx, ddcli.RowForGCResult(result))
}

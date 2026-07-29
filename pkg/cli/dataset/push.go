package dataset

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	"github.com/hyperslop-systems/hyperslop-cli/pkg/jsondoc"
)

// FlattenFlag stores files under their base name instead of their local path.
//
// It is "flatten-paths" and not "flatten", which it was before this ticket, for
// the same reason --stream became --drop-stream: glazed's fields-filters
// section already owns --flatten, and two sections cannot both define it. The
// name that had to move is the one glazed does not own.
const FlattenFlag = "flatten-paths"

// PushCommand publishes a new dataset version.
type PushCommand struct {
	*cmds.CommandDescription
}

var _ cmds.GlazeCommand = &PushCommand{}

// NewPushCommand builds `datadrop dataset push DROP DATASET`.
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
		cmds.WithShort("Publish a new dataset version"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Publish a new dataset version.

Each file is hashed locally first, and the server is asked whether it already
holds those bytes. Files it already has are recorded without being transferred,
so republishing a dataset with one changed file uploads only that file.

    {{app}} dataset push greenhouse readings-2026 \
        --file data/readings.csv --file README.md \
        --title "Greenhouse readings, 2026 season" --license CC-BY-4.0

The transfer summary goes to stderr and the version is the row, so the output
stays pipeable.
`))),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop to publish into")),
			fields.New("dataset", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the dataset name")),
		),
		cmds.WithFlags(
			fields.New("file", fields.TypeStringList,
				fields.WithDefault([]string{}),
				fields.WithHelp("local file to publish; repeatable. Use LOCAL:LOGICAL to rename within the dataset")),
			fields.New("manifest", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp(`manifest JSON document, or "-" for stdin`)),
			fields.New("schema", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("JSON Schema describing one record of the dataset")),
			fields.New("title", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("dataset title (merged into the manifest)")),
			fields.New("license", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("license identifier, e.g. CC-BY-4.0 (merged into the manifest)")),
			fields.New("description", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("dataset description (merged into the manifest)")),
			fields.New(FlattenFlag, fields.TypeBool,
				fields.WithDefault(false),
				fields.WithHelp("store files under their base name instead of their local path (was --flatten before v0.2)")),
		),
		cmds.WithSections(glazedSection, clientSection),
	)}, nil
}

type pushSettings struct {
	Drop        string   `glazed:"drop"`
	Dataset     string   `glazed:"dataset"`
	Files       []string `glazed:"file"`
	Manifest    string   `glazed:"manifest"`
	Schema      string   `glazed:"schema"`
	Title       string   `glazed:"title"`
	License     string   `glazed:"license"`
	Description string   `glazed:"description"`
	Flatten     bool     `glazed:"flatten-paths"`
}

// RunIntoGlazeProcessor publishes the version and emits it.
func (c *PushCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	s := &pushSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	if len(s.Files) == 0 {
		return errors.New("at least one --file is required")
	}

	commitReq, err := buildCommitRequest(s)
	if err != nil {
		return err
	}

	pushFiles, err := resolvePushFiles(s.Files, s.Flatten)
	if err != nil {
		return err
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	result, err := api.PushDataset(ctx, s.Drop, s.Dataset, pushFiles, commitReq)
	if err != nil {
		return err
	}

	// The transfer summary is a diagnostic; the version document is the result.
	fmt.Fprintf(os.Stderr,
		"uploaded %d file(s) (%s), reused %d already-stored file(s) (%s)\n",
		result.Uploaded, ddcli.HumanBytes(result.BytesSent),
		result.Mounted, ddcli.HumanBytes(result.BytesSkipped))

	return gp.AddRow(ctx, ddcli.RowForDatasetVersion(result.Version))
}

// resolvePushFiles turns --file arguments into local/logical pairs.
//
// A LOCAL:LOGICAL form allows renaming; otherwise the local path is used as the
// logical path, cleaned of any leading "./" and rejected if it escapes upwards.
func resolvePushFiles(files []string, flatten bool) ([]client.PushFile, error) {
	resolved := make([]client.PushFile, 0, len(files))
	seen := map[string]string{}

	for _, entry := range files {
		local, logical := entry, ""
		if before, after, found := strings.Cut(entry, ":"); found && after != "" {
			local, logical = before, after
		}

		if logical == "" {
			logical = local
			if flatten {
				logical = filepath.Base(local)
			}
		}
		logical = filepath.ToSlash(filepath.Clean(logical))
		logical = strings.TrimPrefix(logical, "./")

		if err := datadrop.ValidateDatasetPath(logical); err != nil {
			return nil, errors.Wrapf(err, "--file %q", entry)
		}
		if info, err := os.Stat(local); err != nil {
			return nil, errors.Wrapf(err, "--file %q", entry)
		} else if !info.Mode().IsRegular() {
			return nil, errors.Errorf("--file %q is not a regular file", entry)
		}
		if previous, duplicate := seen[logical]; duplicate {
			return nil, errors.Errorf("two files map to the same dataset path %q: %q and %q",
				logical, previous, local)
		}
		seen[logical] = local

		resolved = append(resolved, client.PushFile{LocalPath: local, LogicalPath: logical})
	}
	return resolved, nil
}

// buildCommitRequest merges the manifest document with the convenience flags.
//
// The flags win over the document, so a scripted manifest can be overridden on
// the command line without editing the file.
func buildCommitRequest(s *pushSettings) (datadrop.CommitVersionRequest, error) {
	manifest := map[string]any{}

	if s.Manifest != "" {
		raw, err := ddcli.ReadSpec(s.Manifest)
		if err != nil {
			return datadrop.CommitVersionRequest{}, err
		}
		if err := jsondoc.Decode(raw, &manifest); err != nil {
			return datadrop.CommitVersionRequest{}, errors.Wrapf(err, "manifest %s", s.Manifest)
		}
		// JSON null is a valid absent manifest (matching ParseManifest). It
		// decodes a map to nil, so restore an object before applying CLI
		// overrides instead of panicking on assignment below.
		if manifest == nil {
			manifest = map[string]any{}
		}
	}

	for key, value := range map[string]string{
		"title": s.Title, "license": s.License, "description": s.Description,
	} {
		if value != "" {
			manifest[key] = value
		}
	}

	req := datadrop.CommitVersionRequest{}
	if len(manifest) > 0 {
		encoded, err := json.Marshal(manifest)
		if err != nil {
			return datadrop.CommitVersionRequest{}, errors.Wrap(err, "encode manifest")
		}
		req.Manifest = encoded
	}

	if s.Schema != "" {
		spec, err := ddcli.ReadSpec(s.Schema)
		if err != nil {
			return datadrop.CommitVersionRequest{}, err
		}
		if len(strings.TrimSpace(string(spec))) == 0 {
			return datadrop.CommitVersionRequest{}, errors.Errorf("schema %s is empty", s.Schema)
		}
		req.Schema = spec
	}
	return req, nil
}

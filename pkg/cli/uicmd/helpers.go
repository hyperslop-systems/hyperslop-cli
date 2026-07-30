package uicmd

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/types"
	workbenchv1 "github.com/hyperslop-systems/pbui/gen/go/hyperslop/pbui/workbench/v1"
	"github.com/hyperslop-systems/pbui/pkg/workbenchapi"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
)

func workbenchArgument() *fields.Definition {
	return fields.New(
		"workbench",
		fields.TypeString,
		fields.WithIsArgument(true),
		fields.WithHelp("workbench ID"),
	)
}

func fileField() *fields.Definition {
	return fields.New(
		"file",
		fields.TypeString,
		fields.WithHelp(`protobuf JSON path, or "-" for stdin`),
	)
}

func revisionField() *fields.Definition {
	return fields.New(
		"revision",
		fields.TypeString,
		fields.WithHelp("positive current workbench revision"),
	)
}

func requestIDField() *fields.Definition {
	return fields.New(
		"request-id",
		fields.TypeString,
		fields.WithDefault(""),
		fields.WithHelp("stable idempotency key; generated when omitted"),
	)
}

type fileSettings struct {
	WorkbenchID string `glazed:"workbench"`
	File        string `glazed:"file"`
	Revision    string `glazed:"revision"`
	RequestID   string `glazed:"request-id"`
}

func decodeFileSettings(vals *values.Values) (*fileSettings, error) {
	settings := &fileSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return nil, err
	}
	if settings.File == "" {
		return nil, errors.New(`--file is required (use "-" to read from stdin)`)
	}
	return settings, nil
}

func decodeProtoFile(path string, message proto.Message) error {
	data, err := ddcli.ReadSpec(path)
	if err != nil {
		return err
	}
	return workbenchapi.Unmarshal(data, message)
}

func parseRevision(value string) (uint64, error) {
	revision, err := strconv.ParseUint(value, 10, 64)
	if err != nil || revision == 0 {
		return 0, errors.Errorf("--revision must be a positive unsigned integer, got %q", value)
	}
	return revision, nil
}

func rowForSummary(summary *workbenchv1.WorkbenchSummary) types.Row {
	row := types.NewRow(
		types.MRP("id", summary.Id),
		types.MRP("name", summary.Name),
		types.MRP("revision", strconv.FormatUint(summary.Revision, 10)),
	)
	if summary.UpdatedAt != nil && summary.UpdatedAt.IsValid() {
		row.Set("updated_at", summary.UpdatedAt.AsTime())
	}
	return row
}

func rowForResource(resource *workbenchv1.WorkbenchResource) (types.Row, error) {
	if resource == nil || resource.Workbench == nil {
		return nil, errors.New("workbench response is missing its snapshot")
	}
	data, err := workbenchapi.Marshal(resource.Workbench)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, errors.Wrap(err, "decode workbench row")
	}
	row := types.NewRow(
		types.MRP("id", resource.Workbench.Id),
		types.MRP("name", resource.Workbench.Name),
		types.MRP("revision", strconv.FormatUint(resource.Revision, 10)),
		types.MRP("workbench", document),
	)
	if resource.CreatedAt != nil && resource.CreatedAt.IsValid() {
		row.Set("created_at", resource.CreatedAt.AsTime())
	}
	if resource.UpdatedAt != nil && resource.UpdatedAt.IsValid() {
		row.Set("updated_at", resource.UpdatedAt.AsTime())
	}
	return row, nil
}

func runResource(
	ctx context.Context,
	vals *values.Values,
	call func(context.Context, *client.Client) (*workbenchv1.WorkbenchResource, error),
) (types.Row, error) {
	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return nil, err
	}
	resource, err := call(ctx, api)
	if err != nil {
		return nil, err
	}
	return rowForResource(resource)
}

// Package uicmd exposes agent-facing PBUI workbench query and mutation verbs.
package uicmd

import (
	"strings"

	"github.com/spf13/cobra"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// Register attaches the `hyperslop ui` command group.
func Register(root *cobra.Command) error {
	group := &cobra.Command{
		Use:   "ui",
		Short: "Query and manipulate PBUI workbenches",
		Long: strings.TrimSpace(`
Query and manipulate server-backed PBUI workbenches.

Snapshots and mutation batches use canonical protobuf JSON. Every write is
conditional on a revision and create, replace, and mutate accept an optional
request ID for safe retries.
`),
	}
	if err := ddcli.AddCommands(
		group,
		NewListCommand,
		NewGetCommand,
		NewCreateCommand,
		NewReplaceCommand,
		NewMutateCommand,
		NewDeleteCommand,
	); err != nil {
		return err
	}
	root.AddCommand(group)
	return nil
}

// Package dataset holds the seven verbs that publish and retrieve bulk
// datasets.
//
// Six of them return records and are Glaze commands. `get` writes files to
// disk and is a Bare command (DR-81): its result is on the filesystem, and a
// row saying so would be a description of the real answer rather than the
// answer.
package dataset

import (
	"strings"

	"github.com/spf13/cobra"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// Register attaches the `datadrop dataset` group.
func Register(root *cobra.Command) error {
	group := &cobra.Command{
		Use:   "dataset",
		Short: "Publish and retrieve bulk datasets",
		Long: strings.TrimSpace(`
Publish and retrieve bulk datasets.

A dataset is a named, versioned collection of files inside a drop. Unlike a
stream, which is unbounded and appended to one event at a time, a dataset
version is finite and immutable: correcting it means publishing a new version.
`),
	}

	if err := ddcli.AddCommands(group,
		NewPushCommand,
		NewListCommand,
		NewShowCommand,
		NewGetCommand,
		NewImportCommand,
		NewRmCommand,
		NewGCCommand,
	); err != nil {
		return err
	}
	root.AddCommand(group)
	return nil
}

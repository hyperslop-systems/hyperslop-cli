// Package schemacmd holds the two verbs that manage a stream's JSON Schema
// contract: put and show.
//
// The package is schemacmd rather than schema because glazed/pkg/cmds/schema is
// imported by every verb file in it, and one of the two names has to give.
package schemacmd

import (
	"strings"

	"github.com/spf13/cobra"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// Register attaches `datadrop schema put` and `datadrop schema show`.
func Register(root *cobra.Command) error {
	group := &cobra.Command{
		Use:   "schema",
		Short: "Manage a drop's JSON Schema contracts",
		Long: strings.TrimSpace(`
Register and inspect the JSON Schema a stream's payloads are validated against.

Each put creates a new immutable version; the highest version is the active
one.
`),
	}

	if err := ddcli.AddCommands(group, NewPutCommand, NewShowCommand); err != nil {
		return err
	}
	root.AddCommand(group)
	return nil
}

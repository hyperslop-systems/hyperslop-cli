// Package drops holds the four verbs that act on a drop as a whole: create,
// list, inspect and push.
//
// The package groups them in the source without grouping them in the CLI —
// they stay top-level verbs, `datalab list` rather than `datalab drops list`,
// because that is the existing surface and this is not a renaming ticket
// (DR-82).
package drops

import (
	"github.com/spf13/cobra"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// Register attaches this group's verbs to the root command.
//
// This is the only place in the package where cobra is mentioned, which is what
// keeps the verb files free of wiring.
func Register(root *cobra.Command) error {
	return ddcli.AddCommands(root,
		NewCreateCommand,
		NewListCommand,
		NewInspectCommand,
		NewPushCommand,
	)
}

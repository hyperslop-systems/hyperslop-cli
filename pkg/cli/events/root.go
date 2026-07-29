// Package events holds the three verbs that read a stream: query, tail and
// export.
//
// query and tail return records and are Glaze commands. export returns bytes
// the server formatted and is a Writer command (DR-75) — which is why this
// package is the one place where the --format / --output distinction is
// concrete rather than theoretical. --format names a server-side export
// format; --output names the client-side rendering of rows a command emitted.
// No command here has both.
package events

import (
	"github.com/spf13/cobra"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// Register attaches this group's verbs to the root command.
func Register(root *cobra.Command) error {
	return ddcli.AddCommands(root,
		NewQueryCommand,
		NewTailCommand,
		NewExportCommand,
	)
}

// Package authcmd registers commands that establish a local Datadrop
// credential without accepting an upstream OIDC bearer token.
package authcmd

import (
	"strings"

	"github.com/spf13/cobra"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
)

// Register attaches the `datadrop auth` command group.
func Register(root *cobra.Command) error {
	group := &cobra.Command{
		Use:   "auth",
		Short: "Establish local Datadrop credentials",
		Long: strings.TrimSpace(`
Authentication commands establish local Datadrop credentials.

They never accept a ZITADEL/OIDC access token as a Datadrop data-plane
credential. Browser sign-in creates a local dd_session; device pairing creates
a scoped, revocable local ddp_ token after browser approval.
`),
	}
	if err := ddcli.AddCommands(group, NewDeviceCommand); err != nil {
		return err
	}
	root.AddCommand(group)
	return nil
}

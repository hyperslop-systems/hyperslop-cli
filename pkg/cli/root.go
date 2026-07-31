// Package cli assembles the hyperslop command tree.
//
// hyperslop is the agent/customer-facing CLI for the datalab backend: a thin
// client of the server's HTTP API. It exposes the data verbs (create, push,
// query, tail, export, dataset, schema), the device-pairing auth flow, and
// whoami — and nothing operator-only: no serve, no healthcheck. If a verb works
// here, curl works against the same endpoint.
//
// The shared CLI foundation (client section, command builder, exit codes, row
// projections) lives in this package; the command groups live in subpackages
// (authcmd, drops, events, dataset, schemacmd) and are passed in as registrars
// by cmd/hyperslop/main.go, because those subpackages import this one for the
// foundation — so the dependency runs one way and this package must not import
// them back.
package cli

import (
	"os"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds/logging"
	"github.com/go-go-golems/glazed/pkg/help"
	help_cmd "github.com/go-go-golems/glazed/pkg/help/cmd"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/doc"
)

// NewHyperslopRootCmd builds the customer-only command tree.
//
// The registrars come in as arguments (named by cmd/hyperslop/main.go) rather
// than being imported, because the group subpackages import this package for the
// client section, row projections and exit helper.
func NewHyperslopRootCmd(registrars ...Registrar) (*cobra.Command, error) {
	// The customer binary's identity: HYPERSLOP_* env vars and the "hyperslop: "
	// diagnostic prefix. The admin datalab binary sets these to "datalab".
	SetAppName("hyperslop")
	SetErrorPrefix("hyperslop: ")

	root := &cobra.Command{
		Use:   "hyperslop",
		Short: "Agent/customer CLI for the datalab backend",
		Long: strings.TrimSpace(`
hyperslop is the agent/customer-facing client for a datalab server: it accepts
append-only event data, reads latest-N and time-range queries, tails a live SSE
feed, exports CSV/NDJSON/JSON, publishes and retrieves bulk datasets, manages
JSON Schema contracts, and pairs through a browser-approved device flow to mint
a scoped ddp_ token.

Point it at a server and mint a token first:

    export HYPERSLOP_ADDR=http://data.example.com
    export HYPERSLOP_TOKEN="$(hyperslop auth device --name 'local coding agent' --scopes drops:read,drops:write --expires-in 24h)"

Then:

    hyperslop create greenhouse
    hyperslop push greenhouse temperature=21.7 humidity=0.48
    hyperslop query greenhouse --limit 10
    hyperslop tail greenhouse --follow
    hyperslop export greenhouse --format csv
    hyperslop whoami
`),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return logging.InitLoggerFromCobra(cmd)
		},
	}

	// whoami is the one customer verb without a group, owned directly here.
	if err := AddCommands(root, NewWhoamiCommand); err != nil {
		return nil, err
	}

	for _, register := range registrars {
		if err := register(root); err != nil {
			return nil, err
		}
	}

	// The Glazed help system, loaded from the customer-facing pages embedded in
	// pkg/doc (getting-a-token, cli-output).
	helpSystem := help.NewHelpSystem()
	if err := doc.AddDocToHelpSystem(helpSystem); err != nil {
		return nil, errors.Wrap(err, "loading embedded documentation")
	}
	help_cmd.SetupCobraRootCommand(helpSystem, root)

	if err := addHyperslopLoggingSection(root); err != nil {
		return nil, err
	}

	return root, nil
}

// addHyperslopLoggingSection installs the Glazed logging flags and restores the
// HYPERSLOP_LOG_LEVEL fallback on top of them. See the admin root for the
// rationale on why the fallback is applied with Set rather than via the default.
func addHyperslopLoggingSection(root *cobra.Command) error {
	if err := logging.AddLoggingSectionToRootCommand(root, "hyperslop"); err != nil {
		return errors.Wrap(err, "adding the logging section")
	}
	if level := strings.TrimSpace(os.Getenv("HYPERSLOP_LOG_LEVEL")); level != "" {
		if err := root.PersistentFlags().Set("log-level", level); err != nil {
			return errors.Wrapf(err, "invalid HYPERSLOP_LOG_LEVEL %q", level)
		}
	}
	return nil
}

// Execute runs the hyperslop root command and maps errors onto the documented
// exit codes. It is the only place in the CLI that writes to stderr directly.
func Execute(registrars ...Registrar) int {
	root, err := NewHyperslopRootCmd(registrars...)
	if err != nil {
		_, _ = os.Stderr.WriteString(ErrorPrefix() + err.Error() + "\n")
		return ExitError
	}
	if err := root.Execute(); err != nil {
		_, _ = os.Stderr.WriteString(ErrorPrefix() + err.Error() + "\n")
		if IsCommandError(err) {
			return ExitCodeFor(err)
		}
		// Parser/traversal errors (unknown flag/subcommand, bad arguments) are
		// invocation failures rather than command runtime failures.
		return ExitUsage
	}
	return ExitOK
}

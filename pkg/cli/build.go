package cli

import (
	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// appName is the env prefix the parser reads <APP>_* variables under (e.g.
// HYPERSLOP_ADDR for the hyperslop binary, DATADROP_ADDR for the datadrop admin
// binary). Set it once per binary with SetAppName before building commands.
//
// It has to be set on the parser config of every command, because that is what
// switches on glazed's built-in env source. Leave it empty and the env vars
// silently stop working — the same trap the logging section hit with the log
// level, one layer up.
var appName = "hyperslop"

// SetAppName sets the env prefix used for the client connection flags and their
// help text. Call once, from the root, before any command is built.
func SetAppName(name string) { appName = name }

// AppName returns the configured env prefix.
func AppName() string { return appName }

// Builder constructs one Glazed command.
type Builder func() (cmds.Command, error)

// BuildCobraCommand turns a Glazed command into a cobra command with
// datadrop's conventions applied.
//
// Two of those conventions matter:
//
//   - WithExitCodes wraps the command so that every error it returns is mapped
//     onto the documented exit codes and reported with the "datadrop: " prefix
//     before glazed's cobra.CheckErr can turn it into "Error: " and exit 1. See
//     exit.go.
//   - ShortHelpSections keeps `datadrop query --help` focused on the command's
//     own fields and client connection fields; structured output remains
//     available through its compact section.
//
// MiddlewaresFunc is deliberately not set. Supplying one replaces glazed's
// default chain and takes the env source with it, which is how DATADROP_ADDR
// stops working without anything reporting an error.
func BuildCobraCommand(command cmds.Command) (*cobra.Command, error) {
	cobraCmd, err := cli.BuildCobraCommandFromCommand(
		WithExitCodes(command),
		cli.WithParserConfig(cli.CobraParserConfig{
			ShortHelpSections: []string{schema.DefaultSlug, ClientSectionSlug},
			AppName:           AppName(),
		}),
	)
	if err != nil {
		return nil, errors.Wrapf(err, "building the %s command", command.Description().Name)
	}
	return cobraCmd, nil
}

// AddCommands builds each command and attaches it to parent.
//
// This is the only thing a group's root.go has to do, and it is why no verb
// file contains cobra wiring.
func AddCommands(parent *cobra.Command, builders ...Builder) error {
	for _, build := range builders {
		command, err := build()
		if err != nil {
			return err
		}
		cobraCmd, err := BuildCobraCommand(command)
		if err != nil {
			return err
		}
		parent.AddCommand(cobraCmd)
	}
	return nil
}

// Registrar attaches a group's verbs wherever they belong in the tree.
//
// The registrars live in subpackages of this one — pkg/cli/drops,
// pkg/cli/events and so on — and those subpackages import this package for the
// client section, the row projections and the exit helper. So the dependency
// has to run one way only: this package must not import them back. NewRootCmd
// therefore takes the registrars as arguments and cmd/datadrop/main.go names
// them, which is the one place in the tree that knows about every group.
type Registrar func(root *cobra.Command) error

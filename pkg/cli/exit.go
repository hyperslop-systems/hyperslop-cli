package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
)

// The exit-code contract, and why it needs a helper at all.
//
// pkg/cli/root.go documents five exit codes and cmd/datadrop/smoke_test.go
// asserts them, because a script branches on *why* a command failed rather than
// parsing stderr. Before this ticket, Execute() owned the mapping: cobra
// returned the error, Execute() called exitCodeFor and returned the code to
// main.
//
// Glazed's cobra builder takes that away. Verified against glazed v1.3.8, which
// is what go.mod pins:
//
//	// glazed@v1.3.8/pkg/cli/cobra.go:48
//	cmd.Run = func(cmd *cobra.Command, args []string) {
//	    ...
//	    err = runFunc(ctx, parsedValues)
//	    if _, ok := err.(*cmds.ExitWithoutGlazeError); ok {
//	        os.Exit(0)
//	    }
//	    cobra.CheckErr(err)   // <- prints "Error: ..." and os.Exit(1)
//	}
//
// It sets cmd.Run, not cmd.RunE. The error never reaches root.Execute(), so
// Execute() never sees it, cobra.CheckErr exits 1 for everything, and the
// message prefix silently changes from "datadrop: " to "Error: ".
// commandBuildConfig has five fields and none of them is an error hook, so
// there is no option to change this from the outside.
// Upstream: https://github.com/go-go-golems/glazed/issues/611
//
// The local answer is ExitOn: map the error, print it with the binary's own
// prefix, and exit before returning — so cobra.CheckErr is never reached on an
// error path. Rather than threading `return ExitOn(err)` through every error
// site in nineteen verbs (which is the failure mode where one forgotten call
// site loses the mapping for one verb, and looks like it works), the mapping is
// applied once per command by WithExitCodes at registration time.

// ExitCode values are part of the CLI contract. Scripts depend on them, so they
// must not be renumbered.
const (
	ExitOK         = 0
	ExitError      = 1
	ExitUsage      = 2
	ExitAuth       = 3
	ExitNotFound   = 4
	ExitValidation = 5
)

// errorPrefix is what every diagnostic starts with. Set it once per binary
// with SetErrorPrefix ("hyperslop: " for the customer CLI, "datadrop: " for the
// admin CLI) so every error path uses one spelling.
var errorPrefix = "hyperslop: "

// SetErrorPrefix sets the diagnostic prefix. Call once, from the root, before
// running commands.
func SetErrorPrefix(prefix string) { errorPrefix = prefix }

// ErrorPrefix returns the configured diagnostic prefix.
func ErrorPrefix() string { return errorPrefix }

// exitFunc is os.Exit, indirected so a test can observe the code instead of
// killing the test binary.
var exitFunc = os.Exit

// errSink is where ExitOn writes. Indirected for the same reason.
var errSink io.Writer = os.Stderr

// ExitOn maps err onto the documented exit codes, reports it, and exits.
//
// It returns an error only for the signals that must be handed back untouched:
// nil and *cmds.ExitWithoutGlazeError, which glazed's builder interprets as
// "exit 0". A command-specific long-running loop such as tail --follow may
// convert its own intentional cancellation to nil; cancellation of finite work
// remains a failure because its output may be incomplete.
// Everything else terminates the process here, which is the point.
func ExitOn(err error) error {
	if err == nil {
		return nil
	}

	var exitWithoutGlaze *cmds.ExitWithoutGlazeError
	if errors.As(err, &exitWithoutGlaze) {
		return err
	}

	_, _ = fmt.Fprintln(errSink, ErrorPrefix()+err.Error())
	exitFunc(ExitCodeFor(err))
	return nil // unreachable in production; reachable when exitFunc is stubbed
}

// ExitCodeFor maps an error onto the documented exit codes, so a script can
// branch on why a command failed without parsing its stderr.
func ExitCodeFor(err error) int {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ExitAuth
		case http.StatusNotFound:
			return ExitNotFound
		case http.StatusBadRequest, http.StatusUnprocessableEntity,
			http.StatusConflict, http.StatusRequestEntityTooLarge:
			return ExitValidation
		}
	}
	return ExitError
}

// WithExitCodes wraps a Glazed command so that every error it returns goes
// through ExitOn.
//
// This is the whole exit-code fix, applied in one place per command instead of
// at every `return err`. A verb body stays ordinary Go: it returns errors and
// never mentions os.Exit.
func WithExitCodes(command cmds.Command) cmds.Command {
	switch typed := command.(type) {
	case cmds.GlazeCommand:
		return &exitCodeGlazeCommand{GlazeCommand: typed}
	case cmds.WriterCommand:
		return &exitCodeWriterCommand{WriterCommand: typed}
	case cmds.BareCommand:
		return &exitCodeBareCommand{BareCommand: typed}
	default:
		return command
	}
}

type exitCodeGlazeCommand struct {
	cmds.GlazeCommand
}

var _ cmds.GlazeCommand = &exitCodeGlazeCommand{}

func (c *exitCodeGlazeCommand) RunIntoGlazeProcessor(
	ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
	return ExitOn(c.GlazeCommand.RunIntoGlazeProcessor(ctx, vals, gp))
}

type exitCodeWriterCommand struct {
	cmds.WriterCommand
}

var _ cmds.WriterCommand = &exitCodeWriterCommand{}

func (c *exitCodeWriterCommand) RunIntoWriter(
	ctx context.Context, vals *values.Values, w io.Writer,
) error {
	return ExitOn(c.WriterCommand.RunIntoWriter(ctx, vals, w))
}

type exitCodeBareCommand struct {
	cmds.BareCommand
}

var _ cmds.BareCommand = &exitCodeBareCommand{}

func (c *exitCodeBareCommand) Run(ctx context.Context, vals *values.Values) error {
	return ExitOn(c.BareCommand.Run(ctx, vals))
}

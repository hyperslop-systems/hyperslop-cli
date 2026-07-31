package cli

import (
	"context"
	"io"
	"net/http"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
)

// The exit-code contract, and why it needs a helper at all.
//
// Glazed v1.4.1 propagates command errors through Cobra RunE, so the binary root
// owns diagnostics and process exit. The wrapper here adds two pieces Cobra
// does not know: the stable 1/3/4/5 code carried by codedExitError, and Glaze
// processor finalization when a row-producing command fails after emitting
// successful rows. Immediate os.Exit is forbidden here: it bypasses deferred
// formatter cleanup and can hide already-completed writes from JSON/table output.

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
// with SetErrorPrefix ("hyperslop: " for the customer CLI, "datalab: " for the
// admin CLI) so every error path uses one spelling.
var errorPrefix = "hyperslop: "

// SetErrorPrefix sets the diagnostic prefix. Call once, from the root, before
// running commands.
func SetErrorPrefix(prefix string) { errorPrefix = prefix }

// ErrorPrefix returns the configured diagnostic prefix.
func ErrorPrefix() string { return errorPrefix }

type codedExitError struct {
	cause error
	code  int
}

func (e *codedExitError) Error() string { return e.cause.Error() }
func (e *codedExitError) Unwrap() error { return e.cause }

// ExitOn annotates err with its stable exit code and returns it to Cobra. It
// never prints or exits; the binary root does that only after Glazed has had a
// chance to finalize buffered output.
func ExitOn(err error) error {
	if err == nil {
		return nil
	}
	var exitWithoutGlaze *cmds.ExitWithoutGlazeError
	if errors.As(err, &exitWithoutGlaze) {
		return err
	}
	var alreadyCoded *codedExitError
	if errors.As(err, &alreadyCoded) {
		return err
	}
	return &codedExitError{cause: err, code: ExitCodeFor(err)}
}

// IsCommandError reports whether a wrapped command (rather than Cobra's own
// argument/flag traversal) produced err. Binary roots use this to distinguish
// generic command failure 1 from invocation usage failure 2.
func IsCommandError(err error) bool {
	var coded *codedExitError
	return errors.As(err, &coded)
}

// ExitCodeFor maps an error onto the documented exit codes, so a script can
// branch on why a command failed without parsing its stderr.
func ExitCodeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	var coded *codedExitError
	if errors.As(err, &coded) {
		return coded.code
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ExitAuth
		case http.StatusNotFound:
			return ExitNotFound
		case http.StatusBadRequest, http.StatusUnprocessableEntity,
			http.StatusConflict, http.StatusPreconditionFailed,
			http.StatusRequestEntityTooLarge:
			return ExitValidation
		}
	}
	return ExitError
}

// WithExitCodes wraps each command once so verb bodies return ordinary errors.
// Glaze commands additionally close their processor on failure, preserving
// successful rows emitted before a later input/API error.
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
	err := c.GlazeCommand.RunIntoGlazeProcessor(ctx, vals, gp)
	if err == nil {
		return nil // Glazed's outer runner closes the processor on success.
	}
	var exitWithoutGlaze *cmds.ExitWithoutGlazeError
	if errors.As(err, &exitWithoutGlaze) {
		return err
	}
	// Formatting successful rows is cleanup, not command work; cancellation of
	// the request must not suppress it. The outer runner sees the returned error
	// and therefore will not close the processor a second time.
	if closeErr := gp.Close(context.WithoutCancel(ctx)); closeErr != nil {
		err = errors.Wrapf(err, "finalize partial output: %v", closeErr)
	}
	return ExitOn(err)
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

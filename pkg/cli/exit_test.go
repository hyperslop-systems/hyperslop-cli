package cli

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
)

func apiError(status int, code string) error {
	return &client.APIError{Status: status, Code: code, Detail: "test"}
}

func TestExitOnMapsAPIStatusesWithoutExiting(t *testing.T) {
	cases := []struct{ status, want int }{
		{http.StatusUnauthorized, ExitAuth}, {http.StatusForbidden, ExitAuth},
		{http.StatusNotFound, ExitNotFound}, {http.StatusBadRequest, ExitValidation},
		{http.StatusUnprocessableEntity, ExitValidation}, {http.StatusConflict, ExitValidation},
		{http.StatusRequestEntityTooLarge, ExitValidation}, {http.StatusInternalServerError, ExitError},
	}
	for _, tc := range cases {
		mapped := ExitOn(apiError(tc.status, "Test"))
		if !IsCommandError(mapped) || ExitCodeFor(mapped) != tc.want {
			t.Errorf("status %d mapped to %d (coded=%v), want %d", tc.status, ExitCodeFor(mapped), IsCommandError(mapped), tc.want)
		}
	}
}

func TestExitOnPreservesWrappedCause(t *testing.T) {
	cause := apiError(http.StatusNotFound, "NotFound")
	mapped := ExitOn(errors.Wrap(errors.Wrap(cause, "fetch"), "query"))
	var got *client.APIError
	if ExitCodeFor(mapped) != ExitNotFound || !errors.As(mapped, &got) || got != cause {
		t.Fatalf("mapped error lost code/cause: %v", mapped)
	}
	if ExitCodeFor(ExitOn(context.Canceled)) != ExitError || !errors.Is(ExitOn(context.Canceled), context.Canceled) {
		t.Fatal("cancellation did not remain an ordinary coded failure")
	}
}

func TestExitOnPassesSpecialValuesThrough(t *testing.T) {
	if ExitOn(nil) != nil || ExitCodeFor(nil) != ExitOK {
		t.Fatal("nil was not preserved")
	}
	signal := &cmds.ExitWithoutGlazeError{}
	if got := ExitOn(signal); !errors.Is(got, signal) || IsCommandError(got) {
		t.Fatalf("ExitWithoutGlazeError changed: %v", got)
	}
}

func TestWithExitCodesWrapsGlazeCommand(t *testing.T) {
	command, err := NewWhoamiCommand()
	if err != nil {
		t.Fatal(err)
	}
	wrapped := WithExitCodes(command)
	if _, ok := wrapped.(*exitCodeGlazeCommand); !ok || wrapped.Description().Name != "whoami" {
		t.Fatalf("wrapper = %T description=%q", wrapped, wrapped.Description().Name)
	}
}

func TestGlazeErrorFinalizesSuccessfulRowsBeforeReturning(t *testing.T) {
	cause := apiError(http.StatusNotFound, "LaterRecordFailed")
	command := &partialGlazeCommand{CommandDescription: cmds.NewCommandDescription("partial"), err: cause}
	wrapped := WithExitCodes(command).(cmds.GlazeCommand)
	processor := &recordingProcessor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := wrapped.RunIntoGlazeProcessor(ctx, nil, processor)
	if processor.rows != 1 || !processor.closed || processor.closeContextErr != nil {
		t.Fatalf("processor rows=%d closed=%v closeCtx=%v", processor.rows, processor.closed, processor.closeContextErr)
	}
	var got *client.APIError
	if ExitCodeFor(err) != ExitNotFound || !errors.As(err, &got) || got != cause {
		t.Fatalf("returned error lost code/cause: %v", err)
	}
}

type partialGlazeCommand struct {
	*cmds.CommandDescription
	err error
}

func (c *partialGlazeCommand) RunIntoGlazeProcessor(ctx context.Context, _ *values.Values, gp middlewares.Processor) error {
	if err := gp.AddRow(ctx, types.NewRow(types.MRP("status", "accepted"))); err != nil {
		return err
	}
	return c.err
}

type recordingProcessor struct {
	rows            int
	closed          bool
	closeContextErr error
}

func (p *recordingProcessor) AddRow(context.Context, types.Row) error { p.rows++; return nil }
func (p *recordingProcessor) Close(ctx context.Context) error {
	p.closed = true
	p.closeContextErr = ctx.Err()
	return nil
}

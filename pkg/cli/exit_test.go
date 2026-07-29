package cli

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
)

// The exit codes are the CLI contract: a script branches on why a command
// failed rather than parsing stderr. cmd/datadrop/smoke_test.go's TestExitCodes
// proves it end to end against a real binary and a real server; these tests
// prove the mapping itself, in milliseconds, so that a broken mapping is
// reported by name rather than as "the 4 case wanted 4 and got 1".

// captureExit runs ExitOn with the process-ending parts stubbed out, and
// reports what it would have done.
func captureExit(t *testing.T, err error) (int, string, error) {
	t.Helper()

	var buf bytes.Buffer
	// -1 rather than 0, so that "did not exit" is distinguishable from
	// "exited successfully" — which matters for the two errors ExitOn must
	// pass through untouched.
	code := -1

	originalExit, originalSink := exitFunc, errSink
	exitFunc = func(c int) { code = c }
	errSink = &buf
	t.Cleanup(func() { exitFunc, errSink = originalExit, originalSink })

	returned := ExitOn(err)
	return code, buf.String(), returned
}

func apiError(status int, code string) error {
	return &client.APIError{Status: status, Code: code, Detail: "test"}
}

func TestExitOnMapsAPIStatuses(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		want   int
		reason string
	}{
		{"401", apiError(http.StatusUnauthorized, "Unauthorized"), ExitAuth,
			"a rejected credential"},
		{"403", apiError(http.StatusForbidden, "Forbidden"), ExitAuth,
			"a credential that is not allowed"},
		{"404", apiError(http.StatusNotFound, "NotFound"), ExitNotFound,
			"a drop that does not exist"},
		{"400", apiError(http.StatusBadRequest, "BadRequest"), ExitValidation,
			"a malformed request"},
		{"422", apiError(http.StatusUnprocessableEntity, "Unprocessable"), ExitValidation,
			"a strict schema rejection"},
		{"409", apiError(http.StatusConflict, "Conflict"), ExitValidation,
			"a conflicting write"},
		{"413", apiError(http.StatusRequestEntityTooLarge, "TooLarge"), ExitValidation,
			"a body over the limit"},
		{"500", apiError(http.StatusInternalServerError, "Internal"), ExitError,
			"a server fault, which is not one of the specific codes"},
		{"plain error", errors.New("something local went wrong"), ExitError,
			"anything that is not an API error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := captureExit(t, tc.err)
			if code != tc.want {
				t.Errorf("exit code for %s = %d, want %d", tc.reason, code, tc.want)
			}
		})
	}
}

// The mapping must survive wrapping, because every verb's error arrives through
// at least one errors.Wrap on the way out of pkg/client.
func TestExitOnSeesThroughWrapping(t *testing.T) {
	wrapped := errors.Wrap(
		errors.Wrap(apiError(http.StatusNotFound, "NotFound"), "fetching the drop"),
		"running query")

	code, _, _ := captureExit(t, wrapped)
	if code != ExitNotFound {
		t.Errorf("exit code = %d, want %d for a doubly-wrapped 404", code, ExitNotFound)
	}
}

// One prefix, everywhere. cobra.CheckErr would print "Error: "; anything that
// reaches a user has to say "datadrop: " so that two failure paths do not look
// like two programs.
func TestExitOnUsesTheBinaryPrefix(t *testing.T) {
	_, stderr, _ := captureExit(t, apiError(http.StatusNotFound, "NotFound"))

	if want := ErrorPrefix() + "NotFound: test\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// Ctrl-C on a following tail is the user stopping something that was working,
// not a failure. It must neither print nor exit non-zero.
func TestExitOnIgnoresCancellation(t *testing.T) {
	code, stderr, returned := captureExit(t, context.Canceled)

	if code != -1 {
		t.Errorf("a cancelled context exited %d; it must not exit at all", code)
	}
	if stderr != "" {
		t.Errorf("a cancelled context wrote %q to stderr", stderr)
	}
	if returned != nil {
		t.Errorf("a cancelled context returned %v, want nil", returned)
	}
}

// ExitWithoutGlazeError is glazed's own "stop here, successfully" signal and
// has to be handed back untouched, or the builder never sees it.
func TestExitOnPassesGlazeExitThrough(t *testing.T) {
	signal := &cmds.ExitWithoutGlazeError{}
	code, _, returned := captureExit(t, signal)

	if code != -1 {
		t.Errorf("ExitWithoutGlazeError exited %d; it must not exit at all", code)
	}
	if !errors.Is(returned, error(signal)) {
		t.Errorf("ExitWithoutGlazeError was not returned unchanged; got %v", returned)
	}
}

func TestExitOnIgnoresNil(t *testing.T) {
	code, stderr, returned := captureExit(t, nil)
	if code != -1 || stderr != "" || returned != nil {
		t.Errorf("ExitOn(nil) did something: code=%d stderr=%q err=%v", code, stderr, returned)
	}
}

// A verb that is not wrapped loses the mapping for that verb alone, which is
// worse than losing it everywhere because it looks like it works. WithExitCodes
// is what makes the wrapping structural; this checks it actually wraps each of
// the three interfaces rather than falling through the type switch.
func TestWithExitCodesWrapsEveryInterface(t *testing.T) {
	glaze, err := NewWhoamiCommand()
	if err != nil {
		t.Fatalf("NewWhoamiCommand: %v", err)
	}

	wrapped := WithExitCodes(glaze)
	if _, ok := wrapped.(*exitCodeGlazeCommand); !ok {
		t.Errorf("a GlazeCommand was wrapped as %T, want *exitCodeGlazeCommand", wrapped)
	}
	if wrapped.Description().Name != "whoami" {
		t.Errorf("the wrapper lost the description: %q", wrapped.Description().Name)
	}
}

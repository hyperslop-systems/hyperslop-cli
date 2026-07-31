package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"
)

func TestNewNormalizesBaseURL(t *testing.T) {
	c, err := New("http://localhost:8080/", "token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.BaseURL != "http://localhost:8080" {
		t.Fatalf("BaseURL = %q, want the trailing slash trimmed", c.BaseURL)
	}
}

func TestNewRejectsEmptyAddress(t *testing.T) {
	for _, address := range []string{"", "   "} {
		if _, err := New(address, "token"); err == nil {
			t.Errorf("New(%q) accepted an empty address", address)
		}
	}
}

func TestNewRejectsNonHTTPOrAmbiguousBaseURL(t *testing.T) {
	for _, address := range []string{
		"localhost:8080",
		"/api",
		"ftp://example.org",
		"https:///missing-host",
		"https://user@example.org",
		"https://example.org?tenant=x",
		"https://example.org/#section",
	} {
		if _, err := New(address, "token"); err == nil {
			t.Errorf("New(%q) accepted an invalid server address", address)
		}
	}
}

func TestNewConfiguresConnectionLevelTimeouts(t *testing.T) {
	c, err := New("https://example.org", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	transport, ok := c.HTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", c.HTTP.Transport)
	}
	if transport.ResponseHeaderTimeout <= 0 ||
		transport.TLSHandshakeTimeout <= 0 ||
		transport.IdleConnTimeout <= 0 ||
		transport.ExpectContinueTimeout <= 0 {
		t.Fatalf("transport timeouts are not all bounded: %+v", transport)
	}
	if c.HTTP.Timeout != 0 {
		t.Fatalf("overall timeout = %v, want zero for streaming requests", c.HTTP.Timeout)
	}
}

// The client must send the bearer token, and must not send an Authorization
// header at all when it has no token.
func TestAuthorizationHeader(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		writeJSON(w, map[string]any{"drops": []any{}})
	}))
	defer server.Close()

	withToken, err := New(server.URL, "secret-token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := withToken.ListDrops(context.Background()); err != nil {
		t.Fatalf("ListDrops: %v", err)
	}
	if seen != "Bearer secret-token" {
		t.Fatalf("Authorization = %q, want %q", seen, "Bearer secret-token")
	}

	withoutToken, err := New(server.URL, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := withoutToken.ListDrops(context.Background()); err != nil {
		t.Fatalf("ListDrops: %v", err)
	}
	if seen != "" {
		t.Fatalf("Authorization = %q, want no header when there is no token", seen)
	}
}

// A problem document must surface as a typed *APIError carrying the status, so
// the CLI can map it onto an exit code.
func TestProblemDocumentBecomesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":       "SchemaValidationFailed",
			"detail":     "payload does not satisfy schema version 1",
			"request_id": "01J2KZ",
			"errors": []map[string]string{
				{"path": "/temperature", "message": "got string, want number"},
			},
		})
	}))
	defer server.Close()

	c, err := New(server.URL, "token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.Push(context.Background(), "greenhouse", "events",
		json.RawMessage(`{"temperature":"warm"}`), false)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *APIError: %v", err, err)
	}
	if apiErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("Status = %d, want %d", apiErr.Status, http.StatusUnprocessableEntity)
	}
	if apiErr.Code != "SchemaValidationFailed" {
		t.Fatalf("Code = %q", apiErr.Code)
	}
	if len(apiErr.Errors) != 1 || apiErr.Errors[0].Path != "/temperature" {
		t.Fatalf("Errors = %+v, want one violation at /temperature", apiErr.Errors)
	}
	// The rendered message should include both the detail and the field paths,
	// since that is what a CLI user sees.
	if message := apiErr.Error(); !strings.Contains(message, "/temperature") {
		t.Fatalf("Error() = %q, missing the field path", message)
	}
}

// A non-JSON error body (a proxy error page, say) must still produce a usable
// error rather than a decode failure.
func TestNonProblemErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer server.Close()

	c, err := New(server.URL, "token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.ListDrops(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *APIError", err)
	}
	if apiErr.Status != http.StatusBadGateway {
		t.Fatalf("Status = %d, want %d", apiErr.Status, http.StatusBadGateway)
	}
	if apiErr.Code == "" {
		t.Fatal("Code is empty; it should fall back to the status text")
	}
}

// Push must declare the shape explicitly rather than relying on the server's
// specversion heuristic.
func TestPushSetsModeExplicitly(t *testing.T) {
	var seen url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query()
		writeJSON(w, datalab.AppendResult{ID: "x", Seq: 1})
	}))
	defer server.Close()

	c, err := New(server.URL, "token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := c.Push(context.Background(), "greenhouse", "events",
		json.RawMessage(`{"n":1}`), false); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if seen.Get("mode") != "simple" {
		t.Fatalf("mode = %q, want \"simple\"", seen.Get("mode"))
	}

	if _, err := c.Push(context.Background(), "greenhouse", "events",
		json.RawMessage(`{"specversion":"1.0","data":{}}`), true); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if seen.Get("mode") != "envelope" {
		t.Fatalf("mode = %q, want \"envelope\"", seen.Get("mode"))
	}
}

func TestPushDerivesDuplicateFromHTTPStatus(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		duplicate bool
	}{
		{"new append", http.StatusCreated, false},
		{"idempotent replay", http.StatusOK, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				writeJSON(w, datalab.AppendResult{ID: "event-1", Seq: 7})
			}))
			defer server.Close()

			c, err := New(server.URL, "token")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			result, err := c.Push(context.Background(), "greenhouse", "events",
				json.RawMessage(`{"temperature":21.7}`), false)
			if err != nil {
				t.Fatalf("Push: %v", err)
			}
			if result.Duplicate != tc.duplicate {
				t.Fatalf("Duplicate = %v, want %v for status %d", result.Duplicate, tc.duplicate, tc.status)
			}
		})
	}
}

func TestDeleteDatasetVersionReturnsConcreteVersionForLatest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/versions/latest") {
			t.Errorf("delete path = %q, want latest alias", r.URL.Path)
		}
		writeJSON(w, datalab.DeleteDatasetVersionResult{
			Drop: "greenhouse", Dataset: "readings", Version: 7, Deleted: true,
		})
	}))
	defer server.Close()

	c, err := New(server.URL, "token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	deleted, err := c.DeleteDatasetVersion(context.Background(), "greenhouse", "readings", datalab.LatestVersion)
	if err != nil {
		t.Fatalf("DeleteDatasetVersion: %v", err)
	}
	if deleted.Version != 7 || !deleted.Deleted {
		t.Fatalf("deleted = %+v, want concrete version 7", deleted)
	}
}

// The default stream is omitted from the query string, so request URLs stay
// readable for the common case.
func TestQueryValuesOmitsDefaults(t *testing.T) {
	values := queryValues(datalab.EventQuery{
		Drop:   "greenhouse",
		Stream: datalab.DefaultStream,
	})
	for _, key := range []string{"stream", "limit", "after", "before", "from", "to", "time_field"} {
		if values.Has(key) {
			t.Errorf("query includes %q for an all-default query: %v", key, values)
		}
	}
}

func TestQueryValuesEncodesEverySet(t *testing.T) {
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

	values := queryValues(datalab.EventQuery{
		Drop:      "greenhouse",
		Stream:    "alerts",
		Limit:     25,
		After:     18440,
		Before:    0,
		From:      from,
		To:        to,
		Order:     datalab.OrderAsc,
		TimeField: datalab.TimeFieldReceivedAt,
	})

	for key, want := range map[string]string{
		"stream":     "alerts",
		"limit":      "25",
		"after":      "18440",
		"order":      "asc",
		"time_field": "received_at",
	} {
		if got := values.Get(key); got != want {
			t.Errorf("query[%q] = %q, want %q", key, got, want)
		}
	}

	// Bounds must round-trip as parseable RFC3339.
	for key, want := range map[string]time.Time{"from": from, "to": to} {
		parsed, err := time.Parse(time.RFC3339Nano, values.Get(key))
		if err != nil {
			t.Fatalf("query[%q] = %q, not RFC3339: %v", key, values.Get(key), err)
		}
		if !parsed.Equal(want) {
			t.Fatalf("query[%q] = %v, want %v", key, parsed, want)
		}
	}
}

// The SSE parser has to handle append frames, reset frames, heartbeat comments,
// and multi-line data — all of which the server emits.
func TestParseSSE(t *testing.T) {
	body := strings.Join([]string{
		"retry: 3000",
		"",
		": heartbeat 0",
		"",
		"event: append",
		"id: 1",
		`data: {"seq":1,"data":{"n":1}}`,
		"",
		": heartbeat 1",
		"",
		"event: append",
		"id: 2",
		`data: {"seq":2,"data":{"n":2}}`,
		"",
		"event: reset",
		`data: {"reason":"slow_consumer","cursor":2}`,
		"",
	}, "\n")

	frames := make(chan StreamEvent, 8)
	if err := parseSSE(context.Background(), strings.NewReader(body), frames); err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	close(frames)

	var collected []StreamEvent
	for frame := range frames {
		collected = append(collected, frame)
	}

	if len(collected) != 3 {
		t.Fatalf("parsed %d frames, want 3 (heartbeats must be skipped): %+v", len(collected), collected)
	}
	if collected[0].Name != "append" || collected[0].Envelope.Seq != 1 {
		t.Fatalf("frame 0 = %+v, want append seq 1", collected[0])
	}
	if collected[1].Envelope.Seq != 2 {
		t.Fatalf("frame 1 seq = %d, want 2", collected[1].Envelope.Seq)
	}
	if collected[2].Name != "reset" {
		t.Fatalf("frame 2 name = %q, want \"reset\"", collected[2].Name)
	}
	if collected[2].Reset.Reason != "slow_consumer" || collected[2].Reset.Cursor != 2 {
		t.Fatalf("reset frame = %+v", collected[2].Reset)
	}
}

func TestParseSSETypifiesReadErrorsButNotDecodeErrors(t *testing.T) {
	readFailure := io.MultiReader(strings.NewReader(": heartbeat\n\n"), iotest.ErrReader(io.ErrUnexpectedEOF))
	err := parseSSE(context.Background(), readFailure, make(chan StreamEvent, 1))
	var streamReadErr *StreamReadError
	if !errors.As(err, &streamReadErr) {
		t.Fatalf("read error = %T %v, want *StreamReadError", err, err)
	}

	decodeFailure := strings.NewReader("event: append\ndata: not-json\n\n")
	err = parseSSE(context.Background(), decodeFailure, make(chan StreamEvent, 1))
	if err == nil {
		t.Fatal("parseSSE accepted malformed frame JSON")
	}
	if errors.As(err, &streamReadErr) {
		t.Fatalf("decode error was classified as reconnectable: %v", err)
	}
}

func TestParseSSEStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	body := "event: append\ndata: {\"seq\":1}\n\n"
	frames := make(chan StreamEvent) // unbuffered: the send will block

	err := parseSSE(ctx, strings.NewReader(body), frames)
	if err == nil {
		t.Fatal("parseSSE ignored a cancelled context")
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

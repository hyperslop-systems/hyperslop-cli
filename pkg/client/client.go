// Package client is a typed HTTP client for the datadrop API.
//
// The CLI is built on this rather than reaching into SQLite directly, which is
// what keeps the CLI honest: if `datadrop push` works, `curl` works, because
// they exercise the same endpoints.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

// Client talks to one datadrop server.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New returns a client for baseURL. An empty token means no Authorization
// header is sent, which only works against a server started without --token
// or against a public_read drop.
func New(baseURL, token string) (*Client, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return nil, errors.New("client: server address is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, errors.Wrapf(err, "client: invalid server address %q", baseURL)
	}
	if !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.Errorf(
			"client: server address %q must be an absolute http or https URL with a host", baseURL)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.Errorf(
			"client: server address %q must not contain user info, a query, or a fragment", baseURL)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ExpectContinueTimeout = time.Second
	transport.IdleConnTimeout = 90 * time.Second

	return &Client{
		BaseURL: trimmed,
		Token:   token,
		// No overall timeout: Stream and large downloads are long-lived by
		// design. Connection establishment and response headers are still
		// bounded by the transport; request lifetime comes from ctx.
		HTTP: &http.Client{Transport: transport},
	}, nil
}

// APIError is a problem document returned by the server.
type APIError struct {
	Status    int                  `json:"-"`
	Code      string               `json:"code"`
	Detail    string               `json:"detail"`
	RequestID string               `json:"request_id,omitempty"`
	Errors    []datadrop.Violation `json:"errors,omitempty"`
	// RetryAfter is parsed from the HTTP Retry-After header when present, e.g.
	// on a device-token "RateLimited" response. Zero means the server gave no
	// guidance.
	RetryAfter time.Duration `json:"-"`
}

func (e *APIError) Error() string {
	var sb strings.Builder
	sb.WriteString(e.Code)
	if e.Detail != "" {
		sb.WriteString(": ")
		sb.WriteString(e.Detail)
	}
	for _, v := range e.Errors {
		sb.WriteString("\n  ")
		if v.Path != "" {
			sb.WriteString(v.Path)
			sb.WriteString(": ")
		}
		sb.WriteString(v.Message)
	}
	return sb.String()
}

// CreateDrop creates a drop.
func (c *Client) CreateDrop(ctx context.Context, req datadrop.CreateDropRequest) (datadrop.Drop, error) {
	var created datadrop.Drop
	err := c.doJSON(ctx, http.MethodPost, "/v1/drops", nil, req, &created)
	return created, err
}

// ListDrops returns every drop on the server.
func (c *Client) ListDrops(ctx context.Context) ([]datadrop.Drop, error) {
	var response struct {
		Drops []datadrop.Drop `json:"drops"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/v1/drops", nil, nil, &response)
	return response.Drops, err
}

// GetDrop returns a drop with its counters.
func (c *Client) GetDrop(ctx context.Context, drop string) (datadrop.DropStats, error) {
	var stats datadrop.DropStats
	err := c.doJSON(ctx, http.MethodGet, "/v1/drops/"+url.PathEscape(drop), nil, nil, &stats)
	return stats, err
}

// Push appends one event. body is sent verbatim: either a bare payload or a
// full CloudEvents envelope, discriminated by envelope.
func (c *Client) Push(
	ctx context.Context, drop, stream string, body json.RawMessage, envelope bool,
) (datadrop.AppendResult, error) {
	query := url.Values{}
	if stream != "" && stream != datadrop.DefaultStream {
		query.Set("stream", stream)
	}
	// Be explicit about the shape rather than relying on the server's
	// specversion heuristic.
	if envelope {
		query.Set("mode", "envelope")
	} else {
		query.Set("mode", "simple")
	}

	resp, err := c.do(ctx, http.MethodPost,
		"/v1/drops/"+url.PathEscape(drop)+"/events", query, body)
	if err != nil {
		return datadrop.AppendResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result datadrop.AppendResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return datadrop.AppendResult{}, errors.Wrap(err, "client: decode append result")
	}
	// Duplicate is intentionally absent from JSON: the ingest wire contract
	// distinguishes an idempotent replay (200) from a new append (201).
	result.Duplicate = resp.StatusCode == http.StatusOK
	return result, nil
}

// Query reads a window of a stream.
func (c *Client) Query(ctx context.Context, q datadrop.EventQuery) (datadrop.QueryResult, error) {
	var result datadrop.QueryResult
	err := c.doJSON(ctx, http.MethodGet,
		"/v1/drops/"+url.PathEscape(q.Drop)+"/events", queryValues(q), nil, &result)
	return result, err
}

// PutSchema registers a schema version.
func (c *Client) PutSchema(
	ctx context.Context, drop, stream string, spec json.RawMessage, mode datadrop.Mode,
) (datadrop.PutSchemaResult, error) {
	query := url.Values{}
	if mode != "" {
		query.Set("mode", string(mode))
	}

	var result datadrop.PutSchemaResult
	err := c.doJSON(ctx, http.MethodPut,
		"/v1/drops/"+url.PathEscape(drop)+"/schemas/"+url.PathEscape(stream),
		query, spec, &result)
	return result, err
}

// GetSchema returns the active schema for a stream.
func (c *Client) GetSchema(ctx context.Context, drop, stream string) (datadrop.Schema, error) {
	var sc datadrop.Schema
	err := c.doJSON(ctx, http.MethodGet,
		"/v1/drops/"+url.PathEscape(drop)+"/schemas/"+url.PathEscape(stream), nil, nil, &sc)
	return sc, err
}

// Export streams an export in the requested format. The caller owns the
// returned reader and must close it.
func (c *Client) Export(
	ctx context.Context, drop, format string, q datadrop.EventQuery,
) (io.ReadCloser, error) {
	query := queryValues(q)
	query.Set("format", format)

	resp, err := c.do(ctx, http.MethodGet,
		"/v1/drops/"+url.PathEscape(drop)+"/export", query, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// StreamEvent is one frame from the live feed.
type StreamEvent struct {
	// Name is the SSE event type: "append" or "reset".
	Name string
	// Envelope is populated for "append" frames.
	Envelope datadrop.Envelope
	// Reset carries the server's cursor when the subscriber was evicted.
	Reset struct {
		Reason string `json:"reason"`
		Cursor int64  `json:"cursor"`
	}
}

// Stream subscribes to a drop's live feed, replaying everything after the
// given sequence first.
//
// The returned channel closes when ctx is cancelled or the connection ends.
// Reconnection is the caller's business: it knows the last sequence it
// processed, which is the only state a resume needs.
func (c *Client) Stream(
	ctx context.Context, drop, stream string, after int64,
) (<-chan StreamEvent, <-chan error, error) {
	query := url.Values{}
	if stream != "" && stream != datadrop.DefaultStream {
		query.Set("stream", stream)
	}
	if after > 0 {
		query.Set("after", strconv.FormatInt(after, 10))
	}

	resp, err := c.do(ctx, http.MethodGet,
		"/v1/drops/"+url.PathEscape(drop)+"/events/stream", query, nil)
	if err != nil {
		return nil, nil, err
	}

	frames := make(chan StreamEvent)
	errs := make(chan error, 1)

	go func() {
		defer close(frames)
		defer close(errs)
		defer func() { _ = resp.Body.Close() }()

		if err := parseSSE(ctx, resp.Body, frames); err != nil && !errors.Is(err, context.Canceled) {
			errs <- err
		}
	}()

	return frames, errs, nil
}

// parseSSE decodes a text/event-stream body.
//
// SSE is line-oriented and small enough to parse directly: accumulate
// `event:`, `id:` and `data:` lines, dispatch on a blank line, ignore comment
// lines starting with ':' (which is what heartbeats are).
func parseSSE(ctx context.Context, body io.Reader, frames chan<- StreamEvent) error {
	scanner := bufio.NewScanner(body)
	// SSE frames carry a full event envelope, which can exceed the default
	// 64 KiB token limit.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		name string
		data strings.Builder
	)

	dispatch := func() error {
		if data.Len() == 0 {
			name = ""
			return nil
		}
		payload := data.String()
		data.Reset()

		frame := StreamEvent{Name: name}
		name = ""

		switch frame.Name {
		case "reset":
			if err := json.Unmarshal([]byte(payload), &frame.Reset); err != nil {
				return errors.Wrap(err, "client: decode reset frame")
			}
		default:
			if err := json.Unmarshal([]byte(payload), &frame.Envelope); err != nil {
				return errors.Wrap(err, "client: decode event frame")
			}
		}

		select {
		case frames <- frame:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case line == "":
			if err := dispatch(); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
			// Comment / heartbeat.
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		default:
			// id:, retry:, and any future field: ignored.
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return nil
		}
		return errors.Wrap(err, "client: read event stream")
	}
	return dispatch()
}

// queryValues renders an EventQuery as URL parameters, omitting anything left
// at its default so the request URL stays readable.
func queryValues(q datadrop.EventQuery) url.Values {
	values := url.Values{}

	if q.Stream != "" && q.Stream != datadrop.DefaultStream {
		values.Set("stream", q.Stream)
	}
	if q.Limit > 0 {
		values.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.After > 0 {
		values.Set("after", strconv.FormatInt(q.After, 10))
	}
	if q.Before > 0 {
		values.Set("before", strconv.FormatInt(q.Before, 10))
	}
	if !q.From.IsZero() {
		values.Set("from", q.From.UTC().Format(time.RFC3339Nano))
	}
	if !q.To.IsZero() {
		values.Set("to", q.To.UTC().Format(time.RFC3339Nano))
	}
	if q.Order != "" {
		values.Set("order", string(q.Order))
	}
	if q.TimeField != "" && q.TimeField != datadrop.TimeFieldTime {
		values.Set("time_field", string(q.TimeField))
	}
	return values
}

// doJSON performs a request and decodes a JSON response into out.
func (c *Client) doJSON(
	ctx context.Context, method, path string, query url.Values, body, out any,
) error {
	var encoded []byte
	if body != nil {
		switch typed := body.(type) {
		case json.RawMessage:
			encoded = typed
		default:
			marshalled, err := json.Marshal(body)
			if err != nil {
				return errors.Wrap(err, "client: encode request body")
			}
			encoded = marshalled
		}
	}

	resp, err := c.do(ctx, method, path, query, encoded)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return errors.Wrap(json.NewDecoder(resp.Body).Decode(out), "client: decode response")
}

// do performs a request and converts a non-2xx response into an *APIError.
//
// The response body is left open on success so streaming callers can consume
// it; it is always closed on the error path.
func (c *Client) do(
	ctx context.Context, method, path string, query url.Values, body []byte,
) (*http.Response, error) {
	endpoint := c.BaseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, errors.Wrapf(err, "client: build %s %s", method, path)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "client: %s %s", method, path)
	}

	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return nil, apiErrorFrom(resp)
	}

	return resp, nil
}

// StartDeviceAuthorization begins a browser-approved device pairing flow.
//
// It is unauthenticated: the whole point of the device flow is to obtain a
// token, so the client sends no Authorization header (do() omits it when
// Token is empty). On a non-2xx response it returns an *APIError.
func (c *Client) StartDeviceAuthorization(
	ctx context.Context, req datadrop.StartDeviceAuthorizationRequest,
) (datadrop.StartDeviceAuthorizationResponse, error) {
	var resp datadrop.StartDeviceAuthorizationResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/device/authorizations", nil, req, &resp)
	return resp, err
}

// PollDeviceToken polls a device pairing for its approved token.
//
// On a pending, slow-down, rate-limited or expired authorization it returns an
// *APIError whose Code is one of "AuthorizationPending", "SlowDown",
// "RateLimited" (with RetryAfter set from the Retry-After header) or
// "ExpiredToken". The caller owns the polling loop and the wait interval: it
// knows the last state, and the only resume state a retry needs is "poll again
// after interval".
func (c *Client) PollDeviceToken(
	ctx context.Context, req datadrop.PollDeviceTokenRequest,
) (datadrop.DeviceTokenResponse, error) {
	var resp datadrop.DeviceTokenResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/device/tokens", nil, req, &resp)
	return resp, err
}

// parseRetryAfter turns an HTTP Retry-After header (delta-seconds form) into a
// duration. Returns 0 for an absent or malformed value.
func parseRetryAfter(value string) time.Duration {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

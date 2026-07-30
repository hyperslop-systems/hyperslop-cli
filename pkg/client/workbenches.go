package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	workbenchv1 "github.com/hyperslop-systems/pbui/gen/go/hyperslop/pbui/workbench/v1"
	"github.com/hyperslop-systems/pbui/pkg/workbenchapi"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
)

// ListWorkbenches returns the current user's workbench summaries.
func (c *Client) ListWorkbenches(
	ctx context.Context,
) ([]*workbenchv1.WorkbenchSummary, error) {
	var response workbenchv1.ListWorkbenchesResponse
	if err := c.doWorkbenchJSON(
		ctx, http.MethodGet, "/v1/workbenches", nil, nil, "", &response,
	); err != nil {
		return nil, err
	}
	return response.Workbenches, nil
}

// GetWorkbench returns one current snapshot.
func (c *Client) GetWorkbench(
	ctx context.Context,
	workbenchID string,
) (*workbenchv1.WorkbenchResource, error) {
	var resource workbenchv1.WorkbenchResource
	err := c.doWorkbenchJSON(
		ctx, http.MethodGet, workbenchPath(workbenchID), nil, nil, "", &resource,
	)
	return &resource, err
}

// CreateWorkbench creates a snapshot at revision one.
func (c *Client) CreateWorkbench(
	ctx context.Context,
	document *workbenchv1.WorkbenchDocument,
	requestID string,
) (*workbenchv1.WorkbenchResource, error) {
	requestID, err := ensureRequestID(requestID)
	if err != nil {
		return nil, err
	}
	var resource workbenchv1.WorkbenchResource
	err = c.doWorkbenchJSON(
		ctx,
		http.MethodPost,
		"/v1/workbenches",
		&workbenchv1.CreateWorkbenchRequest{Workbench: document},
		nil,
		requestID,
		&resource,
	)
	return &resource, err
}

// ReplaceWorkbench conditionally replaces a complete snapshot.
func (c *Client) ReplaceWorkbench(
	ctx context.Context,
	workbenchID string,
	expectedRevision uint64,
	document *workbenchv1.WorkbenchDocument,
	requestID string,
) (*workbenchv1.WorkbenchResource, error) {
	requestID, err := ensureRequestID(requestID)
	if err != nil {
		return nil, err
	}
	var resource workbenchv1.WorkbenchResource
	err = c.doWorkbenchJSON(
		ctx,
		http.MethodPut,
		workbenchPath(workbenchID),
		document,
		map[string]string{"If-Match": WorkbenchETag(workbenchID, expectedRevision)},
		requestID,
		&resource,
	)
	return &resource, err
}

// MutateWorkbench conditionally applies one typed mutation batch.
func (c *Client) MutateWorkbench(
	ctx context.Context,
	workbenchID string,
	expectedRevision uint64,
	batch *workbenchv1.MutationBatch,
	requestID string,
) (*workbenchv1.WorkbenchResource, error) {
	requestID, err := ensureRequestID(requestID)
	if err != nil {
		return nil, err
	}
	var resource workbenchv1.WorkbenchResource
	err = c.doWorkbenchJSON(
		ctx,
		http.MethodPost,
		workbenchPath(workbenchID)+"/mutate",
		batch,
		map[string]string{"If-Match": WorkbenchETag(workbenchID, expectedRevision)},
		requestID,
		&resource,
	)
	return &resource, err
}

// DeleteWorkbench conditionally deletes one snapshot.
func (c *Client) DeleteWorkbench(
	ctx context.Context,
	workbenchID string,
	expectedRevision uint64,
) error {
	resp, err := c.doWithHeaders(
		ctx,
		http.MethodDelete,
		workbenchPath(workbenchID),
		nil,
		nil,
		map[string]string{"If-Match": WorkbenchETag(workbenchID, expectedRevision)},
	)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// WorkbenchUpdatedEvent is one workbench SSE invalidation.
type WorkbenchUpdatedEvent struct {
	WorkbenchID string
	Revision    uint64
}

// StreamWorkbench subscribes to revision invalidations after a known revision.
func (c *Client) StreamWorkbench(
	ctx context.Context,
	workbenchID string,
	after uint64,
) (<-chan WorkbenchUpdatedEvent, <-chan error, error) {
	query := url.Values{}
	if after > 0 {
		query.Set("after", strconv.FormatUint(after, 10))
	}
	resp, err := c.do(
		ctx, http.MethodGet, workbenchPath(workbenchID)+"/stream", query, nil,
	)
	if err != nil {
		return nil, nil, err
	}
	events := make(chan WorkbenchUpdatedEvent)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		defer func() { _ = resp.Body.Close() }()
		if err := parseWorkbenchSSE(ctx, resp.Body, events); err != nil &&
			!errors.Is(err, context.Canceled) {
			errs <- err
		}
	}()
	return events, errs, nil
}

// WorkbenchETag returns the strong revision precondition used by write routes.
func WorkbenchETag(workbenchID string, revision uint64) string {
	return `"workbench-` + workbenchID + `-` + strconv.FormatUint(revision, 10) + `"`
}

func workbenchPath(workbenchID string) string {
	return "/v1/workbenches/" + url.PathEscape(workbenchID)
}

func ensureRequestID(requestID string) (string, error) {
	if value := strings.TrimSpace(requestID); value != "" {
		return value, nil
	}
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", errors.Wrap(err, "client: generate request ID")
	}
	return hex.EncodeToString(data[:]), nil
}

func (c *Client) doWorkbenchJSON(
	ctx context.Context,
	method string,
	path string,
	body proto.Message,
	headers map[string]string,
	requestID string,
	out proto.Message,
) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = workbenchapi.Marshal(body)
		if err != nil {
			return err
		}
	}
	if requestID != "" {
		if headers == nil {
			headers = map[string]string{}
		}
		headers["Idempotency-Key"] = requestID
	}
	resp, err := c.doWithHeaders(ctx, method, path, nil, encoded, headers)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "client: read workbench response")
	}
	if out == nil {
		return nil
	}
	return workbenchapi.Unmarshal(data, out)
}

func (c *Client) doWithHeaders(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body []byte,
	headers map[string]string,
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
	for name, value := range headers {
		req.Header.Set(name, value)
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

func parseWorkbenchSSE(
	ctx context.Context,
	body io.Reader,
	events chan<- WorkbenchUpdatedEvent,
) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 16*1024), 1<<20)
	var name string
	var data strings.Builder
	dispatch := func() error {
		if data.Len() == 0 {
			name = ""
			return nil
		}
		payload := data.String()
		data.Reset()
		eventName := name
		name = ""
		if eventName != "workbench.updated" {
			return nil
		}
		var message workbenchv1.WorkbenchUpdatedEvent
		if err := workbenchapi.Unmarshal([]byte(payload), &message); err != nil {
			return err
		}
		select {
		case events <- WorkbenchUpdatedEvent{
			WorkbenchID: message.WorkbenchId,
			Revision:    message.Revision,
		}:
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
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return &StreamReadError{Err: errors.Wrap(err, "client: read workbench stream")}
	}
	return dispatch()
}

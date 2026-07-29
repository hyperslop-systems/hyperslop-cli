package events

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

func TestTailFollowRejectsRangeFiltersItCannotPreserve(t *testing.T) {
	for name, settings := range map[string]rangeSettings{
		"after":  {After: 7},
		"before": {Before: 10},
		"from":   {From: "2026-07-29T00:00:00Z"},
		"to":     {To: "2026-07-30T00:00:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			s := &tailSettings{Follow: true, rangeSettings: settings}
			if err := s.validateFollowRange(); err == nil {
				t.Fatalf("validateFollowRange accepted --follow with %s", name)
			}
		})
	}
	if err := (&tailSettings{Follow: true}).validateFollowRange(); err != nil {
		t.Fatalf("bare --follow rejected: %v", err)
	}
	if err := (&tailSettings{Follow: false, rangeSettings: rangeSettings{Before: 10}}).validateFollowRange(); err != nil {
		t.Fatalf("bounded non-follow tail rejected: %v", err)
	}
}

func TestTailQueryForcesDescendingOrderBeforeCursorValidation(t *testing.T) {
	t.Run("after cursor is rejected locally", func(t *testing.T) {
		s := &tailSettings{
			Drop: "greenhouse",
			rangeSettings: rangeSettings{
				After: 10, Order: string(datadrop.OrderAsc), Limit: tailDefaultLimit,
			},
		}
		if _, err := s.tailQuery(); err == nil {
			t.Fatal("tailQuery accepted an after cursor after forcing descending order")
		}
	})

	t.Run("before cursor is valid descending", func(t *testing.T) {
		s := &tailSettings{
			Drop: "greenhouse",
			rangeSettings: rangeSettings{
				Before: 10, Order: string(datadrop.OrderAsc), Limit: tailDefaultLimit,
			},
		}
		q, err := s.tailQuery()
		if err != nil {
			t.Fatalf("tailQuery: %v", err)
		}
		if q.Order != datadrop.OrderDesc || q.Before != 10 {
			t.Fatalf("query = %+v, want descending before=10", q)
		}
	})
}

func TestFollowStreamReconnectsAfterCleanEOFWithCursor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu       sync.Mutex
		afters   []string
		requests int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		afters = append(afters, r.URL.Query().Get("after"))
		requestNumber := requests
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// Both responses end cleanly without an SSE reset. The first must cause
		// a reconnect; the second cancels the command so the test terminates.
		if requestNumber == 2 {
			cancel()
		}
	}))
	defer server.Close()

	api, err := client.New(server.URL, "")
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	var waits []time.Duration
	err = followStreamWithWait(ctx, nil, api, "greenhouse", datadrop.DefaultStream, 7,
		func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		})
	if err != nil {
		t.Fatalf("followStreamWithWait: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Fatalf("stream requests = %d, want 2", requests)
	}
	wantAfter := strconv.FormatInt(7, 10)
	for i, after := range afters {
		if after != wantAfter {
			t.Errorf("request %d after = %q, want %q", i+1, after, wantAfter)
		}
	}
	if len(waits) != 1 || waits[0] != streamReconnectInitial {
		t.Fatalf("reconnect waits = %v, want [%v]", waits, streamReconnectInitial)
	}
}

func TestNextReconnectDelayIsBounded(t *testing.T) {
	if got := nextReconnectDelay(time.Second); got != 2*time.Second {
		t.Fatalf("nextReconnectDelay(1s) = %v, want 2s", got)
	}
	if got := nextReconnectDelay(streamReconnectMaximum); got != streamReconnectMaximum {
		t.Fatalf("nextReconnectDelay(max) = %v, want %v", got, streamReconnectMaximum)
	}
}

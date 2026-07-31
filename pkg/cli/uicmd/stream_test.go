package uicmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-go-golems/glazed/pkg/types"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
)

func TestFollowWorkbenchStreamReconnectsFromLastRevision(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var afters []string
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		requestNumber := requests
		afters = append(afters, r.URL.Query().Get("after"))
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		if requestNumber == 1 {
			_, _ = io.WriteString(w, "event: workbench.updated\nid: 8\ndata: {\"workbenchId\":\"bench-1\",\"revision\":\"8\"}\n\n")
			return
		}
		cancel()
	}))
	defer server.Close()

	api, err := client.New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	processor := &workbenchStreamProcessor{}
	var waits []time.Duration
	err = followWorkbenchStreamWithWait(
		ctx, processor, api, "bench-1", 7,
		func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("followWorkbenchStreamWithWait: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if want := []string{"7", "8"}; len(afters) != len(want) ||
		afters[0] != want[0] || afters[1] != want[1] {
		t.Fatalf("after cursors = %v, want %v", afters, want)
	}
	if len(waits) != 1 || waits[0] != workbenchReconnectInitial {
		t.Fatalf("reconnect waits = %v, want [%v]", waits, workbenchReconnectInitial)
	}
	if len(processor.rows) != 1 {
		t.Fatalf("rows = %#v, want revision 8", processor.rows)
	}
	revision, ok := processor.rows[0].Get("revision")
	if !ok || revision != strconv.FormatUint(8, 10) {
		t.Fatalf("revision = %#v (present=%v), want 8", revision, ok)
	}
}

type workbenchStreamProcessor struct {
	rows []types.Row
}

func (p *workbenchStreamProcessor) AddRow(_ context.Context, row types.Row) error {
	p.rows = append(p.rows, row)
	return nil
}

func (p *workbenchStreamProcessor) Close(context.Context) error { return nil }

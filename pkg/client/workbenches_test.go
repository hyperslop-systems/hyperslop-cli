package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workbenchv1 "github.com/hyperslop-systems/pbui/gen/go/hyperslop/pbui/workbench/v1"
	"github.com/hyperslop-systems/pbui/pkg/workbenchapi"
	"google.golang.org/protobuf/proto"
)

func writeWorkbenchProto(t *testing.T, w http.ResponseWriter, message proto.Message) {
	t.Helper()
	data, err := workbenchapi.Marshal(message)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func TestMutateWorkbenchSendsRevisionAndIdempotencyHeaders(t *testing.T) {
	var seenBatch workbenchv1.MutationBatch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/workbenches/bench/mutate" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("If-Match"); got != WorkbenchETag("bench", 7) {
			t.Errorf("If-Match = %q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "request-7" {
			t.Errorf("Idempotency-Key = %q", got)
		}
		if err := workbenchapi.Decode(r.Body, 1<<20, &seenBatch); err != nil {
			t.Errorf("decode batch: %v", err)
		}
		writeWorkbenchProto(t, w, &workbenchv1.WorkbenchResource{
			Workbench: &workbenchv1.WorkbenchDocument{Id: "bench"},
			Revision:  8,
		})
	}))
	defer server.Close()

	c, err := New(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	batch := &workbenchv1.MutationBatch{Mutations: []*workbenchv1.Mutation{{
		Body: &workbenchv1.Mutation_WorkbenchRename{
			WorkbenchRename: &workbenchv1.WorkbenchRename{Name: "New name"},
		},
	}}}
	resource, err := c.MutateWorkbench(
		context.Background(), "bench", 7, batch, "request-7",
	)
	if err != nil {
		t.Fatalf("MutateWorkbench: %v", err)
	}
	if resource.Revision != 8 || len(seenBatch.Mutations) != 1 {
		t.Fatalf("resource = %+v, batch = %+v", resource, &seenBatch)
	}
}

func TestCreateWorkbenchGeneratesRequestID(t *testing.T) {
	var requestID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID = r.Header.Get("Idempotency-Key")
		writeWorkbenchProto(t, w, &workbenchv1.WorkbenchResource{
			Workbench: &workbenchv1.WorkbenchDocument{Id: "bench"},
			Revision:  1,
		})
	}))
	defer server.Close()
	c, err := New(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateWorkbench(
		context.Background(), &workbenchv1.WorkbenchDocument{Id: "bench"}, "",
	); err != nil {
		t.Fatalf("CreateWorkbench: %v", err)
	}
	if len(requestID) != 32 {
		t.Fatalf("generated request ID = %q, want 32 hex characters", requestID)
	}
}

func TestParseWorkbenchSSE(t *testing.T) {
	message, err := workbenchapi.Marshal(&workbenchv1.WorkbenchUpdatedEvent{
		WorkbenchId: "bench",
		Revision:    9007199254740993,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := fmt.Sprintf(
		": heartbeat\n\nevent: workbench.updated\nid: 9007199254740993\ndata: %s\n\n",
		message,
	)
	events := make(chan WorkbenchUpdatedEvent, 1)
	if err := parseWorkbenchSSE(context.Background(), strings.NewReader(input), events); err != nil {
		t.Fatalf("parseWorkbenchSSE: %v", err)
	}
	close(events)
	event := <-events
	if event.WorkbenchID != "bench" || event.Revision != 9007199254740993 {
		t.Fatalf("event = %+v", event)
	}
}

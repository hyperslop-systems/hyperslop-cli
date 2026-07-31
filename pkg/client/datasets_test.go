package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"
)

func TestImportDatasetRejectsNegativeRowsBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	api, err := New(server.URL, "token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := api.ImportDataset(context.Background(), "drop", "dataset", "latest",
		"rows.csv", "events", "csv", -1, false); err == nil {
		t.Fatal("ImportDataset accepted a negative row limit")
	}
	if requests != 0 {
		t.Fatalf("negative row limit sent %d requests, want none", requests)
	}
}

func TestGarbageCollectRejectsNegativeAgeBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	api, err := New(server.URL, "token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := api.GarbageCollect(context.Background(), -1); err == nil {
		t.Fatal("GarbageCollect accepted a negative age")
	}
	if requests != 0 {
		t.Fatalf("negative age sent %d requests, want none", requests)
	}
}

func TestSnapshotFileHonorsCancellation(t *testing.T) {
	localPath := filepath.Join(t.TempDir(), "large.bin")
	if err := os.WriteFile(localPath, []byte("bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := snapshotFile(ctx, localPath); !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshotFile error = %v, want context cancellation", err)
	}
}

func TestPushDatasetMountsAStableSnapshotWhenSourceChanges(t *testing.T) {
	localPath := filepath.Join(t.TempDir(), "reading.txt")
	if err := os.WriteFile(localPath, []byte("initial bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	expectedDigest, _, err := HashFile(localPath)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	mountedDigest := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/versions"):
			_ = json.NewEncoder(w).Encode(datalab.DatasetVersion{Version: 1})
		case r.Method == http.MethodHead:
			// Mutate the original after the client has captured and hashed its
			// snapshot but before it mounts the cache hit.
			if err := os.WriteFile(localPath, []byte("later bytes"), 0o600); err != nil {
				t.Errorf("mutate source: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut:
			mountedDigest = r.URL.Query().Get("digest")
			_ = json.NewEncoder(w).Encode(datalab.UploadFileResult{Digest: mountedDigest})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/commit"):
			_ = json.NewEncoder(w).Encode(datalab.DatasetVersion{Version: 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	api, err := New(server.URL, "token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := api.PushDataset(context.Background(), "greenhouse", "readings",
		[]PushFile{{LocalPath: localPath, LogicalPath: "reading.txt"}}, datalab.CommitVersionRequest{})
	if err != nil {
		t.Fatalf("PushDataset: %v", err)
	}
	if result.Mounted != 1 || mountedDigest != expectedDigest {
		t.Fatalf("result=%+v mounted digest=%q, want snapshot %q", result, mountedDigest, expectedDigest)
	}
}

func TestDatasetUploadAndMountUseLogicalPathMediaType(t *testing.T) {
	var mediaTypes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaTypes = append(mediaTypes, r.Header.Get("Content-Type"))
		_ = json.NewEncoder(w).Encode(datalab.UploadFileResult{
			Path: "rows.csv", Digest: "sha256:test", SizeBytes: 4,
		})
	}))
	defer server.Close()

	api, err := New(server.URL, "token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	localPath := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(localPath, []byte("a\n1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := api.UploadDatasetFile(context.Background(), "greenhouse", "readings", 1,
		"rows.csv", localPath, "sha256:test"); err != nil {
		t.Fatalf("UploadDatasetFile: %v", err)
	}
	if _, err := api.MountDatasetFile(context.Background(), "greenhouse", "readings", 2,
		"rows.csv", "sha256:test"); err != nil {
		t.Fatalf("MountDatasetFile: %v", err)
	}

	if len(mediaTypes) != 2 {
		t.Fatalf("requests = %d, want upload and mount", len(mediaTypes))
	}
	for i, mediaType := range mediaTypes {
		if !strings.HasPrefix(mediaType, "text/csv") {
			t.Errorf("request %d Content-Type = %q, want text/csv from logical path", i+1, mediaType)
		}
	}
}

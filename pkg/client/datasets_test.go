package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

func TestDatasetUploadAndMountUseLogicalPathMediaType(t *testing.T) {
	var mediaTypes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaTypes = append(mediaTypes, r.Header.Get("Content-Type"))
		_ = json.NewEncoder(w).Encode(datadrop.UploadFileResult{
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

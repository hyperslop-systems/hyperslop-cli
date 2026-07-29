package dataset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildCommitRequestAppliesOverridesToNullManifest(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifestPath, []byte("null\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	req, err := buildCommitRequest(&pushSettings{
		Manifest: manifestPath,
		Title:    "Greenhouse readings",
		License:  "CC-BY-4.0",
	})
	if err != nil {
		t.Fatalf("buildCommitRequest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(req.Manifest, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest["title"] != "Greenhouse readings" || manifest["license"] != "CC-BY-4.0" {
		t.Fatalf("manifest = %v, want CLI overrides", manifest)
	}
}

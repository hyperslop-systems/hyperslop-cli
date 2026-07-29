package dataset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePushFilesRejectsNonRegularInput(t *testing.T) {
	if _, err := os.Stat("/dev/null"); err != nil {
		t.Skip("platform has no /dev/null device")
	}
	if _, err := resolvePushFiles([]string{"/dev/null:device"}, false); err == nil {
		t.Fatal("resolvePushFiles accepted a character device")
	}
}

func TestResolvePushFilesAllowsSymlinkToRegularFile(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.csv")
	link := filepath.Join(directory, "link.csv")
	if err := os.WriteFile(target, []byte("a\n1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	files, err := resolvePushFiles([]string{link + ":data.csv"}, false)
	if err != nil {
		t.Fatalf("resolvePushFiles rejected symlink to regular file: %v", err)
	}
	if len(files) != 1 || files[0].LogicalPath != "data.csv" {
		t.Fatalf("files = %+v", files)
	}
}

func TestBuildCommitRequestRejectsExplicitEmptySchema(t *testing.T) {
	schemaPath := filepath.Join(t.TempDir(), "empty.schema.json")
	if err := os.WriteFile(schemaPath, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := buildCommitRequest(&pushSettings{Schema: schemaPath}); err == nil {
		t.Fatal("buildCommitRequest silently omitted an explicitly empty schema")
	}
}

func TestBuildCommitRequestPreservesExactManifestNumbers(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	raw := `{"sample_id":9007199254740993,"resolution":0.123456789012345678901}`
	if err := os.WriteFile(manifestPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	req, err := buildCommitRequest(&pushSettings{Manifest: manifestPath, Title: "Readings"})
	if err != nil {
		t.Fatalf("buildCommitRequest: %v", err)
	}
	for _, exact := range []string{"9007199254740993", "0.123456789012345678901"} {
		if !strings.Contains(string(req.Manifest), exact) {
			t.Errorf("manifest %s lost exact number %s", req.Manifest, exact)
		}
	}
}

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

package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishDownloadedFileVerifiesBeforePublication(t *testing.T) {
	output := t.TempDir()
	root, err := os.OpenRoot(output)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	err = publishDownloadedFile(
		root,
		"data/readings.csv",
		io.NopCloser(strings.NewReader("corrupted")),
		digestString("expected"),
		true,
		false,
	)
	if err == nil {
		t.Fatal("publishDownloadedFile accepted a digest mismatch")
	}
	if _, err := os.Stat(filepath.Join(output, "data", "readings.csv")); !os.IsNotExist(err) {
		t.Fatalf("final file exists after failed verification: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(output, "data"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary files remain after failed verification: %v", entries)
	}
}

func TestPublishDownloadedFileDoesNotOverwriteWithoutForce(t *testing.T) {
	output := t.TempDir()
	if err := os.WriteFile(filepath.Join(output, "existing.txt"), []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	root, err := os.OpenRoot(output)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	if err := publishDownloadedFile(
		root, "existing.txt", io.NopCloser(strings.NewReader("replacement")),
		digestString("replacement"), true, false,
	); err == nil {
		t.Fatal("publishDownloadedFile overwrote an existing file without force")
	}
	got, err := os.ReadFile(filepath.Join(output, "existing.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("existing file = %q, want original", got)
	}
}

func TestPublishDownloadedFileForceReplacesAfterVerification(t *testing.T) {
	output := t.TempDir()
	if err := os.WriteFile(filepath.Join(output, "existing.txt"), []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	root, err := os.OpenRoot(output)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	if err := publishDownloadedFile(
		root, "existing.txt", io.NopCloser(strings.NewReader("replacement")),
		digestString("replacement"), true, true,
	); err != nil {
		t.Fatalf("publishDownloadedFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(output, "existing.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "replacement" {
		t.Fatalf("existing file = %q, want replacement", got)
	}
}

func TestPublishDownloadedFileRefusesSymlinkComponents(t *testing.T) {
	output := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(output, "linked")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	root, err := os.OpenRoot(output)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	if err := publishDownloadedFile(
		root, "linked/escape.txt", io.NopCloser(strings.NewReader("no")),
		digestString("no"), true, false,
	); err == nil {
		t.Fatal("publishDownloadedFile accepted a symlink directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("file escaped output root: %v", err)
	}
}

func TestStreamToDestinationDoesNotOverwriteWithoutForce(t *testing.T) {
	target := filepath.Join(t.TempDir(), "archive.tar")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	opened := false
	err := streamToDestination(target, false, func() (io.ReadCloser, error) {
		opened = true
		return io.NopCloser(strings.NewReader("replacement")), nil
	})
	if err == nil {
		t.Fatal("streamToDestination overwrote an existing file without force")
	}
	if opened {
		t.Fatal("download body opened before the existing destination was rejected")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("existing file = %q, want original", got)
	}
}

func TestStreamToDestinationForceReplacesExistingFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "one.csv")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := streamToDestination(target, true, func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("replacement")), nil
	}); err != nil {
		t.Fatalf("streamToDestination: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "replacement" {
		t.Fatalf("existing file = %q, want replacement", got)
	}
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

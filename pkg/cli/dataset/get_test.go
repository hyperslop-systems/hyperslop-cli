package dataset

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"

	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"
)

func TestGetSettingsRejectsArchiveAndFileTogether(t *testing.T) {
	if err := (&getSettings{Archive: true, File: "data/readings.csv"}).validate(); err == nil {
		t.Fatal("get settings accepted --archive with --file")
	}
	for _, settings := range []getSettings{{Archive: true}, {File: "data/readings.csv"}, {}} {
		if err := settings.validate(); err != nil {
			t.Fatalf("valid settings rejected: %v", err)
		}
	}
}

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
	err := streamToDestination(target, false, "", false, func() (io.ReadCloser, error) {
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
	if err := streamToDestination(target, true, "", false, func() (io.ReadCloser, error) {
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

func TestStreamToDestinationForcePreservesExistingFileOnOpenFailure(t *testing.T) {
	target := filepath.Join(t.TempDir(), "archive.tar")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	openErr := errors.New("server returned 404")
	err := streamToDestination(target, true, "", false, func() (io.ReadCloser, error) {
		return nil, openErr
	})
	if !errors.Is(err, openErr) {
		t.Fatalf("streamToDestination error = %v, want %v", err, openErr)
	}
	assertFileContent(t, target, "original")
	assertNoDownloadTemps(t, filepath.Dir(target))
}

func TestStreamToDestinationForcePreservesExistingFileOnTransferFailure(t *testing.T) {
	target := filepath.Join(t.TempDir(), "archive.tar")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	readErr := errors.New("connection reset")
	err := streamToDestination(target, true, "", false, func() (io.ReadCloser, error) {
		body := io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(readErr))
		return io.NopCloser(body), nil
	})
	if !errors.Is(err, readErr) {
		t.Fatalf("streamToDestination error = %v, want wrapped %v", err, readErr)
	}
	assertFileContent(t, target, "original")
	assertNoDownloadTemps(t, filepath.Dir(target))
}

func TestStreamToDestinationForcePreservesExistingFileOnDigestMismatch(t *testing.T) {
	target := filepath.Join(t.TempDir(), "one.csv")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := streamToDestination(target, true, digestString("expected"), true,
		func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("corrupted")), nil
		})
	if err == nil || !strings.Contains(err.Error(), "integrity check failed") {
		t.Fatalf("streamToDestination error = %v, want integrity failure", err)
	}
	assertFileContent(t, target, "original")
	assertNoDownloadTemps(t, filepath.Dir(target))
}

func TestDownloadArchiveVerifiesEntriesBeforePublication(t *testing.T) {
	const (
		logicalPath = "data/readings.csv"
		expected    = "temperature\n21.7\n"
	)
	for _, tc := range []struct {
		name        string
		archiveBody []byte
		wantError   bool
	}{
		{"valid", makeDatasetArchive(t, logicalPath, expected), false},
		{"digest mismatch", makeDatasetArchive(t, logicalPath, "temperature\n99.9\n"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requested := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/drops/greenhouse/datasets/readings/versions/latest":
					_ = json.NewEncoder(w).Encode(datalab.DatasetVersion{
						Drop: "greenhouse", Dataset: "readings", Version: 7,
						Files: []datalab.DatasetFile{{
							Path: logicalPath, Digest: digestString(expected), SizeBytes: int64(len(expected)),
						}},
					})
				case "/v1/drops/greenhouse/datasets/readings/versions/7/archive":
					requested <- r.URL.Path
					_, _ = w.Write(tc.archiveBody)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			api, err := client.New(server.URL, "token")
			if err != nil {
				t.Fatalf("client.New: %v", err)
			}
			target := filepath.Join(t.TempDir(), "version.tar")
			if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			err = downloadArchive(context.Background(), api, &getSettings{
				Drop: "greenhouse", Dataset: "readings", Version: datalab.LatestVersion,
				Archive: true, Output: target, Force: true,
			})
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "integrity check failed") {
					t.Fatalf("downloadArchive error = %v, want integrity failure", err)
				}
				assertFileContent(t, target, "original")
			} else {
				if err != nil {
					t.Fatalf("downloadArchive: %v", err)
				}
				got, readErr := os.ReadFile(target)
				if readErr != nil {
					t.Fatalf("ReadFile: %v", readErr)
				}
				if !bytes.Equal(got, tc.archiveBody) {
					t.Fatal("published archive differs from the server's canonical bytes")
				}
			}
			select {
			case path := <-requested:
				if !strings.Contains(path, "/versions/7/") {
					t.Fatalf("archive request was not pinned: %s", path)
				}
			default:
				t.Fatal("archive endpoint was not requested")
			}
			assertNoDownloadTemps(t, filepath.Dir(target))
		})
	}
}

func makeDatasetArchive(t *testing.T, logicalPath, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range []struct {
		name, content string
	}{
		{"manifest.json", `{}`},
		{"files/" + logicalPath, content},
	} {
		header := &tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.content))}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if _, err := io.WriteString(writer, entry.content); err != nil {
			t.Fatalf("write archive entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return buffer.Bytes()
}

func TestDownloadSingleFileVerifiesResolvedVersionBeforePublication(t *testing.T) {
	const content = "temperature\n21.7\n"
	requestedConcreteVersion := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/drops/greenhouse/datasets/readings/versions/latest":
			_ = json.NewEncoder(w).Encode(datalab.DatasetVersion{
				Drop: "greenhouse", Dataset: "readings", Version: 7,
				Files: []datalab.DatasetFile{{
					Path: "data/readings.csv", Digest: digestString(content), SizeBytes: int64(len(content)),
				}},
			})
		case "/v1/drops/greenhouse/datasets/readings/versions/7/files/data/readings.csv":
			requestedConcreteVersion = true
			_, _ = io.WriteString(w, content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	api, err := client.New(server.URL, "token")
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	target := filepath.Join(t.TempDir(), "readings.csv")
	err = downloadSingleFile(context.Background(), api, &getSettings{
		Drop: "greenhouse", Dataset: "readings", Version: datalab.LatestVersion,
		File: "data/readings.csv", Output: target,
	})
	if err != nil {
		t.Fatalf("downloadSingleFile: %v", err)
	}
	if !requestedConcreteVersion {
		t.Fatal("file bytes were not requested from the manifest's concrete version")
	}
	assertFileContent(t, target, content)
}

func TestDownloadVersionForceDoesNotMixVersionsOnLaterFailure(t *testing.T) {
	files := map[string]string{
		"data/a.csv": "new a\n",
		"data/b.csv": "new b\n",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/drops/greenhouse/datasets/readings/versions/latest" {
			version := datalab.DatasetVersion{Drop: "greenhouse", Dataset: "readings", Version: 7}
			for logicalPath, content := range files {
				version.Files = append(version.Files, datalab.DatasetFile{
					Path: logicalPath, Digest: digestString(content), SizeBytes: int64(len(content)),
				})
			}
			_ = json.NewEncoder(w).Encode(version)
			return
		}
		const prefix = "/v1/drops/greenhouse/datasets/readings/versions/7/files/"
		logicalPath := strings.TrimPrefix(r.URL.Path, prefix)
		if logicalPath == "data/b.csv" {
			// The HTTP request succeeds but integrity verification fails after a
			// has been staged, reproducing the old mixed-version failure.
			_, _ = io.WriteString(w, "corrupt b\n")
			return
		}
		_, _ = io.WriteString(w, files[logicalPath])
	}))
	defer server.Close()
	api, err := client.New(server.URL, "token")
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	output := t.TempDir()
	for logicalPath, old := range map[string]string{"data/a.csv": "old a\n", "data/b.csv": "old b\n"} {
		filename := filepath.Join(output, filepath.FromSlash(logicalPath))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(old), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	err = downloadVersion(context.Background(), api, &getSettings{
		Drop: "greenhouse", Dataset: "readings", Version: datalab.LatestVersion,
		Output: output, Force: true,
	})
	if err == nil || !strings.Contains(err.Error(), "integrity check failed") {
		t.Fatalf("downloadVersion error = %v, want integrity failure", err)
	}
	assertFileContent(t, filepath.Join(output, "data", "a.csv"), "old a\n")
	assertFileContent(t, filepath.Join(output, "data", "b.csv"), "old b\n")
	assertNoDownloadTemps(t, filepath.Join(output, "data"))
}

func TestDownloadVersionPinsAllFilesToResolvedVersion(t *testing.T) {
	files := map[string]string{
		"data/a.csv": "a\n1\n",
		"data/b.csv": "b\n2\n",
	}
	var fileRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/drops/greenhouse/datasets/readings/versions/latest" {
			version := datalab.DatasetVersion{Drop: "greenhouse", Dataset: "readings", Version: 7}
			for logicalPath, content := range files {
				version.Files = append(version.Files, datalab.DatasetFile{
					Path: logicalPath, Digest: digestString(content), SizeBytes: int64(len(content)),
				})
			}
			_ = json.NewEncoder(w).Encode(version)
			return
		}
		const prefix = "/v1/drops/greenhouse/datasets/readings/versions/7/files/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			t.Errorf("file request used an unpinned version: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		logicalPath := strings.TrimPrefix(r.URL.Path, prefix)
		content, ok := files[logicalPath]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fileRequests.Add(1)
		_, _ = io.WriteString(w, content)
	}))
	defer server.Close()

	api, err := client.New(server.URL, "token")
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	output := t.TempDir()
	err = downloadVersion(context.Background(), api, &getSettings{
		Drop: "greenhouse", Dataset: "readings", Version: datalab.LatestVersion, Output: output,
	})
	if err != nil {
		t.Fatalf("downloadVersion: %v", err)
	}
	if got := fileRequests.Load(); got != int32(len(files)) {
		t.Fatalf("file requests = %d, want %d", got, len(files))
	}
	for logicalPath, content := range files {
		assertFileContent(t, filepath.Join(output, filepath.FromSlash(logicalPath)), content)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func assertNoDownloadTemps(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") || strings.Contains(entry.Name(), ".tmp") {
			t.Errorf("temporary file remains after failure: %s", entry.Name())
		}
	}
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

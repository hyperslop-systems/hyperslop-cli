package datalab

import (
	"encoding/json"
	"path"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/jsondoc"
)

// VersionState is the commit boundary of a dataset version.
//
// The distinction is not bookkeeping: a draft may be missing files, or hold a
// file that is still uploading. Serving one produces silently wrong results, so
// every read path filters on StateCommitted.
type VersionState string

const (
	// StateDraft means the version is still being assembled and must not be
	// visible to any reader.
	StateDraft VersionState = "draft"

	// StateCommitted means the version is complete and immutable.
	StateCommitted VersionState = "committed"
)

// LatestVersion is the path element that resolves to the highest committed
// version of a dataset.
const LatestVersion = "latest"

// Dataset is a stable name within a drop. It holds no content itself; content
// lives in its versions.
type Dataset struct {
	Drop      string    `json:"drop"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`

	// Versions is populated by inspection endpoints, and lists committed
	// versions only.
	Versions []DatasetVersion `json:"versions,omitempty"`
}

// DatasetVersion is an immutable numbered snapshot of a dataset.
type DatasetVersion struct {
	Drop        string       `json:"drop"`
	Dataset     string       `json:"dataset"`
	Version     int          `json:"version"`
	State       VersionState `json:"state"`
	FileCount   int          `json:"file_count"`
	TotalBytes  int64        `json:"total_bytes"`
	CreatedAt   time.Time    `json:"created_at"`
	CommittedAt *time.Time   `json:"committed_at,omitempty"`

	// Manifest is the producer's description, stored and returned verbatim.
	Manifest json.RawMessage `json:"manifest,omitempty"`

	// Schema is an optional JSON Schema describing one record of the dataset —
	// a CSV row or an NDJSON line. It is applied during materialization, not at
	// upload time.
	Schema json.RawMessage `json:"schema,omitempty"`

	// Files is populated by inspection endpoints.
	Files []DatasetFile `json:"files,omitempty"`
}

// DatasetFile is one logical path within a version, pointing at a blob.
type DatasetFile struct {
	Path      string `json:"path"`
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
	MediaType string `json:"media_type,omitempty"`
}

// DeleteDatasetVersionResult identifies the immutable version removed by the
// delete endpoint. Version is always concrete even when the request used the
// "latest" alias, so audit rows never lose which version was affected.
type DeleteDatasetVersionResult struct {
	Drop    string `json:"drop"`
	Dataset string `json:"dataset"`
	Version int    `json:"version"`
	Deleted bool   `json:"deleted"`
}

// Manifest is the recognized subset of a version's manifest document.
//
// Only these three fields are interpreted and indexed. Everything else in the
// submitted document is preserved verbatim and returned unchanged; extracting
// more would freeze more of the shape, because removing an indexed field later
// means a migration.
type Manifest struct {
	Title    string `json:"title,omitempty"`
	License  string `json:"license,omitempty"`
	RowCount *int64 `json:"row_count,omitempty"`
}

// ParseManifest validates a manifest document and extracts the indexed fields.
//
// An empty or absent document is legal and yields a zero Manifest: publishing a
// dataset must not require writing metadata first, and metadata can always be
// added by publishing a new version.
//
// Type errors are reported here, at commit time, rather than surfacing later
// when something tries to use row_count as a number.
func ParseManifest(raw json.RawMessage) (Manifest, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return Manifest{}, nil
	}

	// Reject a non-object document explicitly. Unmarshalling an array into a
	// struct produces a confusing error about the wrong Go type.
	probe, err := jsondoc.Value([]byte(trimmed))
	if err != nil {
		return Manifest{}, errors.Wrap(err, "invalid manifest: not valid JSON")
	}
	if _, isObject := probe.(map[string]any); !isObject {
		return Manifest{}, errors.New("invalid manifest: expected a JSON object")
	}

	var manifest Manifest
	if err := json.Unmarshal([]byte(trimmed), &manifest); err != nil {
		return Manifest{}, errors.Wrap(err, "invalid manifest")
	}
	if manifest.RowCount != nil && *manifest.RowCount < 0 {
		return Manifest{}, errors.Errorf("invalid manifest: row_count must not be negative, got %d", *manifest.RowCount)
	}
	return manifest, nil
}

// maxPathLength bounds a logical path so that a hostile client cannot make a
// database row or a filesystem path unreasonably large.
const maxPathLength = 1024

// ValidateDatasetPath checks a logical path within a dataset version.
//
// This is a security control, not a formatting rule. Logical paths never touch
// the filesystem on the server — files are stored under their digest — but the
// path IS used when a client writes a downloaded dataset into a local
// directory, and that is where traversal becomes exploitable. The server is the
// only place that can reject it for every client at once.
func ValidateDatasetPath(p string) error {
	switch {
	case p == "":
		return errors.New("invalid path: must not be empty")
	case p == ".":
		return errors.New("invalid path \".\": must name a file beneath the dataset root")
	case len(p) > maxPathLength:
		return errors.Errorf("invalid path: must be at most %d characters, got %d", maxPathLength, len(p))
	case strings.ContainsRune(p, 0):
		return errors.New("invalid path: must not contain a NUL byte")
	case strings.HasPrefix(p, "/"):
		return errors.Errorf("invalid path %q: must be relative", p)
	case strings.Contains(p, `\`):
		// Backslashes are a separator on Windows, so a path containing one is
		// ambiguous once a client writes it to disk.
		return errors.Errorf("invalid path %q: must not contain a backslash", p)
	}

	// path.Clean collapses "a/../b" and strips "./". If cleaning changes the
	// path, it was not in canonical form, and accepting both forms would let
	// two spellings of one path occupy two rows.
	cleaned := path.Clean(p)
	if cleaned != p {
		return errors.Errorf("invalid path %q: not in canonical form (did you mean %q?)", p, cleaned)
	}
	for _, element := range strings.Split(cleaned, "/") {
		if element == ".." {
			return errors.Errorf("invalid path %q: must not traverse upwards", p)
		}
		if element == "" {
			return errors.Errorf("invalid path %q: must not contain an empty element", p)
		}
	}
	return nil
}

// CommitVersionRequest is the body of the commit endpoint.
type CommitVersionRequest struct {
	Manifest json.RawMessage `json:"manifest,omitempty"`
	Schema   json.RawMessage `json:"schema,omitempty"`
}

// UploadFileResult is returned when a file is added to a draft version.
type UploadFileResult struct {
	Path      string `json:"path"`
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`

	// Deduplicated reports that the server already held these bytes, so no
	// storage was consumed.
	Deduplicated bool `json:"deduplicated"`
}

// ImportResult summarizes a materialization run.
type ImportResult struct {
	Drop     string `json:"drop"`
	Dataset  string `json:"dataset"`
	Version  int    `json:"version"`
	Path     string `json:"path"`
	Stream   string `json:"stream"`
	Rows     int    `json:"rows"`
	Appended int    `json:"appended"`
	Skipped  int    `json:"skipped"`

	// Warnings holds schema violations from rows accepted in permissive mode.
	Warnings []Violation `json:"warnings,omitempty"`

	// WarningCount is the total number of schema violations. Warnings is only a
	// bounded sample so a large permissive import cannot create an unbounded
	// response.
	WarningCount int `json:"warning_count,omitempty"`

	// Truncated reports that the row cap was reached before the file ended.
	Truncated bool `json:"truncated,omitempty"`
}

// Dataset audit actions.
const (
	ActionDatasetVersionOpen   = "dataset.version.open"
	ActionDatasetFileAdd       = "dataset.file.add"
	ActionDatasetVersionCommit = "dataset.version.commit"
	ActionDatasetVersionDelete = "dataset.version.delete"
	ActionDatasetImport        = "dataset.import"
)

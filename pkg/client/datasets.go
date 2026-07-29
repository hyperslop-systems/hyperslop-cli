package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

// ListDatasets returns the datasets in a drop.
func (c *Client) ListDatasets(ctx context.Context, drop string) ([]datadrop.Dataset, error) {
	var response struct {
		Datasets []datadrop.Dataset `json:"datasets"`
	}
	err := c.doJSON(ctx, http.MethodGet,
		"/v1/drops/"+url.PathEscape(drop)+"/datasets", nil, nil, &response)
	return response.Datasets, err
}

// GetDataset returns one dataset with its committed versions.
func (c *Client) GetDataset(ctx context.Context, drop, dataset string) (datadrop.Dataset, error) {
	var found datadrop.Dataset
	err := c.doJSON(ctx, http.MethodGet, c.datasetPath(drop, dataset), nil, nil, &found)
	return found, err
}

// GetDatasetVersion returns one version. Pass datadrop.LatestVersion for the
// highest committed one.
func (c *Client) GetDatasetVersion(
	ctx context.Context, drop, dataset, version string,
) (datadrop.DatasetVersion, error) {
	var found datadrop.DatasetVersion
	err := c.doJSON(ctx, http.MethodGet,
		c.versionPath(drop, dataset, version), nil, nil, &found)
	return found, err
}

// OpenDatasetVersion opens a draft version.
func (c *Client) OpenDatasetVersion(
	ctx context.Context, drop, dataset string,
) (datadrop.DatasetVersion, error) {
	var version datadrop.DatasetVersion
	err := c.doJSON(ctx, http.MethodPost,
		c.datasetPath(drop, dataset)+"/versions", nil, nil, &version)
	return version, err
}

// CommitDatasetVersion seals a draft with a manifest and optional schema.
func (c *Client) CommitDatasetVersion(
	ctx context.Context, drop, dataset string, version int, req datadrop.CommitVersionRequest,
) (datadrop.DatasetVersion, error) {
	var committed datadrop.DatasetVersion
	err := c.doJSON(ctx, http.MethodPost,
		c.versionPath(drop, dataset, strconv.Itoa(version))+"/commit", nil, req, &committed)
	return committed, err
}

// DeleteDatasetVersion removes a version and returns the concrete version the
// server deleted. This matters when version is the "latest" alias.
func (c *Client) DeleteDatasetVersion(
	ctx context.Context, drop, dataset, version string,
) (datadrop.DeleteDatasetVersionResult, error) {
	var deleted datadrop.DeleteDatasetVersionResult
	err := c.doJSON(ctx, http.MethodDelete,
		c.versionPath(drop, dataset, version), nil, nil, &deleted)
	return deleted, err
}

// BlobExists asks whether the server already holds these bytes.
//
// This is the digest precheck. On a hit the client skips the transfer entirely
// and mounts the existing blob instead, which for a dataset republished with one
// changed file means uploading only that file.
func (c *Client) BlobExists(ctx context.Context, digest string) (bool, error) {
	resp, err := c.do(ctx, http.MethodHead, "/v1/blobs/"+url.PathEscape(digest), nil, nil)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	return true, nil
}

// UploadDatasetFile streams a local file into a draft version.
//
// It always sends the digest it computed locally, so the server can verify that
// what arrived is what was sent.
func (c *Client) UploadDatasetFile(
	ctx context.Context, drop, dataset string, version int,
	logicalPath, localPath, digest string,
) (datadrop.UploadFileResult, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return datadrop.UploadFileResult{}, errors.Wrapf(err, "open %s", localPath)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return datadrop.UploadFileResult{}, errors.Wrapf(err, "stat %s", localPath)
	}

	target := c.versionPath(drop, dataset, strconv.Itoa(version)) +
		"/files/" + escapePath(logicalPath) + "?digest=" + url.QueryEscape(digest)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+target, file)
	if err != nil {
		return datadrop.UploadFileResult{}, errors.Wrap(err, "client: build upload request")
	}
	// Setting ContentLength lets the transport avoid chunked encoding, and lets
	// the server's size cap reject an oversized upload before reading it.
	req.ContentLength = info.Size()
	if mediaType := mime.TypeByExtension(filepath.Ext(localPath)); mediaType != "" {
		req.Header.Set("Content-Type", mediaType)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	return c.finishUpload(req)
}

// MountDatasetFile records a file whose bytes the server already holds, without
// transferring anything.
func (c *Client) MountDatasetFile(
	ctx context.Context, drop, dataset string, version int, logicalPath, digest string,
) (datadrop.UploadFileResult, error) {
	target := c.versionPath(drop, dataset, strconv.Itoa(version)) +
		"/files/" + escapePath(logicalPath) + "?digest=" + url.QueryEscape(digest)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+target, http.NoBody)
	if err != nil {
		return datadrop.UploadFileResult{}, errors.Wrap(err, "client: build mount request")
	}
	req.ContentLength = 0
	if mediaType := mime.TypeByExtension(path.Ext(logicalPath)); mediaType != "" {
		req.Header.Set("Content-Type", mediaType)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	return c.finishUpload(req)
}

func (c *Client) finishUpload(req *http.Request) (datadrop.UploadFileResult, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return datadrop.UploadFileResult{}, errors.Wrap(err, "client: upload")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return datadrop.UploadFileResult{}, apiErrorFrom(resp)
	}

	var result datadrop.UploadFileResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return datadrop.UploadFileResult{}, errors.Wrap(err, "client: decode upload result")
	}
	return result, nil
}

// DownloadDatasetFile streams one file. The caller owns the returned reader.
func (c *Client) DownloadDatasetFile(
	ctx context.Context, drop, dataset, version, logicalPath string,
) (io.ReadCloser, error) {
	resp, err := c.do(ctx, http.MethodGet,
		c.versionPath(drop, dataset, version)+"/files/"+escapePath(logicalPath), nil, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// DownloadDatasetArchive streams a whole version as tar.
func (c *Client) DownloadDatasetArchive(
	ctx context.Context, drop, dataset, version string,
) (io.ReadCloser, error) {
	resp, err := c.do(ctx, http.MethodGet,
		c.versionPath(drop, dataset, version)+"/archive", nil, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// PushFile is one local file destined for a logical path in a dataset.
type PushFile struct {
	LocalPath   string
	LogicalPath string
}

// PushResult reports what a push transferred.
type PushResult struct {
	Version      datadrop.DatasetVersion
	Uploaded     int
	Mounted      int
	BytesSent    int64
	BytesSkipped int64
}

// PushDataset publishes a new version: open a draft, upload or mount each file,
// then commit.
//
// Each file is hashed locally first so the server can be asked whether it
// already holds those bytes. That one local read is what turns a republish with
// one changed file into a single upload.
func (c *Client) PushDataset(
	ctx context.Context, drop, dataset string, files []PushFile, req datadrop.CommitVersionRequest,
) (PushResult, error) {
	if len(files) == 0 {
		return PushResult{}, errors.New("client: at least one file is required")
	}

	version, err := c.OpenDatasetVersion(ctx, drop, dataset)
	if err != nil {
		return PushResult{}, err
	}

	result := PushResult{}
	for _, file := range files {
		digest, size, err := HashFile(file.LocalPath)
		if err != nil {
			return result, err
		}

		exists, err := c.BlobExists(ctx, digest)
		if err != nil {
			return result, err
		}

		if exists {
			if _, err := c.MountDatasetFile(ctx, drop, dataset, version.Version,
				file.LogicalPath, digest); err != nil {
				return result, errors.Wrapf(err, "mount %s", file.LogicalPath)
			}
			result.Mounted++
			result.BytesSkipped += size
			continue
		}

		if _, err := c.UploadDatasetFile(ctx, drop, dataset, version.Version,
			file.LogicalPath, file.LocalPath, digest); err != nil {
			return result, errors.Wrapf(err, "upload %s", file.LogicalPath)
		}
		result.Uploaded++
		result.BytesSent += size
	}

	committed, err := c.CommitDatasetVersion(ctx, drop, dataset, version.Version, req)
	if err != nil {
		return result, err
	}
	result.Version = committed
	return result, nil
}

// HashFile computes a local file's content address and size.
func HashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, errors.Wrapf(err, "open %s", path)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, errors.Wrapf(err, "hash %s", path)
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), size, nil
}

// VerifyFile recomputes a downloaded file's digest and compares it.
//
// The client performs the same integrity check the server does, so corruption
// in transit is caught at the point of use rather than trusted away.
func VerifyFile(path, expected string) error {
	actual, _, err := HashFile(path)
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.Errorf("integrity check failed for %s: content hashes to %s, manifest says %s",
			path, actual, expected)
	}
	return nil
}

func (c *Client) datasetPath(drop, dataset string) string {
	return "/v1/drops/" + url.PathEscape(drop) + "/datasets/" + url.PathEscape(dataset)
}

func (c *Client) versionPath(drop, dataset, version string) string {
	return c.datasetPath(drop, dataset) + "/versions/" + url.PathEscape(version)
}

// escapePath percent-encodes each element of a logical path while keeping the
// separators, so that "data/my file.csv" survives the round trip.
func escapePath(logicalPath string) string {
	elements := strings.Split(logicalPath, "/")
	for i, element := range elements {
		elements[i] = url.PathEscape(element)
	}
	return strings.Join(elements, "/")
}

// apiErrorFrom builds an *APIError from a failing response.
func apiErrorFrom(resp *http.Response) error {
	apiErr := &APIError{Status: resp.StatusCode, RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, apiErr); err != nil || apiErr.Code == "" {
		// Not a problem document — a proxy error page, most likely.
		apiErr.Code = http.StatusText(resp.StatusCode)
		apiErr.Detail = strings.TrimSpace(string(raw))
	}
	return apiErr
}

// ImportDataset materializes a dataset file's rows into an event stream.
func (c *Client) ImportDataset(
	ctx context.Context, drop, dataset, version, logicalPath, stream, format string,
	maxRows int, strict bool,
) (datadrop.ImportResult, error) {
	query := url.Values{}
	query.Set("path", logicalPath)
	if stream != "" && stream != datadrop.DefaultStream {
		query.Set("stream", stream)
	}
	if format != "" {
		query.Set("format", format)
	}
	if maxRows > 0 {
		query.Set("max_rows", strconv.Itoa(maxRows))
	}
	if strict {
		query.Set("strict", "true")
	}

	var result datadrop.ImportResult
	err := c.doJSON(ctx, http.MethodPost,
		c.versionPath(drop, dataset, version)+"/import", query, nil, &result)
	return result, err
}

// GCResult summarizes a garbage-collection sweep.
type GCResult struct {
	Scanned    int   `json:"scanned"`
	Referenced int   `json:"referenced"`
	Deleted    int   `json:"deleted"`
	FreedBytes int64 `json:"freed_bytes"`
}

// GarbageCollect asks the server to delete unreferenced stored bytes.
func (c *Client) GarbageCollect(ctx context.Context, minAgeSeconds int) (GCResult, error) {
	query := url.Values{}
	if minAgeSeconds > 0 {
		query.Set("min_age_seconds", strconv.Itoa(minAgeSeconds))
	}

	var result GCResult
	err := c.doJSON(ctx, http.MethodPost, "/v1/blobs/gc", query, nil, &result)
	return result, err
}

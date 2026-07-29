package dataset

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

// GetCommand downloads a dataset version.
//
// It is a BareCommand (DR-81): its result is files on disk, and a row saying
// "four files were written" would be a description of the answer rather than
// the answer. Emitting a row per downloaded file as *well* is a reasonable
// thing to want and is deliberately out of scope — progress goes to stderr
// today and rows would go to stdout, which is a behaviour change for anyone
// redirecting.
type GetCommand struct {
	*cmds.CommandDescription
}

var _ cmds.BareCommand = &GetCommand{}

// NewGetCommand builds `datadrop dataset get DROP DATASET`.
func NewGetCommand() (cmds.Command, error) {
	clientSection, err := ddcli.NewClientSection()
	if err != nil {
		return nil, err
	}

	return &GetCommand{cmds.NewCommandDescription(
		"get",
		cmds.WithShort("Download a dataset version"),
		cmds.WithLong(ddcli.RenderAppText(strings.TrimSpace(`
Download a dataset version.

Each downloaded file's digest is recomputed and compared against the version's
file list, so corruption in transit is caught at the point of use rather than
trusted away.

    {{app}} dataset get greenhouse readings-2026 --output ./downloaded/
    {{app}} dataset get greenhouse readings-2026 --file data/readings.csv -o -
    {{app}} dataset get greenhouse readings-2026 --archive -o version.tar

This verb writes files, so it has no --output format flag: --output here is a
destination, the same one it has always been.
`))),
		cmds.WithArguments(
			fields.New("drop", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the drop holding the dataset")),
			fields.New("dataset", fields.TypeString,
				fields.WithIsArgument(true),
				fields.WithHelp("the dataset name")),
		),
		cmds.WithFlags(
			fields.New("version", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp(`version to download: a number or "latest" (default)`)),
			fields.New("output", fields.TypeString,
				fields.WithShortFlag("o"),
				fields.WithDefault(""),
				fields.WithHelp(`output directory, or a file path / "-" with --file or --archive`)),
			fields.New("file", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("download only this file from the version")),
			fields.New("archive", fields.TypeBool,
				fields.WithDefault(false),
				fields.WithHelp("download the whole version as a tar archive")),
			fields.New("no-verify", fields.TypeBool,
				fields.WithDefault(false),
				fields.WithHelp("skip the digest check on downloaded files")),
			fields.New("force", fields.TypeBool,
				fields.WithDefault(false),
				fields.WithHelp("replace existing destination files")),
		),
		cmds.WithSections(clientSection),
	)}, nil
}

type getSettings struct {
	Drop     string `glazed:"drop"`
	Dataset  string `glazed:"dataset"`
	Version  string `glazed:"version"`
	Output   string `glazed:"output"`
	File     string `glazed:"file"`
	Archive  bool   `glazed:"archive"`
	NoVerify bool   `glazed:"no-verify"`
	Force    bool   `glazed:"force"`
}

// Run downloads the version.
func (c *GetCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &getSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	if err := s.validate(); err != nil {
		return err
	}
	if s.Version == "" {
		s.Version = datadrop.LatestVersion
	}

	api, err := ddcli.ClientFrom(vals)
	if err != nil {
		return err
	}

	if s.Archive {
		return downloadArchive(ctx, api, s)
	}

	if s.File != "" {
		return downloadSingleFile(ctx, api, s)
	}

	if s.Output == "" || s.Output == "-" {
		return errors.New("--output DIRECTORY is required when downloading a whole version")
	}
	return downloadVersion(ctx, api, s)
}

func (s *getSettings) validate() error {
	if s.Archive && s.File != "" {
		return errors.New("--archive and --file are mutually exclusive")
	}
	return nil
}

// downloadArchive downloads a tar stream and, by default, verifies every
// files/... entry against the resolved version manifest before publication.
func downloadArchive(ctx context.Context, api *client.Client, s *getSettings) error {
	version := s.Version
	if s.NoVerify {
		return streamToDestination(s.Output, s.Force, "", false, func() (io.ReadCloser, error) {
			return api.DownloadDatasetArchive(ctx, s.Drop, s.Dataset, version)
		})
	}

	found, err := api.GetDatasetVersion(ctx, s.Drop, s.Dataset, s.Version)
	if err != nil {
		return err
	}
	version = strconv.Itoa(found.Version)
	return streamToDestinationWithCopier(s.Output, s.Force,
		func() (io.ReadCloser, error) {
			return api.DownloadDatasetArchive(ctx, s.Drop, s.Dataset, version)
		},
		func(dst io.Writer, body io.Reader) error {
			return copyVerifiedDatasetArchive(dst, body, found.Files)
		})
}

// downloadSingleFile downloads one file. Unless --no-verify was requested, it
// first resolves the version manifest and uses the concrete version number for
// the byte request. Resolving "latest" before downloading avoids verifying
// bytes from a newer version against an older manifest if a publisher commits
// while the command is running.
func downloadSingleFile(ctx context.Context, api *client.Client, s *getSettings) error {
	version := s.Version
	expectedDigest := ""
	if !s.NoVerify {
		found, err := api.GetDatasetVersion(ctx, s.Drop, s.Dataset, s.Version)
		if err != nil {
			return err
		}
		version = strconv.Itoa(found.Version)
		for _, file := range found.Files {
			if file.Path == s.File {
				expectedDigest = file.Digest
				break
			}
		}
		if expectedDigest == "" {
			return errors.Errorf("dataset version %d does not contain file %q", found.Version, s.File)
		}
	}

	return streamToDestination(s.Output, s.Force, expectedDigest, !s.NoVerify,
		func() (io.ReadCloser, error) {
			return api.DownloadDatasetFile(ctx, s.Drop, s.Dataset, version, s.File)
		})
}

// downloadVersion writes every file of a version beneath a directory, at its
// logical path, and verifies each digest.
func downloadVersion(ctx context.Context, api *client.Client, s *getSettings) error {
	found, err := api.GetDatasetVersion(ctx, s.Drop, s.Dataset, s.Version)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Output, 0o755); err != nil {
		return errors.Wrap(err, "create output directory")
	}
	root, err := os.OpenRoot(s.Output)
	if err != nil {
		return errors.Wrap(err, "open output root")
	}
	defer func() { _ = root.Close() }()

	// Pin every byte request to the manifest's immutable numeric version.
	// Resolving "latest" independently per file could assemble a mixed version
	// if a publisher commits while this loop is running.
	concreteVersion := strconv.Itoa(found.Version)
	staged := make([]stagedDownloadedFile, 0, len(found.Files))
	defer func() { cleanupStagedDownloads(root, staged) }()
	for _, file := range found.Files {
		// The logical path is server-validated, but this is the moment where a
		// hostile path would escape the output directory, so it is checked again
		// on the machine that is about to write it.
		if err := datadrop.ValidateDatasetPath(file.Path); err != nil {
			return errors.Wrapf(err, "refusing to write %q", file.Path)
		}
		body, err := api.DownloadDatasetFile(ctx, s.Drop, s.Dataset, concreteVersion, file.Path)
		if err != nil {
			return errors.Wrapf(err, "download %s", file.Path)
		}
		stagedFile, err := stageDownloadedFile(root, file.Path, body, file.Digest, !s.NoVerify, s.Force)
		if err != nil {
			return errors.Wrapf(err, "download %s", file.Path)
		}
		staged = append(staged, stagedFile)
	}
	if err := publishStagedDownloads(root, staged, s.Force); err != nil {
		return err
	}
	for _, file := range found.Files {
		fmt.Fprintf(os.Stderr, "%s  %s\n", ddcli.HumanBytes(file.SizeBytes), file.Path)
	}

	fmt.Fprintf(os.Stdout, "%s/%s version %d: %d file(s) written to %s\n",
		s.Drop, s.Dataset, found.Version, len(found.Files), s.Output)
	return nil
}

type stagedDownloadedFile struct {
	logicalPath string
	tempName    string
	backupName  string
}

// stageDownloadedFile writes and verifies one file without making it visible at
// its logical destination. Whole-version downloads collect every such stage
// before changing any existing file, which prevents a failed later transfer
// from producing a mixed-version directory.
func stageDownloadedFile(
	root *os.Root,
	logicalPath string,
	body io.ReadCloser,
	expectedDigest string,
	verify bool,
	force bool,
) (stagedDownloadedFile, error) {
	defer func() { _ = body.Close() }()

	parent := path.Dir(logicalPath)
	if parent != "." {
		if err := rejectSymlinkComponents(root, parent); err != nil {
			return stagedDownloadedFile{}, err
		}
		if err := root.MkdirAll(parent, 0o755); err != nil {
			return stagedDownloadedFile{}, errors.Wrapf(err, "create directory for %s", logicalPath)
		}
	}
	if info, err := root.Lstat(logicalPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return stagedDownloadedFile{}, errors.Errorf("refusing symlink destination %q", logicalPath)
		}
		if !force {
			return stagedDownloadedFile{}, errors.Errorf("destination %q already exists (use --force to replace it)", logicalPath)
		}
	} else if !os.IsNotExist(err) {
		return stagedDownloadedFile{}, errors.Wrapf(err, "inspect destination %s", logicalPath)
	}

	tempName, err := downloadTempName(parent)
	if err != nil {
		return stagedDownloadedFile{}, err
	}
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = root.Remove(tempName)
		}
	}()
	file, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return stagedDownloadedFile{}, errors.Wrapf(err, "create temporary file for %s", logicalPath)
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, hasher), body); err != nil {
		return stagedDownloadedFile{}, errors.Wrapf(err, "write temporary file for %s", logicalPath)
	}
	if err := file.Sync(); err != nil {
		return stagedDownloadedFile{}, errors.Wrapf(err, "sync temporary file for %s", logicalPath)
	}
	if err := file.Close(); err != nil {
		return stagedDownloadedFile{}, errors.Wrapf(err, "close temporary file for %s", logicalPath)
	}
	closed = true
	actualDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if verify && actualDigest != expectedDigest {
		return stagedDownloadedFile{}, errors.Errorf(
			"integrity check failed for %s: content hashes to %s, manifest says %s",
			logicalPath, actualDigest, expectedDigest)
	}
	keepTemp = true
	return stagedDownloadedFile{logicalPath: logicalPath, tempName: tempName}, nil
}

func cleanupStagedDownloads(root *os.Root, staged []stagedDownloadedFile) {
	for _, file := range staged {
		if file.tempName != "" {
			_ = root.Remove(file.tempName)
		}
		if file.backupName != "" {
			_ = root.Remove(file.backupName)
		}
	}
}

// publishStagedDownloads changes destinations only after every payload has
// downloaded and passed verification. Existing force targets are moved to
// same-directory backups first; a failure while publishing restores them.
func publishStagedDownloads(root *os.Root, staged []stagedDownloadedFile, force bool) error {
	published := make([]int, 0, len(staged))
	committed := false
	rollback := func() {
		for i := len(published) - 1; i >= 0; i-- {
			_ = root.Remove(staged[published[i]].logicalPath)
		}
		for i := range staged {
			if staged[i].backupName != "" {
				_ = root.Rename(staged[i].backupName, staged[i].logicalPath)
			}
		}
	}
	defer func() {
		if !committed {
			rollback()
		}
	}()

	if force {
		for i := range staged {
			if _, err := root.Lstat(staged[i].logicalPath); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return errors.Wrapf(err, "inspect destination %s", staged[i].logicalPath)
			}
			backup, err := downloadTempName(path.Dir(staged[i].logicalPath))
			if err != nil {
				return err
			}
			if err := root.Rename(staged[i].logicalPath, backup); err != nil {
				return errors.Wrapf(err, "back up destination %s", staged[i].logicalPath)
			}
			staged[i].backupName = backup
		}
	}

	for i := range staged {
		var err error
		if force {
			err = root.Rename(staged[i].tempName, staged[i].logicalPath)
		} else {
			err = root.Link(staged[i].tempName, staged[i].logicalPath)
			if err == nil {
				err = root.Remove(staged[i].tempName)
			}
		}
		if err != nil {
			return errors.Wrapf(err, "publish %s", staged[i].logicalPath)
		}
		staged[i].tempName = ""
		published = append(published, i)
	}
	for i := range staged {
		if staged[i].backupName != "" {
			// Publication already succeeded; a failed best-effort backup cleanup
			// must not report failure and trigger rollback of the new version.
			_ = root.Remove(staged[i].backupName)
		}
	}
	committed = true
	return nil
}

// publishDownloadedFile retains the single-file helper contract while sharing
// the same staging path as a whole-version transaction.
func publishDownloadedFile(
	root *os.Root,
	logicalPath string,
	body io.ReadCloser,
	expectedDigest string,
	verify bool,
	force bool,
) error {
	staged, err := stageDownloadedFile(root, logicalPath, body, expectedDigest, verify, force)
	if err != nil {
		return err
	}
	defer func() { cleanupStagedDownloads(root, []stagedDownloadedFile{staged}) }()
	return publishStagedDownloads(root, []stagedDownloadedFile{staged}, force)
}

func rejectSymlinkComponents(root *os.Root, directory string) error {
	current := ""
	for _, element := range strings.Split(directory, "/") {
		current = path.Join(current, element)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return errors.Wrapf(err, "inspect output path %s", current)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.Errorf("refusing symlink directory %q", current)
		}
		if !info.IsDir() {
			return errors.Errorf("output path component %q is not a directory", current)
		}
	}
	return nil
}

func downloadTempName(parent string) (string, error) {
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", errors.Wrap(err, "mint download temporary name")
	}
	return path.Join(parent, ".datadrop-"+hex.EncodeToString(suffix[:])+".tmp"), nil
}

// streamToDestination writes a single response to stdout or publishes it to a
// file transactionally. A file destination is never truncated in place: bytes
// go to a temporary sibling, are optionally verified, fsynced, and only then
// atomically replace the destination. Thus --force preserves a known-good file
// across HTTP, transfer, and integrity failures.
func streamToDestination(
	output string,
	force bool,
	expectedDigest string,
	verify bool,
	open func() (io.ReadCloser, error),
) error {
	if verify && expectedDigest == "" {
		return errors.New("cannot verify download without an expected digest")
	}
	return streamToDestinationWithCopier(output, force, open,
		func(dst io.Writer, body io.Reader) error {
			return copyDownloadedContent(dst, body, expectedDigest, verify)
		})
}

// streamToDestinationWithCopier owns destination safety while copier owns the
// stream's integrity model (whole-body digest, verified tar entries, or plain
// copy). File outputs are always staged and published only after copier returns
// success; stdout remains streaming and reports a nonzero exit on late failure.
func streamToDestinationWithCopier(
	output string,
	force bool,
	open func() (io.ReadCloser, error),
	copier func(io.Writer, io.Reader) error,
) error {
	if output == "" || output == "-" {
		body, err := open()
		if err != nil {
			return err
		}
		defer func() { _ = body.Close() }()
		return copier(os.Stdout, body)
	}

	if info, err := os.Lstat(output); err == nil {
		if info.IsDir() {
			return errors.Errorf("destination %q is a directory", output)
		}
		if !force {
			return errors.Errorf("destination %q already exists (use --force to replace it)", output)
		}
	} else if !os.IsNotExist(err) {
		return errors.Wrapf(err, "inspect destination %s", output)
	}

	parent := filepath.Dir(output)
	temp, err := os.CreateTemp(parent, "."+filepath.Base(output)+"-*.tmp")
	if err != nil {
		return errors.Wrapf(err, "create temporary file for %s", output)
	}
	tempName := temp.Name()
	published := false
	defer func() {
		_ = temp.Close()
		if !published {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return errors.Wrapf(err, "restrict temporary file for %s", output)
	}

	body, err := open()
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	if err := copier(temp, body); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return errors.Wrapf(err, "sync temporary file for %s", output)
	}
	if err := temp.Close(); err != nil {
		return errors.Wrapf(err, "close temporary file for %s", output)
	}

	if force {
		if err := os.Rename(tempName, output); err != nil {
			return errors.Wrapf(err, "publish %s", output)
		}
	} else {
		// Link is the no-clobber publication primitive: unlike a preflight-only
		// existence check it remains safe if another process creates output
		// while the download is in flight.
		if err := os.Link(tempName, output); err != nil {
			if os.IsExist(err) {
				return errors.Errorf("destination %q already exists (use --force to replace it)", output)
			}
			return errors.Wrapf(err, "publish %s without overwrite", output)
		}
		if err := os.Remove(tempName); err != nil {
			return errors.Wrapf(err, "remove temporary file for %s", output)
		}
	}
	published = true
	return nil
}

const maxArchiveMetadataBytes = 16 << 20

func copyVerifiedDatasetArchive(
	dst io.Writer, body io.Reader, files []datadrop.DatasetFile,
) error {
	expected := make(map[string]datadrop.DatasetFile, len(files))
	for _, file := range files {
		expected[file.Path] = file
	}
	seen := make(map[string]struct{}, len(files))

	// Tee preserves the server's canonical tar bytes exactly while tar.Reader
	// validates framing and exposes each file for digest verification.
	tee := io.TeeReader(body, dst)
	archive := tar.NewReader(tee)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.Wrap(err, "verify dataset archive")
		}

		if header.Name == "manifest.json" || header.Name == "schema.json" {
			if header.Size < 0 || header.Size > maxArchiveMetadataBytes {
				return errors.Errorf("archive metadata entry %q has invalid size %d (maximum %d)",
					header.Name, header.Size, maxArchiveMetadataBytes)
			}
			if _, err := io.CopyN(io.Discard, archive, header.Size); err != nil {
				return errors.Wrapf(err, "read archive entry %s", header.Name)
			}
			continue
		}
		if !strings.HasPrefix(header.Name, "files/") {
			return errors.Errorf("dataset archive contains unexpected entry %q", header.Name)
		}
		logicalPath := strings.TrimPrefix(header.Name, "files/")
		file, ok := expected[logicalPath]
		if !ok {
			return errors.Errorf("dataset archive contains file %q absent from the version manifest", logicalPath)
		}
		if _, duplicate := seen[logicalPath]; duplicate {
			return errors.Errorf("dataset archive contains duplicate file %q", logicalPath)
		}
		if !header.FileInfo().Mode().IsRegular() {
			return errors.Errorf("dataset archive entry %q is not a regular file", header.Name)
		}

		if header.Size != file.SizeBytes {
			return errors.Errorf("integrity check failed for %s: archive header says %d bytes, manifest says %d",
				logicalPath, header.Size, file.SizeBytes)
		}
		hasher := sha256.New()
		if _, err := io.CopyN(hasher, archive, file.SizeBytes); err != nil {
			return errors.Wrapf(err, "read archive file %s", logicalPath)
		}
		actualDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if actualDigest != file.Digest {
			return errors.Errorf(
				"integrity check failed for %s: content hashes to %s, manifest says %s",
				logicalPath, actualDigest, file.Digest)
		}
		seen[logicalPath] = struct{}{}
	}

	// Preserve any legal trailing tar padding/bytes the reader did not need to
	// inspect so output remains byte-for-byte the server's stream.
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return errors.Wrap(err, "finish dataset archive")
	}
	for logicalPath := range expected {
		if _, ok := seen[logicalPath]; !ok {
			return errors.Errorf("dataset archive is missing manifest file %q", logicalPath)
		}
	}
	return nil
}

func copyDownloadedContent(dst io.Writer, body io.Reader, expectedDigest string, verify bool) error {
	writer := dst
	var hasher hashWriter
	if verify {
		hasher = sha256.New()
		writer = io.MultiWriter(dst, hasher)
	}
	if _, err := io.Copy(writer, body); err != nil {
		return errors.Wrap(err, "write download")
	}
	if verify {
		actualDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if actualDigest != expectedDigest {
			return errors.Errorf(
				"integrity check failed: content hashes to %s, manifest says %s",
				actualDigest, expectedDigest)
		}
	}
	return nil
}

// hashWriter is the subset of hash.Hash used while copying a download. Naming
// the narrow interface keeps copyDownloadedContent independent of a concrete
// digest implementation.
type hashWriter interface {
	io.Writer
	Sum([]byte) []byte
}

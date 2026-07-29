package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

// DropStreamFlag names the stream within a drop.
//
// The explicit name distinguishes the stream inside a Datadrop drop from
// output serialization. Glazed v1.4 uses JSONL as its streaming contract and
// no longer exposes a generic --stream flag, but the domain-specific spelling
// remains clearer across the seven verbs that carry it.
const DropStreamFlag = "drop-stream"

// DropStreamField is the flag itself, for the verbs that address a single
// stream rather than a whole time range.
func DropStreamField() *fields.Definition {
	return fields.New(DropStreamFlag, fields.TypeString,
		fields.WithDefault(datadrop.DefaultStream),
		fields.WithHelp("stream within the drop (was --stream before v0.2)"))
}

// ReadSpec reads a JSON document from a file or, for "-", from stdin.
//
// Used for schema documents and dataset manifests, which are the two places a
// verb takes a whole document rather than a scalar.
func ReadSpec(path string) (json.RawMessage, error) {
	if path == "-" {
		body, err := io.ReadAll(os.Stdin)
		return body, errors.Wrap(err, "read from stdin")
	}

	body, err := os.ReadFile(path) //nolint:gosec // the path is the user's own argument
	return body, errors.Wrapf(err, "read %s", path)
}

// HumanBytes renders a byte count for a diagnostic line.
//
// Exact values belong in the row; this is for a human watching an upload.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}

	div, exp := int64(unit), 0
	for size := n / unit; size >= unit && exp < 3; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

// Command hyperslop is the agent/customer-facing CLI for the datalab backend.
//
// It is a thin client of the server's HTTP API: create/push/query/tail/export,
// dataset publish/retrieve, schema put/show, the browser-approved device
// pairing flow, and whoami. It is deliberately not the server — that is the
// proprietary datalab binary. The admin datalab binary imports these same
// customer command groups from this module so the two never duplicate.
//
// See ttmp/2026/07/29/HYPERSLOP-1--*/design-doc/01-*.md for the extraction
// design.
package main

import (
	"os"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/cli/authcmd"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/cli/dataset"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/cli/drops"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/cli/events"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/cli/schemacmd"
)

// The group registrars are named here rather than inside pkg/cli, because the
// group packages import pkg/cli for the client section, the row projections and
// the exit helper. This is the one place in the tree that knows about all of
// them.
func main() {
	os.Exit(cli.Execute(
		authcmd.Register,
		drops.Register,
		events.Register,
		schemacmd.Register,
		dataset.Register,
	))
}

package cli

import (
	"strings"

	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
)

// ClientSectionSlug names the section carrying --addr and --token.
//
// It is a Glazed section rather than a pair of persistent cobra flags (DR-76)
// for three reasons: a section can be filled from a config file or a named
// profile without datalab implementing precedence itself, --print-parsed-fields
// can then say where a value came from, and the flags appear only on the
// commands that actually talk to a server. `datalab serve` used to carry a
// client --token it never read, purely because the flag was persistent on the
// root.
const ClientSectionSlug = "datalab-client"

// DefaultAddr is the server a client command talks to when nothing says
// otherwise.
const DefaultAddr = "http://localhost:8080"

// ClientSettings is the decoded form of the client section.
type ClientSettings struct {
	Addr  string `glazed:"addr"`
	Token string `glazed:"token"`
}

// NewClientSection builds the section every server-talking verb attaches.
//
// --token is fields.TypeSecret, not fields.TypeString, and that is load-bearing
// rather than decorative. The command-settings section adds --print-parsed-fields
// to every Glazed command, and glazed's PrintParsedFields dumps every resolved
// value together with the source it came from. Redaction happens in
// fields.RedactValue, which redacts only when the field's type reports
// IsSensitive() — that is, only for TypeSecret. Declared as TypeString, a
// bearer token would be printed in full, along with the environment variable it
// was read from. Declared as TypeSecret it renders as "sm***en".
//
// The same applies to the flag's default in --help, which fields.AddToCobraCommand
// redacts for sensitive types.
func NewClientSection() (schema.Section, error) {
	section, err := schema.NewSection(
		ClientSectionSlug,
		"How to reach the datalab server",
		schema.WithFields(
			fields.New(
				"addr",
				fields.TypeString,
				fields.WithDefault(DefaultAddr),
				fields.WithHelp("datalab server base URL [$"+strings.ToUpper(AppName())+"_ADDR]"),
			),
			fields.New(
				"token",
				fields.TypeSecret,
				fields.WithDefault(""),
				fields.WithHelp("bearer token [$"+strings.ToUpper(AppName())+"_TOKEN]"),
			),
		),
	)
	if err != nil {
		return nil, errors.Wrap(err, "building the datalab client section")
	}
	return section, nil
}

// ClientFrom builds an API client from the parsed client section.
//
// Never read the cobra flags directly instead of this: a value may have come
// from DATADROP_ADDR or from a config file, in which case the cobra flag is
// empty and the client would silently talk to localhost.
func ClientFrom(vals *values.Values) (*client.Client, error) {
	s := &ClientSettings{}
	if err := vals.DecodeSectionInto(ClientSectionSlug, s); err != nil {
		return nil, errors.Wrap(err, "decoding the datalab client section")
	}
	return client.New(s.Addr, s.Token)
}

// ClientSettingsFrom decodes the client section without building a client, for
// the one verb (whoami) that reports the address it used.
func ClientSettingsFrom(vals *values.Values) (*ClientSettings, error) {
	s := &ClientSettings{}
	if err := vals.DecodeSectionInto(ClientSectionSlug, s); err != nil {
		return nil, errors.Wrap(err, "decoding the datalab client section")
	}
	return s, nil
}

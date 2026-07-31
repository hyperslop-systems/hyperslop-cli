package authcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"
)

// DeviceCommand runs the agent half of the browser-approved device pairing
// flow. It deliberately uses a BareCommand: the useful result is a secret on
// stdout (or a 0600 credential file), not a Glazed table that could be cached.
type DeviceCommand struct {
	*cmds.CommandDescription
}

var _ cmds.BareCommand = &DeviceCommand{}

type deviceSettings struct {
	Addr           string   `glazed:"addr"`
	Name           string   `glazed:"name"`
	Scopes         []string `glazed:"scopes"`
	ExpiresIn      string   `glazed:"expires-in"`
	CredentialFile string   `glazed:"credential-file"`
	Timeout        string   `glazed:"timeout"`
}

// NewDeviceCommand builds `<app> auth device`.
func NewDeviceCommand() (cmds.Command, error) {
	app := ddcli.AppName()
	appUpper := strings.ToUpper(app)

	return &DeviceCommand{cmds.NewCommandDescription(
		"device",
		cmds.WithShort("Pair this agent through an approved browser session"),
		cmds.WithLong(strings.TrimSpace(`
Start a browser-approved device pairing flow and return one scoped local ddp_
token. The command prints a URL and a short code to stderr. Sign in to that URL
in a browser and verify the code; this process then prints the token exactly
once on stdout.

Capture stdout directly, never paste a ZITADEL/OIDC bearer token into Datalab:

    export `+appUpper+`_TOKEN="$(`+app+` auth device --name 'local coding agent' --scopes drops:read,drops:write,workbenches:read,workbenches:write --expires-in 24h)"

Or write an owner-only credential file:

    `+app+` auth device --credential-file ~/.config/datalab/agent.token
`)),
		cmds.WithFlags(
			fields.New("addr", fields.TypeString,
				fields.WithDefault(envOr(appUpper+"_ADDR", "http://localhost:8080")),
				fields.WithHelp("datalab server base URL [$"+appUpper+"_ADDR]")),
			fields.New("name", fields.TypeString,
				fields.WithDefault("coding agent"),
				fields.WithHelp("human-readable name shown to the approving user")),
			fields.New("scopes", fields.TypeStringList,
				fields.WithDefault([]string{"drops:read"}),
				fields.WithHelp("comma-separated operational scopes: drops:read, drops:write, datasets:write, workbenches:read, workbenches:write")),
			fields.New("expires-in", fields.TypeString,
				fields.WithDefault("24h"),
				fields.WithHelp("required token lifetime, at most 30d")),
			fields.New("credential-file", fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("write the one-time token to this owner-only file instead of stdout")),
			fields.New("timeout", fields.TypeString,
				fields.WithDefault("10m"),
				fields.WithHelp("maximum time to wait for browser approval")),
		),
	)}, nil
}

func (c *DeviceCommand) Run(ctx context.Context, vals *values.Values) error {
	settings := &deviceSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return err
	}
	if err := datalab.ValidateTokenName(settings.Name); err != nil {
		return err
	}
	scopes, err := parseScopes(settings.Scopes)
	if err != nil {
		return err
	}
	if err := datalab.ValidateDeviceScopes(scopes); err != nil {
		return err
	}
	timeout, err := time.ParseDuration(settings.Timeout)
	if err != nil || timeout <= 0 {
		return errors.Errorf("invalid --timeout %q", settings.Timeout)
	}
	// Validate and create the destination before starting the one-time flow.
	// Otherwise browser approval can consume the authorization only for the
	// command to discover afterwards that ~/.config/... does not exist or is
	// unwritable, losing the secret forever.
	if settings.CredentialFile != "" {
		if err := prepareCredentialDestination(settings.CredentialFile); err != nil {
			return err
		}
	}

	// No token yet: the whole point of the device flow is to obtain one. The
	// client sends no Authorization header when Token is empty.
	api, err := client.New(settings.Addr, "")
	if err != nil {
		return err
	}

	pairCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started, err := api.StartDeviceAuthorization(pairCtx, datalab.StartDeviceAuthorizationRequest{
		Name: settings.Name, Scopes: scopes, ExpiresIn: settings.ExpiresIn,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Open %s\nCode: %s\nWaiting for browser approval…\n",
		started.VerificationURIComplete, started.UserCode)

	token, err := poll(pairCtx, api, started)
	if err != nil {
		return err
	}

	if settings.CredentialFile != "" {
		if err := writeCredentialFile(settings.CredentialFile, token.Token); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Saved token to %s; export %s_TOKEN=$(cat %q)\n",
			settings.CredentialFile, strings.ToUpper(ddcli.AppName()), settings.CredentialFile)
		return nil
	}
	_, err = fmt.Fprintln(os.Stdout, token.Token)
	return err
}

func parseScopes(raw []string) ([]datalab.Scope, error) {
	var scopes []datalab.Scope
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				scopes = append(scopes, datalab.Scope(part))
			}
		}
	}
	return scopes, nil
}

// poll drives the RFC-8628-style polling loop. The wait interval and the
// "still pending" interpretation live here; the HTTP is the client's.
func poll(
	ctx context.Context, api *client.Client, started datalab.StartDeviceAuthorizationResponse,
) (datalab.DeviceTokenResponse, error) {
	return pollWithWait(ctx, api, started, wait)
}

func pollWithWait(
	ctx context.Context, api *client.Client, started datalab.StartDeviceAuthorizationResponse,
	waitFor func(context.Context, time.Duration) error,
) (datalab.DeviceTokenResponse, error) {
	interval := time.Duration(started.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		if err := waitFor(ctx, interval); err != nil {
			return datalab.DeviceTokenResponse{}, errors.Wrap(err, "waiting for browser approval")
		}
		token, err := api.PollDeviceToken(ctx, datalab.PollDeviceTokenRequest{DeviceCode: started.DeviceCode})
		if err == nil {
			return token, nil
		}
		var apiErr *client.APIError
		if !errors.As(err, &apiErr) {
			return datalab.DeviceTokenResponse{}, err
		}
		switch apiErr.Code {
		case "AuthorizationPending":
			continue
		case "SlowDown":
			interval += 5 * time.Second
			continue
		case "RateLimited":
			if apiErr.RetryAfter > 0 {
				interval = apiErr.RetryAfter
			}
			continue
		case "ExpiredToken":
			return datalab.DeviceTokenResponse{}, errors.New("device authorization expired before approval")
		default:
			return datalab.DeviceTokenResponse{}, errors.Errorf("device authorization failed: %s", apiErr.Detail)
		}
	}
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func prepareCredentialDestination(filePath string) error {
	parent := filepath.Dir(filePath)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return errors.Wrap(err, "create credential directory")
	}
	if info, err := os.Lstat(filePath); err == nil && info.IsDir() {
		return errors.Errorf("credential file %q is a directory", filePath)
	} else if err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "inspect credential file")
	}

	probe, err := os.CreateTemp(parent, "."+filepath.Base(filePath)+"-*.tmp")
	if err != nil {
		return errors.Wrap(err, "preflight credential file")
	}
	probeName := probe.Name()
	if err := probe.Chmod(0o600); err != nil {
		_ = probe.Close()
		_ = os.Remove(probeName)
		return errors.Wrap(err, "restrict credential file probe")
	}
	if err := probe.Close(); err != nil {
		_ = os.Remove(probeName)
		return errors.Wrap(err, "close credential file probe")
	}
	if err := os.Remove(probeName); err != nil {
		return errors.Wrap(err, "remove credential file probe")
	}
	return nil
}

func writeCredentialFile(filePath, token string) error {
	parent := filepath.Dir(filePath)
	file, err := os.CreateTemp(parent, "."+filepath.Base(filePath)+"-*.tmp")
	if err != nil {
		return errors.Wrap(err, "open temporary credential file")
	}
	tempName := file.Name()
	published := false
	defer func() {
		_ = file.Close()
		if !published {
			_ = os.Remove(tempName)
		}
	}()

	if err := file.Chmod(0o600); err != nil {
		return errors.Wrap(err, "restrict credential file")
	}
	if _, err := io.WriteString(file, token+"\n"); err != nil {
		return errors.Wrap(err, "write credential file")
	}
	if err := file.Sync(); err != nil {
		return errors.Wrap(err, "sync credential file")
	}
	if err := file.Close(); err != nil {
		return errors.Wrap(err, "close credential file")
	}
	if err := os.Rename(tempName, filePath); err != nil {
		return errors.Wrap(err, "publish credential file")
	}
	published = true
	return nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

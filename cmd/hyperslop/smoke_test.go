package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This is the hyperslop acceptance test: it builds the real datadrop server
// (from the go-go-datadrop worktree in the split-cli workspace) and the real
// hyperslop client, starts the server with a mock OIDC discovery provider, and
// drives the hyperslop binary against it over a real socket.
//
// It shells out to compiled binaries rather than calling cli.Execute in-process
// so it also covers argument parsing, exit codes, the HYPERSLOP_* env vars, and
// the "hyperslop: " diagnostic prefix — the parts of the CLI contract an
// in-process test cannot see.
//
// The authenticated data verbs (create/push/query/tail/export/dataset/schema)
// are exercised end-to-end against the same server by go-go-datadrop's
// cmd/datadrop smoke tests, which build the datadrop binary whose customer
// commands are imported from this module. This test covers what is distinct to
// the hyperslop binary: its wiring, identity and the device-pairing entry point.

// buildHyperslopClient compiles the hyperslop CLI under test.
func buildHyperslopClient(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "hyperslop")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = append(build.Environ(), "GOWORK=off", "GOFLAGS=-buildvcs=false")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build hyperslop: %v\n%s", err, out)
	}
	return binary
}

// buildDatadropServer compiles the datadrop server from the workspace. It skips
// the test when the server cannot be built — i.e. when hyperslop-cli is built
// standalone (GOWORK=off, no go-go-datadrop in the module graph), which is the
// case in hyperslop-cli's own CI. In the split-cli workspace the server builds
// and the test runs for real.
func buildDatadropServer(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "datadrop-server")
	build := exec.Command("go", "build", "-o", binary, "github.com/go-go-golems/go-go-datadrop/cmd/datadrop")
	// Use the workspace (do not set GOWORK=off) so the local go-go-datadrop
	// worktree is used. Isolate VCS stamping, which walks up into workspace
	// metadata and fails.
	build.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("datadrop server binary not buildable in this build mode (needs the split-cli workspace): %v\n%s", err, out)
	}
	return binary
}

// mockOIDCProvider serves only the discovery document the server needs to
// construct its relying party. No browser callback, no real identity provider.
func mockOIDCProvider(t *testing.T) string {
	t.Helper()
	var issuer string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/authorize",
			"token_endpoint":         issuer + "/token",
			"jwks_uri":               issuer + "/keys",
		})
	}))
	t.Cleanup(provider.Close)
	issuer = provider.URL
	return issuer
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	defer func() { _ = listener.Close() }()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	return port
}

// startServer starts the datadrop server and blocks until /healthz answers 200.
func startServer(t *testing.T, binary, issuer string) (string, func()) {
	t.Helper()
	port := freePort(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "datadrop.db")
	pepperPath := filepath.Join(dir, "device-code-pepper")
	if err := os.WriteFile(pepperPath, []byte("test-device-code-pepper"), 0o600); err != nil {
		t.Fatalf("write pepper: %v", err)
	}
	addr := "127.0.0.1:" + port
	args := []string{
		"serve", "--addr", addr, "--db", dbPath,
		"--external-url", "http://" + addr, "--oidc-issuer", issuer,
		"--oidc-client-id", "test-client", "--oidc-client-secret", "test-secret",
		"--device-code-pepper-file", pepperPath, "--seed-welcome=false", "--no-ui",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, args...)
	var logBuf bytes.Buffer
	cmd.Stdout = &logBuf
	cmd.Stderr = &logBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	cleanup := func() {
		cancel()
		_ = cmd.Wait()
	}
	t.Cleanup(cleanup)

	base := "http://" + addr
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return base, cleanup
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server did not become reachable at %s; log:\n%s", base, logBuf.String())
	return base, cleanup
}

// runClient runs the hyperslop binary and returns stdout, stderr, exit code.
func runClient(t *testing.T, binary, addr string, env []string, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	full := append([]string{"--addr", addr}, args...)
	cmd := exec.CommandContext(ctx, binary, full...)
	cmd.Env = append(os.Environ(), env...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exit := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exit = exitErr.ExitCode()
		} else {
			t.Fatalf("run hyperslop %v: %v", args, err)
		}
	}
	return outBuf.String(), errBuf.String(), exit
}

// TestHyperslopWhoamiAgainstRealServer verifies the hyperslop binary talks to a
// real datadrop server and renders the unauthenticated me row (no token), exit 0.
func TestHyperslopWhoamiAgainstRealServer(t *testing.T) {
	server := buildDatadropServer(t)
	client := buildHyperslopClient(t)
	issuer := mockOIDCProvider(t)
	base, _ := startServer(t, server, issuer)

	out, _, exit := runClient(t, client, base, nil, "whoami", "--format", "jsonl", "--output-fields", "authenticated")
	if exit != 0 {
		t.Fatalf("hyperslop whoami exited %d (want 0)", exit)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &row); err != nil {
		t.Fatalf("decode whoami jsonl %q: %v", out, err)
	}
	if row["authenticated"] != false {
		t.Errorf("whoami authenticated = %v, want false (no token presented)", row["authenticated"])
	}
}

// TestHyperslopAuthDeviceStartsAgainstRealServer verifies the device-pairing
// command hits the real server's /v1/device/authorizations, prints the approval
// URL and code, and then exits non-zero when no approval arrives before timeout.
func TestHyperslopAuthDeviceStartsAgainstRealServer(t *testing.T) {
	server := buildDatadropServer(t)
	client := buildHyperslopClient(t)
	issuer := mockOIDCProvider(t)
	base, _ := startServer(t, server, issuer)

	_, stderr, exit := runClient(t, client, base, nil, "auth", "device", "--name", "smoke agent", "--scopes", "drops:read", "--timeout", "1s")
	if exit == 0 {
		t.Fatalf("auth device exited 0; want non-zero (no approval arrived in 1s)")
	}
	if !strings.Contains(stderr, "Open ") {
		t.Errorf("auth device stderr missing the approval URL; stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Code:") {
		t.Errorf("auth device stderr missing the user code; stderr:\n%s", stderr)
	}
}

// TestHyperslopExitCodeOnUnreachableServer verifies the exit-code contract and
// the "hyperslop: " diagnostic prefix against an unreachable server.
func TestHyperslopExitCodeOnUnreachableServer(t *testing.T) {
	client := buildHyperslopClient(t)
	_, stderr, exit := runClient(t, client, "http://127.0.0.1:1", nil, "whoami")
	if exit != 1 {
		t.Fatalf("whoami against unreachable server exited %d, want 1 (ExitError)", exit)
	}
	if !strings.HasPrefix(strings.TrimSpace(stderr), "hyperslop:") {
		t.Errorf("whoami stderr missing 'hyperslop:' prefix; stderr:\n%s", stderr)
	}
}

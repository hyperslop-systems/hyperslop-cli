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

// This is the hyperslop acceptance test. It builds the REAL datadrop server
// (from the go-go-datadrop worktree in the split-cli workspace) and the real
// hyperslop client, starts the server with a mock OIDC discovery provider, and
// drives the hyperslop binary against it over a real socket — including the
// full authenticated data path (create/push/query/tail/export/schema/dataset/
// whoami) and the documented exit-code contract.
//
// It shells out to compiled binaries rather than calling cli.Execute in-process
// so it also covers argument parsing, exit codes, the HYPERSLOP_* env vars, and
// the "hyperslop: " diagnostic prefix — the parts of the CLI contract an
// in-process test cannot see.
//
// The server-dependent tests skip gracefully when the datadrop server binary
// cannot be built or a token cannot be seeded — i.e. when hyperslop-cli is
// built standalone (GOWORK=off, no go-go-datadrop in the module graph), as in
// hyperslop-cli's own CI. In the split-cli workspace they run for real.

// seederSrc is a tiny main that opens the datadrop SQLite store, creates a
// local user-owned ddp_ token (all scopes), and prints it. It is compiled and
// run with `go run` against the split-cli workspace so it can import
// go-go-datadrop/pkg/store — which hyperslop-cli itself must never import (that
// would be a cycle). It mirrors go-go-datadrop's cmd/datadrop seedSmokeToken.
const seederSrc = `package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-go-golems/go-go-datadrop/pkg/store"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: seeder <dbpath>")
		os.Exit(2)
	}
	st, err := store.Open(context.Background(), filepath.Clean(os.Args[1]))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	user, err := st.UpsertUser(context.Background(), "https://smoke-idp.example/", "smoke-user", "smoke@example.org", "Smoke User")
	if err != nil {
		fmt.Fprintln(os.Stderr, "upsert:", err)
		os.Exit(1)
	}
	created, err := st.CreateAPIToken(context.Background(), user.ID, "smoke", datadrop.AllScopes, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "createtoken:", err)
		os.Exit(1)
	}
	if err := st.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "close:", err)
		os.Exit(1)
	}
	fmt.Print(created.Token)
}
`

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
// when the server cannot be built (standalone hyperslop-cli CI, GOWORK=off).
func buildDatadropServer(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "datadrop-server")
	build := exec.Command("go", "build", "-o", binary, "github.com/go-go-golems/go-go-datadrop/cmd/datadrop")
	build.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("datadrop server binary not buildable in this build mode (needs the split-cli workspace): %v\n%s", err, out)
	}
	return binary
}

// seedToken creates a local ddp_ token in the database BEFORE the server opens
// it, by compiling and running seederSrc with `go run` against the workspace.
// Skips when go-go-datadrop/pkg/store is not resolvable (standalone CI).
func seedToken(t *testing.T, dbPath string) string {
	t.Helper()
	seeder := filepath.Join(t.TempDir(), "seeder_main.go")
	if err := os.WriteFile(seeder, []byte(seederSrc), 0o600); err != nil {
		t.Fatalf("write seeder: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", seeder, dbPath)
	cmd.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("could not seed a token (needs the split-cli workspace with go-go-datadrop): %v", err)
	}
	token := strings.TrimSpace(string(out))
	if !strings.HasPrefix(token, "ddp_") {
		t.Fatalf("seeder returned a non-token: %q", token)
	}
	return token
}

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

// startServer starts the datadrop server against dbPath (and optional blobDir)
// and blocks until /healthz answers 200.
func startServer(t *testing.T, binary, issuer, dbPath, blobDir string) string {
	t.Helper()
	port := freePort(t)
	pepperPath := filepath.Join(filepath.Dir(dbPath), "device-code-pepper")
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
	if blobDir != "" {
		args = append(args, "--blobs", blobDir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, args...)
	var logBuf bytes.Buffer
	cmd.Stdout = &logBuf
	cmd.Stderr = &logBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("server log:\n%s", logBuf.String())
		}
	})
	base := "http://" + addr
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return base
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server did not become reachable at %s; log:\n%s", base, logBuf.String())
	return base
}

// cli runs a hyperslop client subcommand with the given env and returns
// stdout, stderr, exit code.
func runCLI(t *testing.T, binary string, env []string, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
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

// mustRun runs a client subcommand and fails the test if it exits non-zero.
func mustRun(t *testing.T, binary string, env []string, args ...string) (string, string) {
	t.Helper()
	out, errOut, exit := runCLI(t, binary, env, args...)
	if exit != 0 {
		t.Fatalf("hyperslop %v exited %d (want 0)\nstdout:%s\nstderr:%s", args, exit, out, errOut)
	}
	return out, errOut
}

func decodeOneRow(t *testing.T, what, stdout string) map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &rows); err != nil {
		t.Fatalf("decode %s json %q: %v", what, stdout, err)
	}
	if len(rows) != 1 {
		t.Fatalf("%s returned %d rows, want 1: %s", what, len(rows), stdout)
	}
	return rows[0]
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestHyperslopWhoamiAgainstRealServer: anonymous whoami against the real
// server -> exit 0, authenticated=false (no token presented).
func TestHyperslopWhoamiAgainstRealServer(t *testing.T) {
	server := buildDatadropServer(t)
	client := buildHyperslopClient(t)
	issuer := mockOIDCProvider(t)
	base := startServer(t, server, issuer, filepath.Join(t.TempDir(), "datadrop.db"), "")

	out, _, exit := runCLI(t, client, nil, "--addr", base, "whoami", "--format", "json", "--output-fields", "authenticated")
	if exit != 0 {
		t.Fatalf("hyperslop whoami exited %d (want 0)", exit)
	}
	row := decodeOneRow(t, "whoami", strings.TrimSpace(out))
	if row["authenticated"] != false {
		t.Errorf("whoami authenticated = %v, want false (no token presented)", row["authenticated"])
	}
}

// TestHyperslopAuthDeviceStartsAgainstRealServer: the device-pairing command
// hits the real /v1/device/authorizations, prints the approval URL + code, and
// exits non-zero when no approval arrives before timeout.
func TestHyperslopAuthDeviceStartsAgainstRealServer(t *testing.T) {
	server := buildDatadropServer(t)
	client := buildHyperslopClient(t)
	issuer := mockOIDCProvider(t)
	base := startServer(t, server, issuer, filepath.Join(t.TempDir(), "datadrop.db"), "")

	_, stderr, exit := runCLI(t, client, nil, "--addr", base, "auth", "device", "--name", "smoke agent", "--scopes", "drops:read", "--timeout", "1s")
	if exit == 0 {
		t.Fatalf("auth device exited 0; want non-zero (no approval arrived in 1s)")
	}
	if !strings.Contains(stderr, "Open ") || !strings.Contains(stderr, "Code:") {
		t.Errorf("auth device stderr missing the approval URL/code; stderr:\n%s", stderr)
	}
}

// TestHyperslopExitCodeOnUnreachableServer: exit-code contract + "hyperslop: "
// prefix against an unreachable server.
func TestHyperslopExitCodeOnUnreachableServer(t *testing.T) {
	client := buildHyperslopClient(t)
	_, stderr, exit := runCLI(t, client, nil, "--addr", "http://127.0.0.1:1", "whoami")
	if exit != 1 {
		t.Fatalf("whoami against unreachable server exited %d, want 1 (ExitError)", exit)
	}
	if !strings.HasPrefix(strings.TrimSpace(stderr), "hyperslop:") {
		t.Errorf("whoami stderr missing 'hyperslop:' prefix; stderr:\n%s", stderr)
	}
}

// TestHyperslopFullDataPathAgainstRealServer runs the README quick start
// (create/push/query/tail/export/schema/dataset/whoami) through the hyperslop
// binary against a real datadrop server, with a seeded HYPERSLOP_TOKEN.
func TestHyperslopFullDataPathAgainstRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end smoke test in -short mode")
	}
	server := buildDatadropServer(t)
	client := buildHyperslopClient(t)
	issuer := mockOIDCProvider(t)

	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "datadrop.db")
	blobDir := filepath.Join(workDir, "blobs")
	token := seedToken(t, dbPath) // before the server opens the DB
	base := startServer(t, server, issuer, dbPath, blobDir)

	env := []string{"HYPERSLOP_ADDR=" + base, "HYPERSLOP_TOKEN=" + token}
	run := func(args ...string) (string, string) { return mustRun(t, client, env, args...) }

	// --- create ------------------------------------------------------------
	stdout, _ := run("create", "greenhouse", "--format", "json")
	if created := decodeOneRow(t, "create", stdout); created["name"] != "greenhouse" {
		t.Fatalf("create output does not name the drop: %s", stdout)
	}

	// --- push, three ways (kv, stdin json, stdin ndjson) -------------------
	run("push", "greenhouse", "temperature=21.7", "humidity=0.48")
	stdin := strings.NewReader(`{"temperature":22.8}`)
	if out, _, exit := cliWithStdin(t, client, env, stdin, "push", "greenhouse", "--stdin"); exit != 0 {
		t.Fatalf("push --stdin exited %d: %s", exit, out)
	}
	stdin2 := strings.NewReader("{\"temperature\":23.1}\n{\"temperature\":23.5}\n")
	if out, _, exit := cliWithStdin(t, client, env, stdin2, "push", "greenhouse", "--stdin", "--ndjson"); exit != 0 {
		t.Fatalf("push --stdin --ndjson exited %d: %s", exit, out)
	}

	// --- query: 4 events, typed payload columns ---------------------------
	stdout, _ = run("query", "greenhouse", "--limit", "10", "--format", "json")
	var events []map[string]any
	if err := json.Unmarshal([]byte(stdout), &events); err != nil {
		t.Fatalf("decode query output %q: %v", stdout, err)
	}
	if len(events) != 4 {
		t.Fatalf("query returned %d events, want 4", len(events))
	}
	oldest := events[len(events)-1]
	if _, isNumber := oldest["data.temperature"].(float64); !isNumber {
		t.Fatalf("temperature stored as %T, want a number: %+v", oldest["data.temperature"], oldest)
	}

	// --- tail (non-following) ---------------------------------------------
	stdout, _ = run("tail", "greenhouse", "--limit", "2")
	if !strings.Contains(stdout, "seq") {
		t.Fatalf("tail did not print a seq column/header: %s", stdout)
	}

	// --- export csv --------------------------------------------------------
	stdout, _ = run("export", "greenhouse", "--format", "csv")
	if !strings.Contains(stdout, "temperature") {
		t.Fatalf("csv export missing the temperature column: %s", stdout)
	}

	// --- schema put (strict) + show ---------------------------------------
	schemaPath := filepath.Join(workDir, "schema.json")
	writeFile(t, schemaPath, `{
		"type": "object",
		"required": ["temperature"],
		"properties": { "temperature": { "type": "number" } }
	}`)
	run("schema", "put", "greenhouse", "--file", schemaPath, "--mode", "strict")
	stdout, _ = run("schema", "show", "greenhouse", "--format", "json")
	if shown := decodeOneRow(t, "schema show", stdout); shown["mode"] != "strict" {
		t.Fatalf("schema show mode = %v, want strict", shown["mode"])
	}

	// --- dataset push + list + get ----------------------------------------
	csvPath := filepath.Join(workDir, "readings.csv")
	writeFile(t, csvPath, "temperature,humidity\n21.7,0.48\n22.8,0.50\n")
	readmePath := filepath.Join(workDir, "README.md")
	writeFile(t, readmePath, "# Greenhouse readings\n")
	stdout, stderr := run("dataset", "push", "greenhouse", "readings-2026",
		"--file", csvPath+":data/readings.csv",
		"--file", readmePath+":README.md",
		"--title", "Greenhouse readings, 2026 season",
		"--license", "CC-BY-4.0",
		"--format", "json")
	if !strings.Contains(stderr, "uploaded 2 file(s)") {
		t.Fatalf("dataset push should upload both files; stderr: %s", stderr)
	}
	if version := decodeOneRow(t, "dataset push", stdout); version["state"] != "committed" {
		t.Fatalf("dataset push state = %v, want committed", version["state"])
	}
	stdout, _ = run("dataset", "list", "greenhouse", "--format", "json")
	var datasets []map[string]any
	if err := json.Unmarshal([]byte(stdout), &datasets); err != nil {
		t.Fatalf("decode dataset list %q: %v", stdout, err)
	}
	if len(datasets) != 1 || datasets[0]["name"] != "readings-2026" {
		t.Fatalf("dataset list = %+v, want one 'readings-2026'", datasets)
	}
	outDir := filepath.Join(workDir, "out")
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatalf("mkdir out: %v", err)
	}
	run("dataset", "get", "greenhouse", "readings-2026", "--output", outDir)
	got, err := os.ReadFile(filepath.Join(outDir, "data", "readings.csv"))
	if err != nil {
		t.Fatalf("read retrieved dataset file: %v", err)
	}
	if !strings.Contains(string(got), "21.7") {
		t.Fatalf("retrieved CSV missing the data: %s", got)
	}

	// --- whoami (now authenticated) ---------------------------------------
	stdout, _ = run("whoami", "--format", "json", "--output-fields", "authenticated,user_id")
	row := decodeOneRow(t, "whoami", stdout)
	if row["authenticated"] != true {
		t.Fatalf("whoami authenticated = %v, want true (token presented)", row["authenticated"])
	}
	if row["user_id"] == "" {
		t.Fatalf("whoami user_id empty: %s", stdout)
	}
}

// TestHyperslopExitCodeContract: the documented exit codes (auth=3,
// not-found=4, validation=5) through the hyperslop binary.
func TestHyperslopExitCodeContract(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end smoke test in -short mode")
	}
	server := buildDatadropServer(t)
	client := buildHyperslopClient(t)
	issuer := mockOIDCProvider(t)
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "datadrop.db")
	token := seedToken(t, dbPath)
	base := startServer(t, server, issuer, dbPath, "")

	authed := []string{"HYPERSLOP_ADDR=" + base, "HYPERSLOP_TOKEN=" + token}
	wrong := []string{"HYPERSLOP_ADDR=" + base, "HYPERSLOP_TOKEN=ddp_dead_dead_dead"}

	mustRun(t, client, authed, "create", "greenhouse")
	schemaPath := filepath.Join(workDir, "schema.json")
	writeFile(t, schemaPath, `{"type":"object","required":["temperature"],"properties":{"temperature":{"type":"number"}}}`)
	mustRun(t, client, authed, "schema", "put", "greenhouse", "--file", schemaPath, "--mode", "strict")

	cases := []struct {
		name string
		env  []string
		args []string
		want int
	}{
		{"bad credentials exit 3 (auth)", wrong, []string{"query", "greenhouse"}, 3},
		{"unknown drop exits 4 (not found)", authed, []string{"query", "nosuchdrop"}, 4},
		{"strict schema rejection exits 5 (validation)", authed, []string{"push", "greenhouse", "temperature=warm"}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, code := runCLI(t, client, tc.env, tc.args...)
			if code != tc.want {
				t.Fatalf("hyperslop %v exited %d, want %d", tc.args, code, tc.want)
			}
		})
	}
}

// cliWithStdin runs a client subcommand with the given stdin.
func cliWithStdin(t *testing.T, binary string, env []string, stdin interface {
	Read([]byte) (int, error)
}, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = stdin
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

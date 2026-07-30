package uicmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	workbenchv1 "github.com/hyperslop-systems/pbui/gen/go/hyperslop/pbui/workbench/v1"
	"github.com/hyperslop-systems/pbui/pkg/workbenchapi"
)

func TestRegisterExposesWorkbenchVerbsAndOutputFlags(t *testing.T) {
	root := &cobra.Command{Use: "hyperslop"}
	if err := Register(root); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ui, _, err := root.Find([]string{"ui"})
	if err != nil {
		t.Fatalf("find ui: %v", err)
	}
	for _, name := range []string{"list", "get", "create", "replace", "mutate", "delete"} {
		command, _, err := ui.Find([]string{name})
		if err != nil || command == ui {
			t.Errorf("ui %s is not registered: %v", name, err)
			continue
		}
		for _, flag := range []string{"format", "output-fields", "max-output-rows"} {
			if command.Flags().Lookup(flag) == nil {
				t.Errorf("ui %s has no --%s flag", name, flag)
			}
		}
	}
	mutate, _, _ := ui.Find([]string{"mutate"})
	for _, flag := range []string{"file", "revision", "request-id", "addr", "token"} {
		if mutate.Flags().Lookup(flag) == nil {
			t.Errorf("ui mutate has no --%s flag", flag)
		}
	}
}

func TestListCommandEmitsStructuredWorkbenchRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workbenches" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer command-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		data, err := workbenchapi.Marshal(&workbenchv1.ListWorkbenchesResponse{
			Workbenches: []*workbenchv1.WorkbenchSummary{{
				Id: "bench-1", Name: "Operations", Revision: 11,
			}},
		})
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()

	root := &cobra.Command{Use: "hyperslop", SilenceUsage: true, SilenceErrors: true}
	if err := Register(root); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{
		"ui", "list",
		"--addr", server.URL,
		"--token", "command-token",
		"--format", "json",
	})
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write
	executeErr := root.Execute()
	_ = write.Close()
	os.Stdout = previous
	output, readErr := io.ReadAll(read)
	_ = read.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if executeErr != nil {
		t.Fatalf("Execute: %v\n%s", executeErr, output)
	}
	for _, expected := range []string{`"id": "bench-1"`, `"name": "Operations"`, `"revision": "11"`} {
		if !strings.Contains(string(output), expected) {
			t.Errorf("output %q does not contain %q", output, expected)
		}
	}
}

func TestRevisionRejectsZeroAndNonNumericValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "abc"} {
		if _, err := parseRevision(value); err == nil {
			t.Errorf("parseRevision(%q) succeeded", value)
		}
	}
	if got, err := parseRevision("18446744073709551615"); err != nil || got != ^uint64(0) {
		t.Fatalf("maximum uint64 = %d, %v", got, err)
	}
}

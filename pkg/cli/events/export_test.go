package events

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/pkg/errors"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

func TestExportQueryHonorsDirectionalCursorOrder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		order  string
		after  int64
		before int64
	}{
		{"ascending after", string(datadrop.OrderAsc), 10, 0},
		{"descending before", string(datadrop.OrderDesc), 0, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := exportSettings{
				Drop: "greenhouse",
				rangeSettings: rangeSettings{
					Order: tc.order, After: tc.after, Before: tc.before, Limit: datadrop.MaxLimit,
				},
			}
			q, err := s.query(s.Drop)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if string(q.Order) != tc.order || q.After != tc.after || q.Before != tc.before {
				t.Fatalf("query = %+v, want order=%s after=%d before=%d", q, tc.order, tc.after, tc.before)
			}
		})
	}
}

func TestPublishExportFilePreservesExistingFileOnTransferFailure(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "events.ndjson")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	readErr := errors.New("connection reset")
	body := io.MultiReader(strings.NewReader("partial\n"), iotest.ErrReader(readErr))
	if err := publishExportFile(target, body); !errors.Is(err, readErr) {
		t.Fatalf("publishExportFile error = %v, want wrapped %v", err, readErr)
	}
	assertExportContent(t, target, "original\n")
	assertNoExportTemps(t, directory)
}

func TestPublishExportFileReplacesOnlyAfterCompleteTransfer(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "events.csv")
	if err := os.WriteFile(target, []byte("old\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := publishExportFile(target, strings.NewReader("seq,value\n1,new\n")); err != nil {
		t.Fatalf("publishExportFile: %v", err)
	}
	assertExportContent(t, target, "seq,value\n1,new\n")
	assertNoExportTemps(t, directory)
}

func assertExportContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func assertNoExportTemps(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Errorf("temporary export remains: %s", entry.Name())
		}
	}
}

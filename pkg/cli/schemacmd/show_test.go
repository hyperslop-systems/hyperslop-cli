package schemacmd

import (
	"strings"
	"testing"
)

func TestShowHelpExtractsTheSchemaDocument(t *testing.T) {
	command, err := NewShowCommand()
	if err != nil {
		t.Fatalf("NewShowCommand: %v", err)
	}
	long := command.Description().Long
	if !strings.Contains(long, "jq '.spec'") {
		t.Fatalf("show help has no explicit schema extraction: %s", long)
	}
	if strings.Count(long, "--format") != 1 {
		t.Fatalf("show round-trip example has conflicting format flags: %s", long)
	}
}

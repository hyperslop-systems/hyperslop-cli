package dataset

import "testing"

func TestImportLimitDescriptionDoesNotReportZeroAsDefault(t *testing.T) {
	if got := importLimitDescription(0); got != "the server's row limit" {
		t.Fatalf("importLimitDescription(0) = %q, want the server default without a bogus zero", got)
	}
	if got := importLimitDescription(250); got != "the 250-row limit" {
		t.Fatalf("importLimitDescription(250) = %q, want explicit limit", got)
	}
}

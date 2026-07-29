package jsondoc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValuePreservesNumericLexemes(t *testing.T) {
	raw := []byte(`{"integer":9007199254740993,"decimal":0.123456789012345678901}`)
	value, err := Value(raw)
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, exact := range []string{"9007199254740993", "0.123456789012345678901"} {
		if !strings.Contains(string(encoded), exact) {
			t.Errorf("round trip %s lost %s", encoded, exact)
		}
	}
}

func TestDecodeRequiresExactlyOneDocument(t *testing.T) {
	for _, raw := range []string{"", `{"a":1}{"b":2}`, `{"a":1} garbage`} {
		if _, err := Value([]byte(raw)); err == nil {
			t.Errorf("Value accepted %q", raw)
		}
	}
}

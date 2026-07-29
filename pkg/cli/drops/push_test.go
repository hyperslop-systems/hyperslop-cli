package drops

import (
	"encoding/json"
	"testing"
)

// payloadFromFields is where a user's shell input becomes typed JSON, and the
// rule is a heuristic: parse the value as JSON when it is valid JSON, treat it
// as a string otherwise. These cases pin down what that means in practice.
func TestPayloadFromFieldsTypesValues(t *testing.T) {
	cases := map[string]struct {
		fields []string
		want   map[string]any
	}{
		"numbers stay numbers": {
			fields: []string{"temperature=21.7", "count=3"},
			want:   map[string]any{"temperature": 21.7, "count": float64(3)},
		},
		"bare words become strings": {
			fields: []string{"note=hello"},
			want:   map[string]any{"note": "hello"},
		},
		"booleans and null are parsed": {
			fields: []string{"ok=true", "off=false", "missing=null"},
			want:   map[string]any{"ok": true, "off": false, "missing": nil},
		},
		"arrays are parsed": {
			fields: []string{`tags=["a","b"]`},
			want:   map[string]any{"tags": []any{"a", "b"}},
		},
		"objects are parsed": {
			fields: []string{`location={"lat":52.5}`},
			want:   map[string]any{"location": map[string]any{"lat": 52.5}},
		},
		"an explicitly quoted number stays a string": {
			fields: []string{`raw="21.7"`},
			want:   map[string]any{"raw": "21.7"},
		},
		"a value containing = keeps the remainder": {
			fields: []string{"equation=a=b"},
			want:   map[string]any{"equation": "a=b"},
		},
		"an empty value is an empty string": {
			fields: []string{"note="},
			want:   map[string]any{"note": ""},
		},
		"negative numbers are numbers": {
			fields: []string{"offset=-3"},
			want:   map[string]any{"offset": float64(-3)},
		},
		"exponent notation is a number": {
			fields: []string{"big=1e3"},
			want:   map[string]any{"big": float64(1000)},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			encoded, err := payloadFromFields(tc.fields, nil)
			if err != nil {
				t.Fatalf("payloadFromFields: %v", err)
			}

			var got map[string]any
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatalf("decode %s: %v", encoded, err)
			}
			assertPayload(t, got, tc.want)
		})
	}
}

// --string is the escape hatch for when the heuristic guesses wrong.
func TestPayloadFromFieldsStringOverride(t *testing.T) {
	encoded, err := payloadFromFields(nil, []string{"temperature=21.7", "flag=true"})
	if err != nil {
		t.Fatalf("payloadFromFields: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("decode %s: %v", encoded, err)
	}
	assertPayload(t, got, map[string]any{"temperature": "21.7", "flag": "true"})
}

// A positional key=value wins over a --string with the same key, so the more
// specific argument on the command line is the one that takes effect.
func TestPayloadFromFieldsPositionalOverridesString(t *testing.T) {
	encoded, err := payloadFromFields([]string{"temperature=21.7"}, []string{"temperature=warm"})
	if err != nil {
		t.Fatalf("payloadFromFields: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("decode %s: %v", encoded, err)
	}
	if got["temperature"] != 21.7 {
		t.Fatalf("temperature = %v (%T), want the positional 21.7", got["temperature"], got["temperature"])
	}
}

func TestPayloadFromFieldsRejectsMalformedArguments(t *testing.T) {
	for reason, fields := range map[string][]string{
		"no separator": {"temperature"},
		"empty key":    {"=21.7"},
		"empty string": {""},
	} {
		if _, err := payloadFromFields(fields, nil); err == nil {
			t.Errorf("payloadFromFields accepted an argument with %s: %q", reason, fields)
		}
	}
}

func TestSplitField(t *testing.T) {
	key, value, err := splitField("a=b=c")
	if err != nil {
		t.Fatalf("splitField: %v", err)
	}
	if key != "a" || value != "b=c" {
		t.Fatalf("splitField(%q) = (%q, %q), want (\"a\", \"b=c\")", "a=b=c", key, value)
	}
}

// The envelope is only constructed when a CloudEvents attribute was set;
// otherwise the payload goes out bare, keeping the simple path simple.
func TestEnvelopeOverridesWrap(t *testing.T) {
	payload := json.RawMessage(`{"temperature":21.7}`)

	bare, isEnvelope, err := envelopeOverrides{}.wrap(payload)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if isEnvelope {
		t.Fatal("an empty override set produced an envelope")
	}
	if string(bare) != string(payload) {
		t.Fatalf("payload was rewritten to %s", bare)
	}

	wrapped, isEnvelope, err := envelopeOverrides{
		source: "device:sensor-7", eventType: "io.example.reading", subject: "zone-a",
	}.wrap(payload)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if !isEnvelope {
		t.Fatal("overrides did not produce an envelope")
	}

	var envelope map[string]any
	if err := json.Unmarshal(wrapped, &envelope); err != nil {
		t.Fatalf("decode envelope %s: %v", wrapped, err)
	}
	for key, want := range map[string]any{
		"specversion": "1.0",
		"source":      "device:sensor-7",
		"type":        "io.example.reading",
		"subject":     "zone-a",
	} {
		if envelope[key] != want {
			t.Errorf("envelope[%q] = %v, want %v", key, envelope[key], want)
		}
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok || data["temperature"] != 21.7 {
		t.Fatalf("envelope data = %v, want the original payload", envelope["data"])
	}
}

// Setting only one attribute is enough to switch to envelope mode, and the
// unset attributes must be omitted rather than sent empty.
func TestEnvelopeOverridesOmitsUnsetAttributes(t *testing.T) {
	wrapped, isEnvelope, err := envelopeOverrides{source: "device:sensor-7"}.
		wrap(json.RawMessage(`{"n":1}`))
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if !isEnvelope {
		t.Fatal("a single override did not produce an envelope")
	}

	var envelope map[string]any
	if err := json.Unmarshal(wrapped, &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"type", "subject"} {
		if _, present := envelope[key]; present {
			t.Errorf("unset attribute %q was sent anyway", key)
		}
	}
}

func assertPayload(t *testing.T, got, want map[string]any) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("payload has %d keys, want %d: %+v", len(got), len(want), got)
	}
	for key, wantValue := range want {
		gotValue, present := got[key]
		if !present {
			t.Fatalf("payload is missing key %q: %+v", key, got)
		}

		wantJSON, _ := json.Marshal(wantValue)
		gotJSON, _ := json.Marshal(gotValue)
		if string(wantJSON) != string(gotJSON) {
			t.Errorf("payload[%q] = %s (%T), want %s (%T)",
				key, gotJSON, gotValue, wantJSON, wantValue)
		}
	}
}

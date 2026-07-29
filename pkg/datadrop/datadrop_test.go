package datadrop

import (
	"strings"
	"testing"
	"time"
)

func TestValidateTokenNameCountsUnicodeCharacters(t *testing.T) {
	if err := ValidateTokenName(strings.Repeat("界", 100)); err != nil {
		t.Fatalf("100-character CJK token name rejected: %v", err)
	}
	err := ValidateTokenName(strings.Repeat("界", 101))
	if err == nil || !strings.Contains(err.Error(), "101 characters") {
		t.Fatalf("101-character token name error = %v", err)
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"a", "greenhouse", "zone-a", "sensor_7", "abc123", "0start",
		strings.Repeat("a", 63)} // 63 chars is the documented maximum
	for _, name := range valid {
		if err := ValidateName("drop", name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := map[string]string{
		"empty":            "",
		"uppercase":        "Greenhouse",
		"leading dash":     "-leading",
		"leading under":    "_leading",
		"space":            "has space",
		"slash":            "has/slash",
		"dot":              "has.dot",
		"trailing newline": "greenhouse\n",
		"too long":         strings.Repeat("a", 64),
	}
	for reason, name := range invalid {
		if err := ValidateName("drop", name); err == nil {
			t.Errorf("ValidateName(%q) accepted a name with a %s", name, reason)
		}
	}
}

// The error message must name what was rejected, so a CLI user can fix it.
func TestValidateNameErrorMentionsTheKind(t *testing.T) {
	err := ValidateName("stream", "Bad Name")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); !strings.Contains(got, "stream") || !strings.Contains(got, "Bad Name") {
		t.Fatalf("error %q should name both the kind and the value", got)
	}
}

func TestNormalizeStream(t *testing.T) {
	for input, want := range map[string]string{
		"":        DefaultStream,
		"   ":     DefaultStream,
		"events":  "events",
		"alerts":  "alerts",
		" alerts": "alerts",
	} {
		if got := NormalizeStream(input); got != want {
			t.Errorf("NormalizeStream(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateDatasetPathRejectsRootMarker(t *testing.T) {
	for _, valid := range []string{"data.csv", "data/readings.csv", "a/.hidden"} {
		if err := ValidateDatasetPath(valid); err != nil {
			t.Errorf("ValidateDatasetPath(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{"", ".", "a/.", "../a", "/absolute", `a\\b`} {
		if err := ValidateDatasetPath(invalid); err == nil {
			t.Errorf("ValidateDatasetPath(%q) accepted an unusable path", invalid)
		}
	}
}

func TestValidateRetention(t *testing.T) {
	for _, value := range []string{"", "1s", "30m", "24h", "90d", "2w", "5y", "999d"} {
		if err := ValidateRetention(value); err != nil {
			t.Errorf("ValidateRetention(%q) = %v, want nil", value, err)
		}
	}

	for _, value := range []string{"0d", "90", "d", "-1d", "90days", "1.5d", "90D"} {
		if err := ValidateRetention(value); err == nil {
			t.Errorf("ValidateRetention(%q) accepted an invalid window", value)
		}
	}
}

func TestParseExpiresInRejectsDurationOverflow(t *testing.T) {
	now := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	if _, err := ParseExpiresIn("2562048h", now); err == nil {
		t.Fatal("ParseExpiresIn accepted a duration that overflows time.Duration")
	}
	deadline, err := ParseExpiresIn("2562047h", now)
	if err != nil {
		t.Fatalf("ParseExpiresIn rejected the largest whole-hour duration: %v", err)
	}
	if deadline == nil || !deadline.After(now) {
		t.Fatalf("deadline = %v, want a future time", deadline)
	}
}

func TestParseMode(t *testing.T) {
	for input, want := range map[string]Mode{
		"":           ModeStrict, // an unspecified mode defaults to the safe one
		"strict":     ModeStrict,
		"permissive": ModePermissive,
	} {
		got, err := ParseMode(input)
		if err != nil {
			t.Fatalf("ParseMode(%q) = %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseMode(%q) = %q, want %q", input, got, want)
		}
	}

	// "observed"/"suggest"/"warn" are upstream-design vocabulary that v0.1 does
	// not implement; they must be rejected rather than silently mapped.
	for _, input := range []string{"observed", "suggest", "warn", "open", "Strict"} {
		if _, err := ParseMode(input); err == nil {
			t.Errorf("ParseMode(%q) accepted an unimplemented mode", input)
		}
	}
}

func TestParseOrder(t *testing.T) {
	for input, want := range map[string]Order{
		"":     OrderDesc, // a bare query is a "latest N"
		"desc": OrderDesc,
		"asc":  OrderAsc,
	} {
		got, err := ParseOrder(input)
		if err != nil {
			t.Fatalf("ParseOrder(%q) = %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseOrder(%q) = %q, want %q", input, got, want)
		}
	}

	for _, input := range []string{"sideways", "ASC", "ascending", "1"} {
		if _, err := ParseOrder(input); err == nil {
			t.Errorf("ParseOrder(%q) accepted an invalid order", input)
		}
	}
}

// ParseTimeField is a security boundary, not a convenience: its result is
// interpolated into SQL, so anything outside the allowlist must be rejected.
func TestParseTimeFieldIsAnAllowlist(t *testing.T) {
	for input, want := range map[string]TimeField{
		"":            TimeFieldTime,
		"time":        TimeFieldTime,
		"received_at": TimeFieldReceivedAt,
	} {
		got, err := ParseTimeField(input)
		if err != nil {
			t.Fatalf("ParseTimeField(%q) = %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseTimeField(%q) = %q, want %q", input, got, want)
		}
	}

	for _, injection := range []string{
		"seq",
		"time; DROP TABLE events",
		"time)) OR 1=1 --",
		"received_at, data",
		"TIME",
	} {
		if _, err := ParseTimeField(injection); err == nil {
			t.Errorf("ParseTimeField(%q) escaped the allowlist", injection)
		}
	}
}

func TestEventQueryNormalizeAppliesDefaults(t *testing.T) {
	q := EventQuery{Drop: "greenhouse"}
	if err := q.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if q.Stream != DefaultStream {
		t.Errorf("Stream = %q, want %q", q.Stream, DefaultStream)
	}
	if q.Limit != DefaultLimit {
		t.Errorf("Limit = %d, want %d", q.Limit, DefaultLimit)
	}
	if q.Order != OrderDesc {
		t.Errorf("Order = %q, want %q", q.Order, OrderDesc)
	}
	if q.TimeField != TimeFieldTime {
		t.Errorf("TimeField = %q, want %q", q.TimeField, TimeFieldTime)
	}
}

// The limit is clamped server-side regardless of what a client asks for.
func TestEventQueryNormalizeClampsLimit(t *testing.T) {
	for input, want := range map[int]int{
		-1:            DefaultLimit,
		0:             DefaultLimit,
		1:             1,
		MaxLimit:      MaxLimit,
		MaxLimit + 1:  MaxLimit,
		1_000_000_000: MaxLimit,
	} {
		q := EventQuery{Drop: "greenhouse", Limit: input}
		if err := q.Normalize(); err != nil {
			t.Fatalf("Normalize(limit=%d): %v", input, err)
		}
		if q.Limit != want {
			t.Errorf("limit %d clamped to %d, want %d", input, q.Limit, want)
		}
	}
}

func TestEventQueryNormalizeRejectsInvalidInput(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	cases := map[string]EventQuery{
		"missing drop":     {},
		"invalid drop":     {Drop: "Not Valid"},
		"invalid stream":   {Drop: "greenhouse", Stream: "Not Valid"},
		"invalid order":    {Drop: "greenhouse", Order: "sideways"},
		"invalid field":    {Drop: "greenhouse", TimeField: "seq"},
		"inverted range":   {Drop: "greenhouse", From: base.Add(time.Hour), To: base},
		"zero-width range": {Drop: "greenhouse", From: base, To: base},
		"after descending": {Drop: "greenhouse", After: 10, Order: OrderDesc},
		"before ascending": {Drop: "greenhouse", Before: 10, Order: OrderAsc},
		"both cursors":     {Drop: "greenhouse", After: 10, Before: 20, Order: OrderAsc},
	}

	for reason, q := range cases {
		if err := q.Normalize(); err == nil {
			t.Errorf("Normalize accepted a query with a %s: %+v", reason, q)
		}
	}
}

// An open-ended range is legal: only one bound need be set.
func TestEventQueryNormalizeAllowsOpenEndedRanges(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	for reason, q := range map[string]EventQuery{
		"from only": {Drop: "greenhouse", From: base},
		"to only":   {Drop: "greenhouse", To: base},
	} {
		if err := q.Normalize(); err != nil {
			t.Errorf("Normalize rejected a %s range: %v", reason, err)
		}
	}
}

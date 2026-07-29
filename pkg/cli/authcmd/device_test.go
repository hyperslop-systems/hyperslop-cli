package authcmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datadrop"
)

func TestPollRetriesRateLimitedResponseAfterRetryAfter(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"code":   "RateLimited",
				"detail": "too many device authorization requests",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(datadrop.DeviceTokenResponse{Token: "ddp_test_secret", TokenID: "tok_test"})
	}))
	defer server.Close()

	api, err := client.New(server.URL, "")
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	var waits []time.Duration
	token, err := pollWithWait(context.Background(), api,
		datadrop.StartDeviceAuthorizationResponse{DeviceCode: "device-code", Interval: 5},
		func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if token.Token != "ddp_test_secret" || requests != 2 {
		t.Fatalf("poll result = %+v after %d requests", token, requests)
	}
	if want := []time.Duration{5 * time.Second, 7 * time.Second}; len(waits) != len(want) || waits[0] != want[0] || waits[1] != want[1] {
		t.Fatalf("poll waits = %v, want %v", waits, want)
	}
}

package authcmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperslop-systems/hyperslop-cli/pkg/client"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"
)

func TestPrepareCredentialDestinationCreatesParentsBeforeWriting(t *testing.T) {
	credentialPath := filepath.Join(t.TempDir(), "nested", "config", "agent.token")
	if err := prepareCredentialDestination(credentialPath); err != nil {
		t.Fatalf("prepareCredentialDestination: %v", err)
	}
	parentInfo, err := os.Stat(filepath.Dir(credentialPath))
	if err != nil {
		t.Fatalf("Stat parent: %v", err)
	}
	if got := parentInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("credential parent mode = %o, want 700", got)
	}
	if _, err := os.Stat(credentialPath); !os.IsNotExist(err) {
		t.Fatalf("preflight created the final credential file: %v", err)
	}

	if err := writeCredentialFile(credentialPath, "ddp_first_secret"); err != nil {
		t.Fatalf("writeCredentialFile: %v", err)
	}
	if err := writeCredentialFile(credentialPath, "ddp_replacement_secret"); err != nil {
		t.Fatalf("replace credential file: %v", err)
	}
	content, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "ddp_replacement_secret\n" {
		t.Fatalf("credential content = %q", content)
	}
	fileInfo, err := os.Stat(credentialPath)
	if err != nil {
		t.Fatalf("Stat credential: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("credential mode = %o, want 600", got)
	}
}

func TestPrepareCredentialDestinationRejectsDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := prepareCredentialDestination(directory); err == nil {
		t.Fatal("prepareCredentialDestination accepted a directory as a credential file")
	}
}

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
		_ = json.NewEncoder(w).Encode(datalab.DeviceTokenResponse{Token: "ddp_test_secret", TokenID: "tok_test"})
	}))
	defer server.Close()

	api, err := client.New(server.URL, "")
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	var waits []time.Duration
	token, err := pollWithWait(context.Background(), api,
		datalab.StartDeviceAuthorizationResponse{DeviceCode: "device-code", Interval: 5},
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

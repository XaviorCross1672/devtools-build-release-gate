package rollout

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSetRetriesRateLimitWithSameIdempotencyKey(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "release-92:set" {
			t.Errorf("Idempotency-Key = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"default_value":false`) {
			t.Errorf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"data":null,"error":{"code":"","message":"retry later"},"metadata":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{},"error":null,"metadata":{}}`))
	}))
	defer server.Close()

	client := NewFlagsClient(server.URL, "test-key", server.Client())
	client.sleep = func(context.Context, time.Duration) error { return nil }
	if err := client.Set(context.Background(), "devtools.symbol-index", false, "release-92:set"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestGetParsesDefaultValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"key":"devtools.symbol-index","enabled":true,"default_value":false},"error":null,"metadata":{}}`))
	}))
	defer server.Close()

	client := NewFlagsClient(server.URL, "test-key", server.Client())
	flag, err := client.Get(context.Background(), "devtools.symbol-index")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if flag.DefaultValue {
		t.Fatal("DefaultValue = true, want false")
	}
}

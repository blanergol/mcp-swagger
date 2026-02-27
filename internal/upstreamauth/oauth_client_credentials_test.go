package upstreamauth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestOAuthClientCredentialsProviderCachingAndRefresh проверяет ожидаемое поведение в тестовом сценарии.
func TestOAuthClientCredentialsProviderCachingAndRefresh(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)

		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("client:secret"))
		if got := r.Header.Get("Authorization"); got != expectedAuth {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.PostForm.Get("grant_type"); got != "client_credentials" {
			t.Fatalf("unexpected grant_type: %s", got)
		}
		if got := r.PostForm.Get("scope"); got != "read write" {
			t.Fatalf("unexpected scope: %s", got)
		}
		if got := r.PostForm.Get("audience"); got != "api://upstream" {
			t.Fatalf("unexpected audience: %s", got)
		}

		response := map[string]any{
			"access_token": "token-" + time.Now().Format("150405.000"),
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider, err := NewOAuthClientCredentialsProvider(OAuthClientCredentialsOptions{
		TokenURL:     server.URL,
		ClientID:     "client",
		ClientSecret: "secret",
		Scopes:       "read write",
		Audience:     "api://upstream",
		CacheTTL:     120 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	req1, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err := provider.Apply(req1); err != nil {
		t.Fatalf("apply 1: %v", err)
	}
	first := req1.Header.Get("Authorization")
	if first == "" {
		t.Fatal("expected authorization header on first apply")
	}

	req2, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err := provider.Apply(req2); err != nil {
		t.Fatalf("apply 2: %v", err)
	}
	second := req2.Header.Get("Authorization")
	if second != first {
		t.Fatalf("expected cached token, got %q != %q", second, first)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 token call, got %d", got)
	}

	time.Sleep(150 * time.Millisecond)

	req3, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err := provider.Apply(req3); err != nil {
		t.Fatalf("apply 3: %v", err)
	}
	third := req3.Header.Get("Authorization")
	if third == first {
		t.Fatalf("expected refreshed token, got same value %q", third)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 token calls after refresh, got %d", got)
	}
}

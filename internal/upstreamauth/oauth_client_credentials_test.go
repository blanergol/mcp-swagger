package upstreamauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testRoundTrip func(*http.Request) (*http.Response, error)

func (f testRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("broken body") }
func (errBody) Close() error             { return nil }

// TestNewOAuthClientCredentialsProviderValidationMatrix protects constructor validation and defaults.
func TestNewOAuthClientCredentialsProviderValidationMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    OAuthClientCredentialsOptions
		wantErr bool
	}{
		{name: "missing token url", opts: OAuthClientCredentialsOptions{ClientID: "id", ClientSecret: "secret"}, wantErr: true},
		{name: "missing client id", opts: OAuthClientCredentialsOptions{TokenURL: "https://issuer/token", ClientSecret: "secret"}, wantErr: true},
		{name: "missing client secret", opts: OAuthClientCredentialsOptions{TokenURL: "https://issuer/token", ClientID: "id"}, wantErr: true},
		{name: "valid minimal", opts: OAuthClientCredentialsOptions{TokenURL: "https://issuer/token", ClientID: "id", ClientSecret: "secret"}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			provider, err := NewOAuthClientCredentialsProvider(tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected constructor error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected constructor error: %v", err)
			}
			if provider.httpClient == nil {
				t.Fatalf("provider must initialize http client")
			}
		})
	}
}

// TestApplyNilRequestNoop protects contract that Apply(nil) is a no-op.
func TestApplyNilRequestNoop(t *testing.T) {
	t.Parallel()

	provider, err := NewOAuthClientCredentialsProvider(OAuthClientCredentialsOptions{
		TokenURL:     "https://issuer/token",
		ClientID:     "id",
		ClientSecret: "secret",
		HTTPClient:   &http.Client{Transport: testRoundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("must not call") })},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := provider.Apply(nil); err != nil {
		t.Fatalf("Apply(nil) must not fail: %v", err)
	}
}

// TestOAuthClientCredentialsProviderCachingAndRefresh protects caching/refresh contract without sleeps.
func TestOAuthClientCredentialsProviderCachingAndRefresh(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
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
			"access_token": "token-" + strconv.Itoa(int(call)),
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
		CacheTTL:     2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	now := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	provider.now = func() time.Time { return now }

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

	now = now.Add(3 * time.Minute)
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

// TestFetchTokenDependencyErrorMatrix protects dependency-error handling from HTTP/token endpoint/parsing layers.
func TestFetchTokenDependencyErrorMatrix(t *testing.T) {
	t.Parallel()

	t.Run("transport error", func(t *testing.T) {
		provider := mustProvider(t, OAuthClientCredentialsOptions{
			TokenURL:     "https://issuer/token",
			ClientID:     "id",
			ClientSecret: "secret",
			HTTPClient: &http.Client{Transport: testRoundTrip(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial failure")
			})},
		})
		_, _, err := provider.fetchToken(context.Background())
		if err == nil || !strings.Contains(err.Error(), "dial failure") {
			t.Fatalf("expected transport error, got %v", err)
		}
	})

	t.Run("status 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer server.Close()

		provider := mustProvider(t, OAuthClientCredentialsOptions{TokenURL: server.URL, ClientID: "id", ClientSecret: "secret"})
		_, _, err := provider.fetchToken(context.Background())
		if err == nil || !strings.Contains(err.Error(), "status 500") {
			t.Fatalf("expected status error, got %v", err)
		}
	})

	t.Run("response body read error", func(t *testing.T) {
		provider := mustProvider(t, OAuthClientCredentialsOptions{
			TokenURL:     "https://issuer/token",
			ClientID:     "id",
			ClientSecret: "secret",
			HTTPClient: &http.Client{Transport: testRoundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: errBody{}, Header: make(http.Header)}, nil
			})},
		})
		_, _, err := provider.fetchToken(context.Background())
		if err == nil || !strings.Contains(err.Error(), "broken body") {
			t.Fatalf("expected body read error, got %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "{")
		}))
		defer server.Close()

		provider := mustProvider(t, OAuthClientCredentialsOptions{TokenURL: server.URL, ClientID: "id", ClientSecret: "secret"})
		_, _, err := provider.fetchToken(context.Background())
		if err == nil || !strings.Contains(err.Error(), "decode token response") {
			t.Fatalf("expected decode error, got %v", err)
		}
	})

	t.Run("missing access token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"token_type":"Bearer","expires_in":3600}`)
		}))
		defer server.Close()

		provider := mustProvider(t, OAuthClientCredentialsOptions{TokenURL: server.URL, ClientID: "id", ClientSecret: "secret"})
		_, _, err := provider.fetchToken(context.Background())
		if err == nil || !strings.Contains(err.Error(), "missing access_token") {
			t.Fatalf("expected missing token error, got %v", err)
		}
	})

	t.Run("context canceled", func(t *testing.T) {
		provider := mustProvider(t, OAuthClientCredentialsOptions{
			TokenURL:     "https://issuer/token",
			ClientID:     "id",
			ClientSecret: "secret",
			HTTPClient: &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
				<-req.Context().Done()
				return nil, req.Context().Err()
			})},
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := provider.fetchToken(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})
}

// TestParseExpiresInMatrix protects expires_in parsing from multiple JSON-compatible types.
func TestParseExpiresInMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want time.Duration
	}{
		{name: "nil", in: nil, want: 0},
		{name: "float64", in: float64(60), want: 60 * time.Second},
		{name: "float32", in: float32(30), want: 30 * time.Second},
		{name: "int", in: int(10), want: 10 * time.Second},
		{name: "int64", in: int64(20), want: 20 * time.Second},
		{name: "json number", in: json.Number("45"), want: 45 * time.Second},
		{name: "json number invalid", in: json.Number("4.5"), want: 0},
		{name: "string int", in: "120", want: 120 * time.Second},
		{name: "string invalid", in: "abc", want: 0},
		{name: "unsupported bool", in: true, want: 0},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := parseExpiresIn(tc.in)
			if got != tc.want {
				t.Fatalf("parseExpiresIn(%#v)=%s want %s", tc.in, got, tc.want)
			}
		})
	}
}

// TestDeriveTTLRules protects safety-margin and minimum TTL invariants.
func TestDeriveTTLRules(t *testing.T) {
	t.Parallel()

	provider := &OAuthClientCredentialsProvider{}
	tests := []struct {
		name string
		in   any
		want time.Duration
	}{
		{name: "missing expires_in uses default", in: nil, want: 50 * time.Second},
		{name: "large expires_in subtracts margin", in: int(120), want: 110 * time.Second},
		{name: "small expires_in floors to 5s", in: int(8), want: 5 * time.Second},
		{name: "exact margin floors to 5s", in: int(10), want: 5 * time.Second},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := provider.deriveTTL(tc.in)
			if got != tc.want {
				t.Fatalf("deriveTTL(%#v)=%s want %s", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseExpiresInPropertyEquivalentRepresentations protects representation-invariance invariant.
func TestParseExpiresInPropertyEquivalentRepresentations(t *testing.T) {
	t.Parallel()

	r := rand.New(rand.NewSource(1))
	for i := 0; i < 1000; i++ {
		v := r.Int63n(1 << 20)
		base := parseExpiresIn(v)
		asInt := parseExpiresIn(int(v))
		asString := parseExpiresIn(strconv.FormatInt(v, 10))
		asJSON := parseExpiresIn(json.Number(strconv.FormatInt(v, 10)))
		if base != asInt || base != asString || base != asJSON {
			t.Fatalf("equivalent representations must match: base=%s int=%s string=%s json=%s", base, asInt, asString, asJSON)
		}
	}
}

func mustProvider(t *testing.T, opts OAuthClientCredentialsOptions) *OAuthClientCredentialsProvider {
	t.Helper()
	provider, err := NewOAuthClientCredentialsProvider(opts)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	return provider
}

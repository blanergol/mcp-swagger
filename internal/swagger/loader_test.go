package swagger

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blanergol/mcp-swagger/internal/netguard"
)

// TestDetectSourceKind проверяет ожидаемое поведение в тестовом сценарии.
func TestDetectSourceKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		wantKind  sourceKind
		wantError bool
	}{
		{name: "relative_file", source: "./openapi.yaml", wantKind: sourceKindFile},
		{name: "absolute_file", source: "/etc/spec/openapi.json", wantKind: sourceKindFile},
		{name: "http_url", source: "http://specs.example.com/openapi.json", wantKind: sourceKindHTTP},
		{name: "https_url", source: "https://specs.example.com/openapi.yaml", wantKind: sourceKindHTTP},
		{name: "unsupported_scheme", source: "file:///tmp/openapi.yaml", wantKind: sourceKindUnsupportedURL, wantError: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			kind, _, err := detectSourceKind(tc.source)
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error for source %q", tc.source)
				}
			} else if err != nil {
				t.Fatalf("unexpected error for source %q: %v", tc.source, err)
			}
			if kind != tc.wantKind {
				t.Fatalf("detectSourceKind(%q)=%q, want %q", tc.source, kind, tc.wantKind)
			}
		})
	}
}

// TestSourceLoaderLoadsFromLocalFile проверяет ожидаемое поведение в тестовом сценарии.
func TestSourceLoaderLoadsFromLocalFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	specPath := filepath.Join(tempDir, "openapi.yaml")
	content := "openapi: 3.0.3\ninfo:\n  title: test\n  version: 1.0.0\n"
	if err := os.WriteFile(specPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp swagger file: %v", err)
	}

	loader := NewSourceLoader(specPath, nil, WithMaxBytes(1024))
	payload, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("load local file failed: %v", err)
	}
	if string(payload) != content {
		t.Fatalf("unexpected payload: %q", string(payload))
	}
}

// TestSourceLoaderLoadsFromHTTPJSONAndYAML проверяет ожидаемое поведение в тестовом сценарии.
func TestSourceLoaderLoadsFromHTTPJSONAndYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
	}{
		{name: "json", payload: `{"openapi":"3.0.3","info":{"title":"test","version":"1.0.0"}}`},
		{name: "yaml", payload: "openapi: 3.0.3\ninfo:\n  title: test\n  version: 1.0.0\n"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := strings.TrimSpace(r.Header.Get("User-Agent")); got != "Loader-Test/1.0" {
					t.Fatalf("unexpected user-agent: %q", got)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.payload))
			}))
			defer server.Close()

			loader := NewSourceLoader(server.URL+"/openapi", nil,
				WithHTTPTimeout(DefaultSwaggerHTTPTimeout),
				WithMaxBytes(1024*1024),
				WithUserAgent("Loader-Test/1.0"),
			)
			payload, err := loader.Load(context.Background())
			if err != nil {
				t.Fatalf("load http source failed: %v", err)
			}
			if string(payload) != tc.payload {
				t.Fatalf("unexpected payload: %q", string(payload))
			}
		})
	}
}

// TestSourceLoaderRedirectAllowed проверяет ожидаемое поведение в тестовом сценарии.
func TestSourceLoaderRedirectAllowed(t *testing.T) {
	t.Parallel()

	targetPayload := "openapi: 3.0.3\ninfo:\n  title: redirected\n  version: 1.0.0\n"
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(targetPayload))
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/openapi.yaml", http.StatusFound)
	}))
	defer redirector.Close()

	loader := NewSourceLoader(redirector.URL+"/spec", nil, WithMaxRedirects(5))
	payload, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("load with allowed redirect failed: %v", err)
	}
	if string(payload) != targetPayload {
		t.Fatalf("unexpected redirected payload: %q", string(payload))
	}
}

// TestSourceLoaderRedirectBlockedByPolicy проверяет ожидаемое поведение в тестовом сценарии.
func TestSourceLoaderRedirectBlockedByPolicy(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("openapi: 3.0.3\n"))
	}))
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/openapi.yaml", http.StatusFound)
	}))
	defer redirector.Close()
	redirectorURL, err := url.Parse(redirector.URL)
	if err != nil {
		t.Fatalf("parse redirector URL: %v", err)
	}

	validator := func(_ context.Context, rawURL string) error {
		parsed, parseErr := url.Parse(rawURL)
		if parseErr != nil {
			return parseErr
		}
		host := strings.TrimSpace(parsed.Host)
		if host == targetURL.Host {
			return errors.New("target host blocked")
		}
		if host == redirectorURL.Host {
			return nil
		}
		return errors.New("unknown host blocked")
	}

	loader := NewSourceLoader(redirector.URL+"/spec", nil, WithURLValidator(validator), WithMaxRedirects(5))
	_, err = loader.Load(context.Background())
	if err == nil {
		t.Fatalf("expected redirect policy error")
	}
	if !strings.Contains(err.Error(), "redirect blocked by policy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSourceLoaderMaxBytesExceeded проверяет ожидаемое поведение в тестовом сценарии.
func TestSourceLoaderMaxBytesExceeded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("openapi: 3.0.3\ninfo:\n  title: huge\n  version: 1.0.0\n"))
	}))
	defer server.Close()

	loader := NewSourceLoader(server.URL+"/openapi.yaml", nil, WithMaxBytes(16))
	_, err := loader.Load(context.Background())
	if err == nil {
		t.Fatalf("expected size-limit error")
	}
	if !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSourceLoaderRespectsSwaggerAllowedHostsPolicy проверяет ожидаемое поведение в тестовом сценарии.
func TestSourceLoaderRespectsSwaggerAllowedHostsPolicy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("openapi: 3.0.3\n"))
	}))
	defer server.Close()

	guard := netguard.New(netguard.Config{
		AllowedHosts:         []string{"specs.example.com"},
		BlockPrivateNetworks: false,
	})

	loader := NewSourceLoader(server.URL+"/openapi.yaml", nil, WithURLValidator(guard.ValidateURL))
	_, err := loader.Load(context.Background())
	if err == nil {
		t.Fatalf("expected host allowlist policy error")
	}
	if !errors.Is(err, netguard.ErrHostNotAllowed) {
		t.Fatalf("expected ErrHostNotAllowed, got: %v", err)
	}
}

// TestSourceLoaderRejectsUnsupportedURLScheme проверяет ожидаемое поведение в тестовом сценарии.
func TestSourceLoaderRejectsUnsupportedURLScheme(t *testing.T) {
	t.Parallel()

	loader := NewSourceLoader("file:///tmp/openapi.yaml", nil)
	_, err := loader.Load(context.Background())
	if err == nil {
		t.Fatalf("expected unsupported scheme error")
	}
	if !strings.Contains(err.Error(), "only http/https are allowed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

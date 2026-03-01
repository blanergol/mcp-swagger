package swagger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type brokenReadCloser struct{}

func (brokenReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (brokenReadCloser) Close() error             { return nil }

// TestHTTPLoaderDependencyErrorMatrix protects error mapping from transport/HTTP/body dependencies.
func TestHTTPLoaderDependencyErrorMatrix(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("empty source unavailable", func(t *testing.T) {
		loader := NewHTTPLoader("", HTTPLoaderOptions{})
		_, err := loader.Load(ctx)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected ErrUnavailable, got %v", err)
		}
	})

	t.Run("unsupported scheme", func(t *testing.T) {
		loader := NewHTTPLoader("file:///tmp/openapi.yaml", HTTPLoaderOptions{})
		_, err := loader.Load(ctx)
		if err == nil || !strings.Contains(err.Error(), "only http/https") {
			t.Fatalf("expected unsupported scheme error, got %v", err)
		}
	})

	t.Run("url validator blocks request", func(t *testing.T) {
		loader := NewHTTPLoader("https://example.com/openapi.yaml", HTTPLoaderOptions{
			URLValidator: func(context.Context, string) error { return errors.New("blocked by policy") },
		})
		_, err := loader.Load(ctx)
		if err == nil || !strings.Contains(err.Error(), "blocked by policy") {
			t.Fatalf("expected policy block error, got %v", err)
		}
	})

	t.Run("http transport error", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial failure")
		})}
		loader := NewHTTPLoader("https://example.com/openapi.yaml", HTTPLoaderOptions{HTTPClient: client})
		_, err := loader.Load(ctx)
		if err == nil || !strings.Contains(err.Error(), "fetch swagger url") {
			t.Fatalf("expected fetch error wrapper, got %v", err)
		}
	})

	t.Run("context canceled", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		})}
		loader := NewHTTPLoader("https://example.com/openapi.yaml", HTTPLoaderOptions{HTTPClient: client})
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := loader.Load(cancelCtx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("non 2xx status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "upstream down", http.StatusInternalServerError)
		}))
		defer server.Close()

		loader := NewHTTPLoader(server.URL, HTTPLoaderOptions{})
		_, err := loader.Load(ctx)
		if err == nil || !strings.Contains(err.Error(), "status 500") {
			t.Fatalf("expected status error, got %v", err)
		}
	})

	t.Run("declared content length exceeds max", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: 1024,
				Body:          io.NopCloser(strings.NewReader("ok")),
				Header:        make(http.Header),
			}, nil
		})}
		loader := NewHTTPLoader("https://example.com/openapi.yaml", HTTPLoaderOptions{HTTPClient: client, MaxBytes: 16})
		_, err := loader.Load(ctx)
		if err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("expected size limit error, got %v", err)
		}
	})

	t.Run("body exceeds max bytes", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", 128)))
		}))
		defer server.Close()

		loader := NewHTTPLoader(server.URL, HTTPLoaderOptions{MaxBytes: 64})
		_, err := loader.Load(ctx)
		if err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("expected size limit read error, got %v", err)
		}
	})

	t.Run("body read error", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       brokenReadCloser{},
				Header:     make(http.Header),
			}, nil
		})}
		loader := NewHTTPLoader("https://example.com/openapi.yaml", HTTPLoaderOptions{HTTPClient: client})
		_, err := loader.Load(ctx)
		if err == nil || !strings.Contains(err.Error(), "read swagger response") {
			t.Fatalf("expected read error wrapper, got %v", err)
		}
	})
}

// TestFileLoaderMatrix protects file-based loading contracts and size guardrails.
func TestFileLoaderMatrix(t *testing.T) {
	t.Parallel()

	t.Run("empty path unavailable", func(t *testing.T) {
		loader := NewFileLoader("", FileLoaderOptions{})
		_, err := loader.Load(context.Background())
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected ErrUnavailable, got %v", err)
		}
	})

	t.Run("missing file error", func(t *testing.T) {
		loader := NewFileLoader(filepath.Join(t.TempDir(), "missing.yaml"), FileLoaderOptions{})
		_, err := loader.Load(context.Background())
		if err == nil || !strings.Contains(err.Error(), "read swagger file") {
			t.Fatalf("expected read swagger file error, got %v", err)
		}
	})

	t.Run("file stat size guard", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "openapi.yaml")
		if err := os.WriteFile(path, []byte(strings.Repeat("x", 128)), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		loader := NewFileLoader(path, FileLoaderOptions{MaxBytes: 64})
		_, err := loader.Load(context.Background())
		if err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("expected size limit error, got %v", err)
		}
	})

	t.Run("valid file load", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "openapi.yaml")
		payload := []byte(minimalOpenAPIYAML)
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		loader := NewFileLoader(path, FileLoaderOptions{MaxBytes: 1024})
		got, err := loader.Load(context.Background())
		if err != nil {
			t.Fatalf("unexpected load error: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("unexpected loaded payload")
		}
	})
}

// TestReadAllWithLimitMatrix protects generic size-limit helper behavior.
func TestReadAllWithLimitMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		limit   int64
		wantErr bool
	}{
		{name: "empty", source: "", limit: 8},
		{name: "exactly limit", source: "12345678", limit: 8},
		{name: "below limit", source: "1234", limit: 8},
		{name: "over limit", source: "123456789", limit: 8, wantErr: true},
		{name: "negative limit uses default", source: "x", limit: -1},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := readAllWithLimit(strings.NewReader(tc.source), tc.limit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected limit error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestParseURLSourceDeterminismProperty protects deterministic URL-source detection.
func TestParseURLSourceDeterminismProperty(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"http://example.com/spec.json",
		"https://example.com/spec.yaml",
		" ./openapi.yaml ",
		"file:///tmp/openapi.yaml",
		"C:/work/openapi.yaml",
		"relative/path/openapi.yml",
		"mailto:user@example.com",
	}
	for _, in := range inputs {
		u1, ok1 := parseURLSource(in)
		u2, ok2 := parseURLSource(in)
		if ok1 != ok2 {
			t.Fatalf("parseURLSource ok mismatch for %q", in)
		}
		s1 := ""
		s2 := ""
		if u1 != nil {
			s1 = u1.String()
		}
		if u2 != nil {
			s2 = u2.String()
		}
		if s1 != s2 {
			t.Fatalf("parseURLSource non-deterministic for %q: %q vs %q", in, s1, s2)
		}
	}
}

func Example_detectSourceKind() {
	kind, _, err := detectSourceKind("https://example.com/openapi.yaml")
	fmt.Println(kind, err == nil)
	// Output: http true
}

package audit

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"testing"
	"time"
)

// TestSanitizeURLRedactsSecrets protects URL sanitization for userinfo and sensitive query params.
func TestSanitizeURLRedactsSecrets(t *testing.T) {
	t.Parallel()

	raw := "https://user:pass@example.com/pets?token=abc&apiKey=123&password=secret&q=dogs"
	sanitized := sanitizeURL(raw)

	parsed, err := url.Parse(sanitized)
	if err != nil {
		t.Fatalf("sanitized url must stay parseable: %v", err)
	}
	if parsed.User != nil {
		t.Fatalf("userinfo must be stripped, got %v", parsed.User)
	}
	q := parsed.Query()
	if q.Get("token") != "[REDACTED]" || q.Get("apiKey") != "[REDACTED]" || q.Get("password") != "[REDACTED]" {
		t.Fatalf("sensitive query params must be redacted, got %q", parsed.RawQuery)
	}
	if q.Get("q") != "dogs" {
		t.Fatalf("non-sensitive query params must be preserved, got %q", q.Get("q"))
	}
}

// TestRedactHeadersAndJSONGuards protects recursive redaction behavior.
func TestRedactHeadersAndJSONGuards(t *testing.T) {
	t.Parallel()

	headers := redactHeaders(map[string]string{
		"Authorization": "Bearer secret",
		"X-Api-Key":     "123",
		"Content-Type":  "application/json",
	}, toLowerSet([]string{"authorization", "x-api-key"}))

	if headers["Authorization"] != "[REDACTED]" || headers["X-Api-Key"] != "[REDACTED]" {
		t.Fatalf("headers must be redacted: %#v", headers)
	}
	if headers["Content-Type"] != "application/json" {
		t.Fatalf("non-redacted header must be preserved: %#v", headers)
	}

	body := redactJSON(map[string]any{
		"password": "p1",
		"nested": map[string]any{
			"token": "t1",
		},
		"jsonString": "{\"secret\":\"s1\",\"value\":\"ok\"}",
	}, toLowerSet([]string{"password", "token", "secret"}))

	m := body.(map[string]any)
	if m["password"] != "[REDACTED]" {
		t.Fatalf("password must be redacted, got %#v", m["password"])
	}
	nested := m["nested"].(map[string]any)
	if nested["token"] != "[REDACTED]" {
		t.Fatalf("nested token must be redacted, got %#v", nested["token"])
	}
	jsonStr := m["jsonString"].(string)
	if jsonStr != "{\"secret\":\"[REDACTED]\",\"value\":\"ok\"}" && jsonStr != "{\"value\":\"ok\",\"secret\":\"[REDACTED]\"}" {
		t.Fatalf("json string payload must be redacted, got %q", jsonStr)
	}
}

// TestLogCallDoesNotMutateEntryGuards ensures logging redaction doesn't mutate caller-owned payloads.
func TestLogCallDoesNotMutateEntryGuards(t *testing.T) {
	t.Parallel()

	originalLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(originalLogger) })
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	entry := Entry{
		Timestamp: time.Now().UTC(),
		URL:       "https://example.com?token=abc",
		RequestHeaders: map[string]string{
			"Authorization": "Bearer abc",
		},
		RequestBody: map[string]any{
			"password": "secret",
		},
	}

	logger := NewLogger(true, []string{"authorization"}, []string{"password"})
	if err := logger.LogCall(context.Background(), entry); err != nil {
		t.Fatalf("log call returned unexpected error: %v", err)
	}

	if entry.RequestHeaders["Authorization"] != "Bearer abc" {
		t.Fatalf("request headers must not be mutated: %#v", entry.RequestHeaders)
	}
	if entry.RequestBody.(map[string]any)["password"] != "secret" {
		t.Fatalf("request body must not be mutated: %#v", entry.RequestBody)
	}
}

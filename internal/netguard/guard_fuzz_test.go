package netguard

import (
	"context"
	"strings"
	"testing"
)

// FuzzValidateURLNoPanic protects parser/validator robustness for malformed URL-like inputs.
func FuzzValidateURLNoPanic(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"http://example.com",
		"https://example.com/path",
		"https://example.com/path?x=1&y=2",
		"HTTPS://EXAMPLE.COM",
		"http://127.0.0.1",
		"http://[::1]",
		"http://1.1.1.1",
		"https://api.example.com:8443/v1",
		"https:///missing-host",
		"ftp://example.com/file",
		"file:///tmp/openapi.yaml",
		"mailto:user@example.com",
		"javascript:alert(1)",
		"http://user:pass@example.com",
		"https://example.com/%2e%2e/%2e%2e/etc/passwd",
		"https://example.com/路径",
		"https://xn--e1afmkfd.xn--p1ai",
		"https://example.com/#fragment",
		"http://[2001:db8::1]:8080/path",
		"https://-bad-hostname",
		"https://example..com",
		"http://example.com:99999",
		"http://.example.com",
		"http://example.com?token=secret",
		"http://example.com\nHost:evil.com",
		"https://example.com\r\nX-Test:1",
		"https://example.com/%00",
		"http://0.0.0.0",
		"http://169.254.169.254/latest/meta-data",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	guard := New(Config{BlockPrivateNetworks: false})
	ctx := context.Background()

	f.Fuzz(func(t *testing.T, raw string) {
		_ = normalizeHost(raw)
		err := guard.ValidateURL(ctx, raw)
		if err == nil {
			trimmed := strings.TrimSpace(raw)
			lowerTrimmed := strings.ToLower(trimmed)
			if !strings.HasPrefix(lowerTrimmed, "http://") && !strings.HasPrefix(lowerTrimmed, "https://") {
				t.Fatalf("ValidateURL accepted non-http input %q", raw)
			}
		}
	})
}

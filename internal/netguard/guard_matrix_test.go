package netguard

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"testing"
)

// TestNormalizeHostMatrix protects host normalization contract for mixed inputs.
func TestNormalizeHostMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "spaces", raw: "   ", want: ""},
		{name: "simple host", raw: "Example.COM", want: "example.com"},
		{name: "host with port", raw: "Example.com:8443", want: "example.com"},
		{name: "http url", raw: "http://Example.com:8080/v1", want: "example.com"},
		{name: "https url", raw: "https://Api.Example.com/users", want: "api.example.com"},
		{name: "ipv4 host", raw: "127.0.0.1", want: "127.0.0.1"},
		{name: "ipv4 with port", raw: "127.0.0.1:9000", want: "127.0.0.1"},
		{name: "ipv6 bracket", raw: "[2001:db8::1]", want: "2001:db8::1"},
		{name: "ipv6 url", raw: "http://[2001:db8::1]:8080/path", want: "2001:db8::1"},
		{name: "ipv6 zone", raw: "fe80::1%eth0", want: "fe80::1"},
		{name: "https with userinfo", raw: "https://user:pass@api.example.com/path", want: "api.example.com"},
		{name: "already normalized", raw: "api.example.com", want: "api.example.com"},
		{name: "tab/newline trimmed", raw: "\n\tApi.Example.com\t", want: "api.example.com"},
		{name: "file like string", raw: "C:\\path\\file", want: "c"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeHost(tc.raw)
			if got != tc.want {
				t.Fatalf("normalizeHost(%q)=%q want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestValidateURLMatrix protects URL validation contracts across valid/invalid/security scenarios.
func TestValidateURLMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         Config
		rawURL      string
		wantErr     error
		wantErrText string
	}{
		{name: "empty string", cfg: Config{}, rawURL: "", wantErr: ErrInvalidURL},
		{name: "spaces", cfg: Config{}, rawURL: "   ", wantErr: ErrInvalidURL},
		{name: "unsupported scheme", cfg: Config{}, rawURL: "ftp://example.com/file", wantErr: ErrInvalidURL},
		{name: "missing host", cfg: Config{}, rawURL: "https:///v1", wantErr: ErrInvalidURL},
		{name: "exact allowlist pass", cfg: Config{AllowedHosts: []string{"api.example.com"}}, rawURL: "https://api.example.com/v1/users"},
		{name: "wildcard allowlist pass", cfg: Config{AllowedHosts: []string{"*.example.com"}}, rawURL: "https://svc.example.com/v1"},
		{name: "allowlist case and port", cfg: Config{AllowedHosts: []string{"api.example.com"}}, rawURL: "https://API.EXAMPLE.COM:8443/v1"},
		{name: "disallowed host blocked", cfg: Config{AllowedHosts: []string{"api.example.com"}}, rawURL: "https://evil.example.com", wantErr: ErrHostNotAllowed},
		{name: "private ipv4 blocked", cfg: Config{BlockPrivateNetworks: true}, rawURL: "http://127.0.0.1:8080/healthz", wantErr: ErrPrivateNetwork},
		{name: "private ipv6 blocked", cfg: Config{BlockPrivateNetworks: true}, rawURL: "http://[::1]:8080/healthz", wantErr: ErrPrivateNetwork},
		{name: "public ipv4 allowed", cfg: Config{BlockPrivateNetworks: true}, rawURL: "https://8.8.8.8/dns-query"},
		{name: "private blocked with allowlist", cfg: Config{AllowedHosts: []string{"127.0.0.1"}, BlockPrivateNetworks: true}, rawURL: "http://127.0.0.1", wantErr: ErrPrivateNetwork},
		{name: "allowlist does not bypass private block", cfg: Config{AllowedHosts: []string{"localhost"}, BlockPrivateNetworks: true}, rawURL: "http://localhost", wantErr: ErrPrivateNetwork, wantErrText: "host resolves to private network"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := New(tc.cfg).ValidateURL(context.Background(), tc.rawURL)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateURL(%q) unexpected error: %v", tc.rawURL, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateURL(%q) error=%v want %v", tc.rawURL, err, tc.wantErr)
			}
			if tc.wantErrText != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErrText)) {
				t.Fatalf("expected error to contain %q, got %v", tc.wantErrText, err)
			}
		})
	}
}

// TestValidateHostMatrix protects host-level allowlist and private-network checks.
func TestValidateHostMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		host    string
		wantErr error
	}{
		{name: "empty host", cfg: Config{}, host: "", wantErr: ErrInvalidURL},
		{name: "spaces host", cfg: Config{}, host: "   ", wantErr: ErrInvalidURL},
		{name: "exact host allowed", cfg: Config{AllowedHosts: []string{"api.example.com"}}, host: "api.example.com"},
		{name: "exact host with case allowed", cfg: Config{AllowedHosts: []string{"api.example.com"}}, host: "API.EXAMPLE.COM"},
		{name: "suffix host allowed", cfg: Config{AllowedHosts: []string{"*.example.com"}}, host: "svc.example.com"},
		{name: "suffix root also allowed", cfg: Config{AllowedHosts: []string{"*.example.com"}}, host: "example.com"},
		{name: "suffix mismatch blocked", cfg: Config{AllowedHosts: []string{"*.example.com"}}, host: "example.net", wantErr: ErrHostNotAllowed},
		{name: "host not allowlisted", cfg: Config{AllowedHosts: []string{"api.example.com"}}, host: "other.example.com", wantErr: ErrHostNotAllowed},
		{name: "private loopback blocked", cfg: Config{BlockPrivateNetworks: true}, host: "127.0.0.1", wantErr: ErrPrivateNetwork},
		{name: "private cgnat blocked", cfg: Config{BlockPrivateNetworks: true}, host: "100.64.1.1", wantErr: ErrPrivateNetwork},
		{name: "reserved blocked", cfg: Config{BlockPrivateNetworks: true}, host: "240.0.0.1", wantErr: ErrPrivateNetwork},
		{name: "public ip allowed", cfg: Config{BlockPrivateNetworks: true}, host: "1.1.1.1"},
		{name: "public ipv6 allowed", cfg: Config{BlockPrivateNetworks: true}, host: "2001:4860:4860::8888"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := New(tc.cfg).ValidateHost(context.Background(), tc.host)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateHost(%q) unexpected error: %v", tc.host, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateHost(%q) error=%v want %v", tc.host, err, tc.wantErr)
			}
		})
	}
}

// TestNormalizeHostIdempotentProperty protects normalization convergence invariant for arbitrary inputs.
func TestNormalizeHostIdempotentProperty(t *testing.T) {
	t.Parallel()

	r := rand.New(rand.NewSource(1))
	alphabet := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789:/[]%.-_\\ \\t\\n")
	for i := 0; i < 1000; i++ {
		n := 1 + r.Intn(64)
		buf := make([]rune, n)
		for j := 0; j < n; j++ {
			buf[j] = alphabet[r.Intn(len(alphabet))]
		}
		input := string(buf)
		current := normalizeHost(input)
		stable := false
		for step := 0; step < 128; step++ {
			next := normalizeHost(current)
			if next == current {
				stable = true
				break
			}
			current = next
		}
		if !stable {
			t.Fatalf("normalizeHost must converge: input=%q last=%q", input, current)
		}
	}
}

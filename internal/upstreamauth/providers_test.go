package upstreamauth

import (
	"errors"
	"net/http"
	"testing"
)

// TestAPIKeyProviderMatrix protects constructor validation and header-application contract.
func TestAPIKeyProviderMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		header  string
		value   string
		wantErr bool
	}{
		{name: "missing header", header: "", value: "secret", wantErr: true},
		{name: "missing value", header: "X-API-Key", value: "", wantErr: true},
		{name: "spaces only invalid", header: "   ", value: "   ", wantErr: true},
		{name: "valid", header: "X-API-Key", value: "secret"},
		{name: "valid trimmed", header: " X-Key ", value: " value "},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			provider, err := NewAPIKeyProvider(tc.header, tc.value)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected constructor error")
				}
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("expected ErrInvalidConfig, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected constructor error: %v", err)
			}
			req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
			req.Header.Set("X-API-Key", "old")
			if err := provider.Apply(req); err != nil {
				t.Fatalf("apply returned error: %v", err)
			}
			if got := req.Header.Get(provider.header); got != provider.value {
				t.Fatalf("header %q=%q want %q", provider.header, got, provider.value)
			}
		})
	}

	provider, _ := NewAPIKeyProvider("X-API-Key", "secret")
	if err := provider.Apply(nil); err != nil {
		t.Fatalf("Apply(nil) must be no-op, got %v", err)
	}
}

// TestStaticBearerProviderMatrix protects constructor validation and authorization header contract.
func TestStaticBearerProviderMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{name: "empty", token: "", wantErr: true},
		{name: "spaces", token: "   ", wantErr: true},
		{name: "valid", token: "token-123"},
		{name: "valid trimmed", token: " token-xyz "},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			provider, err := NewStaticBearerProvider(tc.token)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected constructor error")
				}
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("expected ErrInvalidConfig, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected constructor error: %v", err)
			}
			req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
			req.Header.Set("Authorization", "Bearer old")
			if err := provider.Apply(req); err != nil {
				t.Fatalf("apply returned error: %v", err)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer "+provider.token {
				t.Fatalf("authorization=%q want %q", got, "Bearer "+provider.token)
			}
		})
	}

	provider, _ := NewStaticBearerProvider("token")
	if err := provider.Apply(nil); err != nil {
		t.Fatalf("Apply(nil) must be no-op, got %v", err)
	}
}

// TestNoopProviderContract protects no-op provider behavior.
func TestNoopProviderContract(t *testing.T) {
	t.Parallel()

	provider := NewNoopProvider()
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Set("Authorization", "Bearer unchanged")
	req.Header.Set("X-API-Key", "unchanged")

	if err := provider.Apply(req); err != nil {
		t.Fatalf("noop apply returned error: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer unchanged" {
		t.Fatalf("noop must not change headers, got authorization=%q", got)
	}
	if got := req.Header.Get("X-API-Key"); got != "unchanged" {
		t.Fatalf("noop must not change headers, got x-api-key=%q", got)
	}
}

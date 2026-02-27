package netguard

import (
	"context"
	"errors"
	"testing"
)

// TestValidateURLAllowedHostPasses проверяет ожидаемое поведение в тестовом сценарии.
func TestValidateURLAllowedHostPasses(t *testing.T) {
	t.Parallel()

	guard := New(Config{
		AllowedHosts:         []string{"api.example.com"},
		BlockPrivateNetworks: false,
	})
	if err := guard.ValidateURL(context.Background(), "https://api.example.com/v1/users"); err != nil {
		t.Fatalf("expected allowlisted host to pass, got error: %v", err)
	}
}

// TestValidateURLDisallowedHostBlocked проверяет ожидаемое поведение в тестовом сценарии.
func TestValidateURLDisallowedHostBlocked(t *testing.T) {
	t.Parallel()

	guard := New(Config{
		AllowedHosts:         []string{"api.example.com"},
		BlockPrivateNetworks: false,
	})
	err := guard.ValidateURL(context.Background(), "https://evil.example.com/v1/users")
	if err == nil {
		t.Fatalf("expected disallowed host to be blocked")
	}
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("expected ErrHostNotAllowed, got: %v", err)
	}
}

// TestValidateURLPrivateIPBlockedWhenEnabled проверяет ожидаемое поведение в тестовом сценарии.
func TestValidateURLPrivateIPBlockedWhenEnabled(t *testing.T) {
	t.Parallel()

	guard := New(Config{
		BlockPrivateNetworks: true,
	})
	err := guard.ValidateURL(context.Background(), "http://127.0.0.1:8080/healthz")
	if err == nil {
		t.Fatalf("expected private IP to be blocked")
	}
	if !errors.Is(err, ErrPrivateNetwork) {
		t.Fatalf("expected ErrPrivateNetwork, got: %v", err)
	}
}

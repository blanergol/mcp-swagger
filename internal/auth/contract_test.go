package auth

import (
	"errors"
	"testing"
)

// TestUnauthorizedErrorContracts protects Error/Unwrap/IsUnauthorized behavior.
func TestUnauthorizedErrorContracts(t *testing.T) {
	t.Parallel()

	var nilErr *UnauthorizedError
	if got := nilErr.Error(); got != "unauthorized" {
		t.Fatalf("nil UnauthorizedError string=%q want unauthorized", got)
	}

	plain := &UnauthorizedError{}
	if got := plain.Error(); got != "unauthorized" {
		t.Fatalf("plain UnauthorizedError string=%q want unauthorized", got)
	}
	if !errors.Is(plain, ErrUnauthorized) {
		t.Fatalf("UnauthorizedError must unwrap to ErrUnauthorized")
	}
	if !IsUnauthorized(plain) {
		t.Fatalf("IsUnauthorized must match UnauthorizedError")
	}
	if IsForbidden(plain) {
		t.Fatalf("UnauthorizedError must not match forbidden")
	}

	withReason := &UnauthorizedError{Reason: "bad token"}
	if got := withReason.Error(); got != "unauthorized: bad token" {
		t.Fatalf("unexpected UnauthorizedError string: %q", got)
	}
}

// TestForbiddenErrorContracts protects Error/Unwrap/IsForbidden behavior.
func TestForbiddenErrorContracts(t *testing.T) {
	t.Parallel()

	var nilErr *ForbiddenError
	if got := nilErr.Error(); got != "forbidden" {
		t.Fatalf("nil ForbiddenError string=%q want forbidden", got)
	}

	plain := &ForbiddenError{}
	if got := plain.Error(); got != "forbidden" {
		t.Fatalf("plain ForbiddenError string=%q want forbidden", got)
	}
	if !errors.Is(plain, ErrForbidden) {
		t.Fatalf("ForbiddenError must unwrap to ErrForbidden")
	}
	if !IsForbidden(plain) {
		t.Fatalf("IsForbidden must match ForbiddenError")
	}
	if IsUnauthorized(plain) {
		t.Fatalf("ForbiddenError must not match unauthorized")
	}

	withReason := &ForbiddenError{Reason: "missing scope"}
	if got := withReason.Error(); got != "forbidden: missing scope" {
		t.Fatalf("unexpected ForbiddenError string: %q", got)
	}
}

// TestIsHelpersWithPlainErrors protects Is* helpers when wrapping sentinel errors.
func TestIsHelpersWithPlainErrors(t *testing.T) {
	t.Parallel()

	if !IsUnauthorized(ErrUnauthorized) {
		t.Fatalf("ErrUnauthorized must match IsUnauthorized")
	}
	if !IsForbidden(ErrForbidden) {
		t.Fatalf("ErrForbidden must match IsForbidden")
	}
	if IsUnauthorized(errors.New("x")) {
		t.Fatalf("random error must not match IsUnauthorized")
	}
	if IsForbidden(errors.New("y")) {
		t.Fatalf("random error must not match IsForbidden")
	}
}

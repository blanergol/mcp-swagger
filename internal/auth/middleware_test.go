package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type validatorFunc func(ctx context.Context, token string) (Principal, error)

func (f validatorFunc) Validate(ctx context.Context, token string) (Principal, error) {
	return f(ctx, token)
}

// TestBearerTokenParsingGuards protects authorization header parsing and unauthorized classification.
func TestBearerTokenParsingGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		header    string
		wantToken string
		wantErr   error
	}{
		{name: "valid bearer", header: "Bearer abc123", wantToken: "abc123"},
		{name: "valid case-insensitive scheme", header: "bearer xyz", wantToken: "xyz"},
		{name: "missing token", header: "Bearer", wantErr: ErrUnauthorized},
		{name: "wrong scheme", header: "Basic token", wantErr: ErrUnauthorized},
		{name: "empty header", header: "", wantErr: ErrUnauthorized},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := bearerToken(tc.header)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected error %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if got != tc.wantToken {
				t.Fatalf("unexpected token: got %q want %q", got, tc.wantToken)
			}
		})
	}
}

// TestMiddlewareStatusCodesAndPrincipalContext protects auth middleware response/status and context propagation.
func TestMiddlewareStatusCodesAndPrincipalContext(t *testing.T) {
	t.Parallel()

	t.Run("nil validator returns unauthorized", func(t *testing.T) {
		h := Middleware(nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatalf("next handler must not be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("forbidden validator error maps to 403", func(t *testing.T) {
		h := Middleware(validatorFunc(func(context.Context, string) (Principal, error) {
			return Principal{}, &ForbiddenError{Reason: "missing scope"}
		}), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatalf("next handler must not be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("successful validation injects principal", func(t *testing.T) {
		nextCalled := false
		h := Middleware(validatorFunc(func(_ context.Context, token string) (Principal, error) {
			if token != "valid-token" {
				t.Fatalf("unexpected token passed to validator: %q", token)
			}
			return Principal{Subject: "user-1", Scopes: []string{"mcp:tools.call"}}, nil
		}), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				t.Fatalf("principal must be present in request context")
			}
			if principal.Subject != "user-1" {
				t.Fatalf("unexpected principal subject: %q", principal.Subject)
			}
			w.WriteHeader(http.StatusNoContent)
		}))

		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if !nextCalled {
			t.Fatalf("next handler must be called on successful validation")
		}
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected next handler status, got %d", rec.Code)
		}
	})
}

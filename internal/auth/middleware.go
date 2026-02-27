package auth

import (
	"context"
	"net/http"
	"strings"
)

// principalContextKey используется как приватный ключ context, чтобы избежать коллизий между пакетами.
type principalContextKey struct{}

// ContextWithPrincipal добавляет Principal в context запроса.
func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext получает Principal from context.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

// Middleware валидирует Authorization: Bearer token и sets Principal in context.
func Middleware(validator Validator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if validator == nil {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		token, err := bearerToken(r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		principal, err := validator.Validate(r.Context(), token)
		if err != nil {
			switch {
			case IsForbidden(err):
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			default:
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			}
			return
		}

		next.ServeHTTP(w, r.WithContext(ContextWithPrincipal(r.Context(), principal)))
	})
}

// bearerToken выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func bearerToken(header string) (string, error) {
	parts := strings.Fields(strings.TrimSpace(header))
	if len(parts) != 2 {
		return "", &UnauthorizedError{Reason: "invalid authorization header"}
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return "", &UnauthorizedError{Reason: "authorization scheme must be bearer"}
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", &UnauthorizedError{Reason: "empty bearer token"}
	}
	return token, nil
}

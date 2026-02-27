package auth

import (
	"context"
	"errors"
)

// Principal описывает аутентифицированный субъект, извлеченный из токена.
type Principal struct {
	Subject   string
	Scopes    []string
	RawClaims map[string]any
}

// Validator задает контракт валидации Bearer-токена.
type Validator interface {
	Validate(ctx context.Context, token string) (Principal, error)
}

var (
	// ErrUnauthorized означает отсутствующий или невалидный токен (HTTP 401).
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden означает валидный токен без нужных прав (HTTP 403).
	ErrForbidden = errors.New("forbidden")
)

// UnauthorizedError оборачивает ошибки неаутентифицированного доступа.
type UnauthorizedError struct {
	Reason string
}

// Error возвращает человекочитаемое описание ошибки.
func (e *UnauthorizedError) Error() string {
	if e == nil || e.Reason == "" {
		return "unauthorized"
	}
	return "unauthorized: " + e.Reason
}

// Unwrap возвращает вложенную первопричину ошибки для errors.Is/errors.As.
func (e *UnauthorizedError) Unwrap() error {
	return ErrUnauthorized
}

// ForbiddenError оборачивает ошибки авторизации (недостаточно прав).
type ForbiddenError struct {
	Reason string
}

// Error возвращает человекочитаемое описание ошибки.
func (e *ForbiddenError) Error() string {
	if e == nil || e.Reason == "" {
		return "forbidden"
	}
	return "forbidden: " + e.Reason
}

// Unwrap возвращает вложенную первопричину ошибки для errors.Is/errors.As.
func (e *ForbiddenError) Unwrap() error {
	return ErrForbidden
}

// IsUnauthorized возвращает true, если ошибка соответствует Unauthorized (401).
func IsUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}

// IsForbidden возвращает true, если ошибка соответствует Forbidden (403).
func IsForbidden(err error) bool {
	return errors.Is(err, ErrForbidden)
}

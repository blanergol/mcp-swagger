package upstreamauth

import (
	"errors"
	"net/http"
)

// Provider применяет авторизацию к исходящему HTTP-запросу в upstream API.
type Provider interface {
	Apply(req *http.Request) error
}

var (
	// ErrInvalidConfig означает некорректную конфигурацию upstream-аутентификации.
	ErrInvalidConfig = errors.New("invalid upstream auth config")
)

// NoopProvider не меняет запрос и используется в режиме `none`.
type NoopProvider struct{}

// NewNoopProvider создает провайдер без авторизации.
func NewNoopProvider() *NoopProvider {
	return &NoopProvider{}
}

// Apply ничего не делает и всегда возвращает nil.
func (p *NoopProvider) Apply(_ *http.Request) error {
	return nil
}

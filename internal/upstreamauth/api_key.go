package upstreamauth

import (
	"fmt"
	"net/http"
	"strings"
)

// APIKeyProvider добавляет API key header.
type APIKeyProvider struct {
	header string
	value  string
}

// NewAPIKeyProvider создает API key auth provider.
func NewAPIKeyProvider(header, value string) (*APIKeyProvider, error) {
	header = strings.TrimSpace(header)
	value = strings.TrimSpace(value)
	if header == "" {
		return nil, fmt.Errorf("%w: UPSTREAM_API_KEY_HEADER is required", ErrInvalidConfig)
	}
	if value == "" {
		return nil, fmt.Errorf("%w: UPSTREAM_API_KEY_VALUE is required", ErrInvalidConfig)
	}
	return &APIKeyProvider{header: header, value: value}, nil
}

// Apply устанавливает API-ключ в заданный заголовок запроса.
func (p *APIKeyProvider) Apply(req *http.Request) error {
	if req == nil {
		return nil
	}
	req.Header.Set(p.header, p.value)
	return nil
}

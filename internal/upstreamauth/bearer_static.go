package upstreamauth

import (
	"fmt"
	"net/http"
	"strings"
)

// StaticBearerProvider добавляет Authorization: Bearer <token> header.
type StaticBearerProvider struct {
	token string
}

// NewStaticBearerProvider создает static bearer provider.
func NewStaticBearerProvider(token string) (*StaticBearerProvider, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("%w: UPSTREAM_BEARER_TOKEN is required", ErrInvalidConfig)
	}
	return &StaticBearerProvider{token: token}, nil
}

// Apply добавляет заголовок `Authorization: Bearer <token>`.
func (p *StaticBearerProvider) Apply(req *http.Request) error {
	if req == nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	return nil
}

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// IntrospectionOptions настраивает RFC 7662 introspection validator.
type IntrospectionOptions struct {
	IntrospectionURL string
	ClientID         string
	ClientSecret     string
	Issuer           string
	Audience         string
	RequiredScopes   []string
	CacheTTL         time.Duration
	HTTPClient       *http.Client
}

// IntrospectionValidator валидирует opaque tokens via introspection endpoint.
type IntrospectionValidator struct {
	introspectionURL string
	clientID         string
	clientSecret     string
	issuer           string
	audience         string
	requiredScopes   map[string]struct{}
	cacheTTL         time.Duration
	httpClient       *http.Client
	now              func() time.Time

	mu    sync.RWMutex
	cache map[string]introspectionCacheEntry
}

// introspectionCacheEntry хранит внутреннее состояние, используемое для кэширования и синхронизации.
type introspectionCacheEntry struct {
	principal Principal
	expiresAt time.Time
}

// NewIntrospectionValidator создает introspection validator.
func NewIntrospectionValidator(opts IntrospectionOptions) (*IntrospectionValidator, error) {
	if strings.TrimSpace(opts.IntrospectionURL) == "" {
		return nil, errors.New("introspection url is required")
	}
	if strings.TrimSpace(opts.ClientID) == "" {
		return nil, errors.New("oauth client id is required")
	}
	if strings.TrimSpace(opts.ClientSecret) == "" {
		return nil, errors.New("oauth client secret is required")
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	cacheTTL := opts.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 45 * time.Second
	}

	return &IntrospectionValidator{
		introspectionURL: strings.TrimSpace(opts.IntrospectionURL),
		clientID:         strings.TrimSpace(opts.ClientID),
		clientSecret:     strings.TrimSpace(opts.ClientSecret),
		issuer:           strings.TrimSpace(opts.Issuer),
		audience:         strings.TrimSpace(opts.Audience),
		requiredScopes:   scopesToSet(opts.RequiredScopes),
		cacheTTL:         cacheTTL,
		httpClient:       client,
		now:              time.Now,
		cache:            make(map[string]introspectionCacheEntry),
	}, nil
}

// Validate валидирует token by RFC 7662 introspection endpoint.
func (v *IntrospectionValidator) Validate(ctx context.Context, token string) (Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Principal{}, &UnauthorizedError{Reason: "empty token"}
	}

	if principal, ok := v.cached(token); ok {
		return principal, nil
	}

	claims, err := v.introspect(ctx, token)
	if err != nil {
		return Principal{}, &UnauthorizedError{Reason: err.Error()}
	}

	active, ok := claims["active"].(bool)
	if !ok || !active {
		return Principal{}, &UnauthorizedError{Reason: "inactive token"}
	}

	exp, hasExp, err := claimTime(claims, "exp")
	if err != nil {
		return Principal{}, &UnauthorizedError{Reason: err.Error()}
	}
	if hasExp && !v.now().Before(exp) {
		return Principal{}, &UnauthorizedError{Reason: "token is expired"}
	}

	nbf, hasNBF, err := claimTime(claims, "nbf")
	if err != nil {
		return Principal{}, &UnauthorizedError{Reason: err.Error()}
	}
	if hasNBF && v.now().Before(nbf) {
		return Principal{}, &UnauthorizedError{Reason: "token is not valid yet"}
	}

	if err := validateOptionalIssuerAudience(claims, v.issuer, v.audience); err != nil {
		return Principal{}, &UnauthorizedError{Reason: err.Error()}
	}

	scopes := extractScopes(claims)
	if err := ensureRequiredScopes(scopes, v.requiredScopes); err != nil {
		return Principal{}, &ForbiddenError{Reason: err.Error()}
	}

	sub, _ := claims["sub"].(string)
	principal := Principal{
		Subject:   sub,
		Scopes:    scopes,
		RawClaims: cloneClaims(claims),
	}

	v.store(token, principal, exp, hasExp)
	return principal, nil
}

// introspect выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (v *IntrospectionValidator) introspect(ctx context.Context, token string) (map[string]any, error) {
	form := url.Values{}
	form.Set("token", token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.introspectionURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(v.clientID, v.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("introspection endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var claims map[string]any
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, fmt.Errorf("invalid introspection response: %w", err)
	}
	return claims, nil
}

// cached выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (v *IntrospectionValidator) cached(token string) (Principal, bool) {
	v.mu.RLock()
	entry, ok := v.cache[token]
	v.mu.RUnlock()
	if !ok {
		return Principal{}, false
	}
	if v.now().After(entry.expiresAt) {
		v.mu.Lock()
		delete(v.cache, token)
		v.mu.Unlock()
		return Principal{}, false
	}
	return entry.principal, true
}

// store выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (v *IntrospectionValidator) store(token string, principal Principal, exp time.Time, hasExp bool) {
	expiresAt := v.now().Add(v.cacheTTL)
	if hasExp && exp.Before(expiresAt) {
		expiresAt = exp
	}
	v.mu.Lock()
	v.cache[token] = introspectionCacheEntry{
		principal: principal,
		expiresAt: expiresAt,
	}
	v.mu.Unlock()
}

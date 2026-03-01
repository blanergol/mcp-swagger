package upstreamauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultTokenSafetyMargin задает значение по умолчанию, используемое при пустой или неполной конфигурации.
const defaultTokenSafetyMargin = 10 * time.Second

// OAuthClientCredentialsOptions настраивает OAuth 2.1 client credentials auth.
type OAuthClientCredentialsOptions struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       string
	Audience     string
	CacheTTL     time.Duration
	HTTPClient   *http.Client
}

// OAuthClientCredentialsProvider получает и кэширует bearer tokens via token endpoint.
type OAuthClientCredentialsProvider struct {
	tokenURL     string
	clientID     string
	clientSecret string
	scopes       string
	audience     string
	cacheTTL     time.Duration
	httpClient   *http.Client
	now          func() time.Time

	mu     sync.Mutex
	cached oauthToken
}

// oauthToken хранит данные токена и время его актуальности для повторного использования.
type oauthToken struct {
	accessToken string
	expiresAt   time.Time
}

// tokenResponse описывает служебную структуру данных для передачи между шагами обработки.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   any    `json:"expires_in"`
}

// NewOAuthClientCredentialsProvider создает oauth client credentials provider.
func NewOAuthClientCredentialsProvider(opts OAuthClientCredentialsOptions) (*OAuthClientCredentialsProvider, error) {
	tokenURL := strings.TrimSpace(opts.TokenURL)
	clientID := strings.TrimSpace(opts.ClientID)
	clientSecret := strings.TrimSpace(opts.ClientSecret)
	if tokenURL == "" {
		return nil, fmt.Errorf("%w: UPSTREAM_OAUTH_TOKEN_URL is required", ErrInvalidConfig)
	}
	if clientID == "" {
		return nil, fmt.Errorf("%w: UPSTREAM_OAUTH_CLIENT_ID is required", ErrInvalidConfig)
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("%w: UPSTREAM_OAUTH_CLIENT_SECRET is required", ErrInvalidConfig)
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &OAuthClientCredentialsProvider{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		scopes:       strings.TrimSpace(opts.Scopes),
		audience:     strings.TrimSpace(opts.Audience),
		cacheTTL:     opts.CacheTTL,
		httpClient:   httpClient,
		now:          time.Now,
	}, nil
}

// Apply obtains a token (cached) и sets Authorization header.
func (p *OAuthClientCredentialsProvider) Apply(req *http.Request) error {
	if req == nil {
		return nil
	}
	token, err := p.token(req.Context())
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// token выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (p *OAuthClientCredentialsProvider) token(ctx context.Context) (string, error) {
	now := p.now()

	p.mu.Lock()
	if p.cached.accessToken != "" && now.Before(p.cached.expiresAt) {
		token := p.cached.accessToken
		p.mu.Unlock()
		return token, nil
	}
	p.mu.Unlock()

	fetched, ttl, err := p.fetchToken(ctx)
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	p.cached = oauthToken{
		accessToken: fetched,
		expiresAt:   p.now().Add(ttl),
	}
	p.mu.Unlock()

	return fetched, nil
}

// fetchToken выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (p *OAuthClientCredentialsProvider) fetchToken(ctx context.Context) (string, time.Duration, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	if p.scopes != "" {
		form.Set("scope", p.scopes)
	}
	if p.audience != "" {
		form.Set("audience", p.audience)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.SetBasicAuth(p.clientID, p.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", 0, fmt.Errorf("token endpoint status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, err
	}

	var parsed tokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", 0, fmt.Errorf("decode token response: %w", err)
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return "", 0, fmt.Errorf("token response missing access_token")
	}

	ttl := p.deriveTTL(parsed.ExpiresIn)
	if p.cacheTTL > 0 {
		ttl = p.cacheTTL
	}
	if ttl <= 0 {
		ttl = 50 * time.Second
	}

	return parsed.AccessToken, ttl, nil
}

// deriveTTL выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (p *OAuthClientCredentialsProvider) deriveTTL(raw any) time.Duration {
	expiresIn := parseExpiresIn(raw)
	if expiresIn <= 0 {
		return 50 * time.Second
	}
	ttl := expiresIn - defaultTokenSafetyMargin
	if ttl < 5*time.Second {
		ttl = 5 * time.Second
	}
	return ttl
}

// parseExpiresIn разбирает входные данные и возвращает нормализованное представление.
func parseExpiresIn(raw any) time.Duration {
	switch v := raw.(type) {
	case nil:
		return 0
	case float64:
		return time.Duration(v) * time.Second
	case float32:
		return time.Duration(v) * time.Second
	case int:
		return time.Duration(v) * time.Second
	case int64:
		return time.Duration(v) * time.Second
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0
		}
		return time.Duration(i) * time.Second
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0
		}
		return time.Duration(i) * time.Second
	default:
		return 0
	}
}

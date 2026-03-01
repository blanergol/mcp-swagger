package auth

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTJWKSOptions настраивает JWT validation using JWKS.
type JWTJWKSOptions struct {
	Issuer         string
	Audience       string
	JWKSURL        string
	RequiredScopes []string
	CacheTTL       time.Duration
	HTTPClient     *http.Client
}

// JWTJWKSValidator валидирует JWS JWT tokens using remote JWKS.
type JWTJWKSValidator struct {
	issuer   string
	audience string
	jwksURL  string

	requiredScopes map[string]struct{}
	cacheTTL       time.Duration
	httpClient     *http.Client
	now            func() time.Time

	mu    sync.RWMutex
	cache jwksCache
}

// jwksCache хранит внутреннее состояние, используемое для кэширования и синхронизации.
type jwksCache struct {
	expiresAt time.Time
	keys      map[string]cachedJWK
}

// cachedJWK хранит внутреннее состояние, используемое для кэширования и синхронизации.
type cachedJWK struct {
	key any
	kty string
	alg string
}

// jwksDocument отражает формат служебного payload, используемого при десериализации внешнего ответа.
type jwksDocument struct {
	Keys []jwkKey `json:"keys"`
}

// jwkKey используется как приватный ключ context, чтобы избежать коллизий между пакетами.
type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`

	N string `json:"n"`
	E string `json:"e"`

	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// NewJWTJWKSValidator создает JWKS-backed validator.
func NewJWTJWKSValidator(opts JWTJWKSOptions) (*JWTJWKSValidator, error) {
	if strings.TrimSpace(opts.JWKSURL) == "" {
		return nil, errors.New("JWKS URL is required")
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	cacheTTL := opts.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}

	return &JWTJWKSValidator{
		issuer:         strings.TrimSpace(opts.Issuer),
		audience:       strings.TrimSpace(opts.Audience),
		jwksURL:        strings.TrimSpace(opts.JWKSURL),
		requiredScopes: scopesToSet(opts.RequiredScopes),
		cacheTTL:       cacheTTL,
		httpClient:     client,
		now:            time.Now,
	}, nil
}

// Validate валидирует JWT signature и claims.
func (v *JWTJWKSValidator) Validate(ctx context.Context, token string) (Principal, error) {
	if strings.TrimSpace(token) == "" {
		return Principal{}, &UnauthorizedError{Reason: "empty token"}
	}

	claims := jwt.MapClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
		jwt.WithLeeway(30*time.Second),
	)
	parsed, err := parser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		alg, _ := t.Header["alg"].(string)
		if alg != "RS256" && alg != "ES256" {
			return nil, &UnauthorizedError{Reason: "unsupported signing algorithm"}
		}
		kid, _ := t.Header["kid"].(string)
		key, err := v.lookupKey(ctx, kid, alg)
		if err != nil {
			return nil, err
		}
		return key, nil
	})
	if err != nil || !parsed.Valid {
		return Principal{}, &UnauthorizedError{Reason: "invalid jwt"}
	}

	claimsMap := map[string]any(claims)
	if err := validateJWTClaims(claimsMap, v.issuer, v.audience, v.now()); err != nil {
		return Principal{}, &UnauthorizedError{Reason: err.Error()}
	}

	scopes := extractScopes(claimsMap)
	if err := ensureRequiredScopes(scopes, v.requiredScopes); err != nil {
		return Principal{}, &ForbiddenError{Reason: err.Error()}
	}

	sub, _ := claimsMap["sub"].(string)
	return Principal{
		Subject:   sub,
		Scopes:    scopes,
		RawClaims: cloneClaims(claimsMap),
	}, nil
}

// lookupKey выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (v *JWTJWKSValidator) lookupKey(ctx context.Context, kid, alg string) (any, error) {
	if key, ok := v.cachedKey(kid, alg); ok {
		return key, nil
	}
	if err := v.refreshJWKS(ctx); err != nil {
		return nil, &UnauthorizedError{Reason: "jwks refresh failed"}
	}
	if key, ok := v.cachedKey(kid, alg); ok {
		return key, nil
	}
	return nil, &UnauthorizedError{Reason: "matching jwk not found"}
}

// cachedKey выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (v *JWTJWKSValidator) cachedKey(kid, alg string) (any, bool) {
	v.mu.RLock()
	cache := v.cache
	v.mu.RUnlock()

	if len(cache.keys) == 0 || v.now().After(cache.expiresAt) {
		return nil, false
	}

	if kid != "" {
		entry, ok := cache.keys[kid]
		if !ok {
			return nil, false
		}
		if jwkMatchesAlg(entry, alg) {
			return entry.key, true
		}
		return nil, false
	}

	var found any
	count := 0
	for _, entry := range cache.keys {
		if !jwkMatchesAlg(entry, alg) {
			continue
		}
		found = entry.key
		count++
	}
	if count == 1 {
		return found, true
	}
	return nil, false
}

// refreshJWKS выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (v *JWTJWKSValidator) refreshJWKS(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	var doc jwksDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("invalid jwks payload: %w", err)
	}

	keys := make(map[string]cachedJWK, len(doc.Keys))
	for i, key := range doc.Keys {
		parsed, err := parseJWK(key)
		if err != nil {
			continue
		}
		cacheKey := strings.TrimSpace(key.Kid)
		if cacheKey == "" {
			cacheKey = fmt.Sprintf("_anon_%d", i)
		}
		keys[cacheKey] = parsed
	}
	if len(keys) == 0 {
		return errors.New("jwks contains no supported keys")
	}

	v.mu.Lock()
	v.cache = jwksCache{
		expiresAt: v.now().Add(v.cacheTTL),
		keys:      keys,
	}
	v.mu.Unlock()
	return nil
}

// parseJWK разбирает входные данные и возвращает нормализованное представление.
func parseJWK(key jwkKey) (cachedJWK, error) {
	switch strings.ToUpper(strings.TrimSpace(key.Kty)) {
	case "RSA":
		pub, err := parseRSAPublicKey(key.N, key.E)
		if err != nil {
			return cachedJWK{}, err
		}
		return cachedJWK{key: pub, kty: "RSA", alg: strings.TrimSpace(key.Alg)}, nil
	case "EC":
		pub, err := parseECPublicKey(key.Crv, key.X, key.Y)
		if err != nil {
			return cachedJWK{}, err
		}
		return cachedJWK{key: pub, kty: "EC", alg: strings.TrimSpace(key.Alg)}, nil
	default:
		return cachedJWK{}, errors.New("unsupported kty")
	}
}

// parseRSAPublicKey разбирает входные данные и возвращает нормализованное представление.
func parseRSAPublicKey(nRaw, eRaw string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(nRaw))
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(eRaw))
	if err != nil {
		return nil, err
	}
	if len(eBytes) == 0 {
		return nil, errors.New("invalid rsa exponent")
	}
	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = (e << 8) | int(b)
	}
	if e <= 0 {
		return nil, errors.New("invalid rsa exponent value")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}

// parseECPublicKey разбирает входные данные и возвращает нормализованное представление.
func parseECPublicKey(crvRaw, xRaw, yRaw string) (*ecdsa.PublicKey, error) {
	curveName := strings.TrimSpace(crvRaw)
	if curveName != "P-256" {
		return nil, errors.New("only P-256 is supported")
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(xRaw))
	if err != nil {
		return nil, err
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(yRaw))
	if err != nil {
		return nil, err
	}
	curve := elliptic.P256()
	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)

	byteLen := (curve.Params().BitSize + 7) / 8
	rawPoint := make([]byte, 1+2*byteLen)
	rawPoint[0] = 0x04
	x.FillBytes(rawPoint[1 : 1+byteLen])
	y.FillBytes(rawPoint[1+byteLen:])
	if _, err := ecdh.P256().NewPublicKey(rawPoint); err != nil {
		return nil, errors.New("ec point is not on curve")
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

// jwkMatchesAlg выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func jwkMatchesAlg(key cachedJWK, alg string) bool {
	if key.alg != "" {
		return key.alg == alg
	}
	switch key.kty {
	case "RSA":
		return alg == "RS256"
	case "EC":
		return alg == "ES256"
	default:
		return false
	}
}

// validateJWTClaims выполняет проверку входных данных и возвращает ошибку при нарушении инвариантов.
func validateJWTClaims(claims map[string]any, issuer, audience string, now time.Time) error {
	iss, ok := claimString(claims, "iss")
	if strings.TrimSpace(issuer) != "" {
		if !ok || iss != issuer {
			return errors.New("issuer claim mismatch")
		}
	}

	if strings.TrimSpace(audience) != "" {
		audRaw, found := claims["aud"]
		if !found || !audContains(audRaw, audience) {
			return errors.New("audience claim mismatch")
		}
	}

	exp, ok, err := claimTime(claims, "exp")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("exp claim is required")
	}
	if !now.Before(exp) {
		return errors.New("token is expired")
	}

	nbf, ok, err := claimTime(claims, "nbf")
	if err != nil {
		return err
	}
	if ok && now.Before(nbf) {
		return errors.New("token is not valid yet")
	}

	return nil
}

// validateOptionalIssuerAudience выполняет проверку входных данных и возвращает ошибку при нарушении инвариантов.
func validateOptionalIssuerAudience(claims map[string]any, issuer, audience string) error {
	if strings.TrimSpace(issuer) != "" {
		if iss, ok := claimString(claims, "iss"); ok && iss != issuer {
			return errors.New("issuer claim mismatch")
		}
	}
	if strings.TrimSpace(audience) != "" {
		if audRaw, ok := claims["aud"]; ok && !audContains(audRaw, audience) {
			return errors.New("audience claim mismatch")
		}
	}
	return nil
}

// claimString выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func claimString(claims map[string]any, key string) (string, bool) {
	value, ok := claims[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	return text, true
}

// claimTime выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func claimTime(claims map[string]any, key string) (time.Time, bool, error) {
	value, ok := claims[key]
	if !ok {
		return time.Time{}, false, nil
	}

	switch v := value.(type) {
	case float64:
		return time.Unix(int64(v), 0), true, nil
	case float32:
		return time.Unix(int64(v), 0), true, nil
	case int64:
		return time.Unix(v, 0), true, nil
	case int:
		return time.Unix(int64(v), 0), true, nil
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return time.Time{}, false, err
		}
		return time.Unix(i, 0), true, nil
	case string:
		i, err := json.Number(strings.TrimSpace(v)).Int64()
		if err != nil {
			return time.Time{}, false, err
		}
		return time.Unix(i, 0), true, nil
	default:
		return time.Time{}, false, fmt.Errorf("claim %q is not a numeric timestamp", key)
	}
}

// audContains выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func audContains(raw any, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v) == expected
	case []string:
		for _, item := range v {
			if strings.TrimSpace(item) == expected {
				return true
			}
		}
		return false
	case []any:
		for _, item := range v {
			text, ok := item.(string)
			if ok && strings.TrimSpace(text) == expected {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// extractScopes извлекает целевые данные из входного объекта с валидацией формата.
func extractScopes(claims map[string]any) []string {
	var scopes []string
	scopes = append(scopes, valuesToScopes(claims["scope"])...)
	scopes = append(scopes, valuesToScopes(claims["scp"])...)
	scopes = append(scopes, valuesToScopes(claims["permissions"])...)

	if len(scopes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(scopes))
	uniq := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		uniq = append(uniq, scope)
	}
	if len(uniq) == 0 {
		return nil
	}
	return uniq
}

// valuesToScopes выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func valuesToScopes(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		return splitScopeString(v)
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, splitScopeString(item)...)
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text, ok := item.(string)
			if ok {
				out = append(out, splitScopeString(text)...)
			}
		}
		return out
	default:
		return nil
	}
}

// splitScopeString выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func splitScopeString(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// scopesToSet выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func scopesToSet(scopes []string) map[string]struct{} {
	if len(scopes) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			out[scope] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ensureRequiredScopes выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func ensureRequiredScopes(scopes []string, required map[string]struct{}) error {
	if len(required) == 0 {
		return nil
	}
	have := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		have[scope] = struct{}{}
	}
	for scope := range required {
		if _, ok := have[scope]; !ok {
			return fmt.Errorf("required scope %q is missing", scope)
		}
	}
	return nil
}

// cloneClaims создает независимую копию значения, чтобы избежать побочных эффектов мутаций.
func cloneClaims(claims map[string]any) map[string]any {
	if claims == nil {
		return nil
	}
	copyMap := make(map[string]any, len(claims))
	for k, v := range claims {
		copyMap[k] = v
	}
	return copyMap
}

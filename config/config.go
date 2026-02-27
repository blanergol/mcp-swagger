package config

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config хранит runtime-конфигурацию, загруженную из переменных окружения.
type Config struct {
	// Transport выбирает транспорт MCP: stdio (по умолчанию) или streamable.
	Transport string
	// HTTPAddr задает адрес bind для HTTP сервера streamable транспорта.
	HTTPAddr string
	// Version попадает в health/tool metadata и помогает трассировать релизы.
	Version string
	// LogLevel управляет уровнем структурированных логов процесса.
	LogLevel string
	// CorrelationIDHeader задает имя заголовка сквозной корреляции в HTTP режиме.
	CorrelationIDHeader string
	// MetricsAuthRequired требует Bearer-аутентификацию для endpoint /metrics.
	MetricsAuthRequired bool

	// SwaggerPath указывает путь к локальному файлу или URL спецификации OpenAPI.
	SwaggerPath string
	// SwaggerFormat управляет парсером (auto/json/yaml).
	SwaggerFormat string
	// SwaggerBaseURL принудительно переопределяет базовый URL при резолвинге операций.
	SwaggerBaseURL string
	// SwaggerReload включает перечитывание спецификации на каждый запрос (удобно для dev).
	SwaggerReload bool
	// SwaggerCacheTTL задает срок жизни кэша при SwaggerReload=false.
	SwaggerCacheTTL time.Duration
	// SwaggerHTTPTimeout ограничивает время загрузки SWAGGER_PATH по сети.
	SwaggerHTTPTimeout time.Duration
	// SwaggerMaxBytes задает hard-limit размера спецификации (файл и URL).
	SwaggerMaxBytes int64
	// SwaggerUserAgent отправляется при HTTP загрузке спецификации.
	SwaggerUserAgent string

	// CORSAllowedOrigins задает allowlist origin-ов для streamable HTTP.
	CORSAllowedOrigins []string

	// Inbound OAuth конфигурирует проверку токенов на входящем /mcp.
	InboundOAuthIssuer                string
	InboundOAuthAudience              string
	InboundOAuthJWKSURL               string
	InboundOAuthJWKSCacheTTL          time.Duration
	InboundOAuthIntrospectionURL      string
	InboundOAuthClientID              string
	InboundOAuthClientSecret          string
	InboundOAuthRequiredScopes        []string
	InboundOAuthIntrospectionCacheTTL time.Duration

	// HTTP timeouts/limits защищают streamable transport от зависаний и oversized body.
	HTTPReadTimeout       time.Duration
	HTTPReadHeaderTimeout time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration
	HTTPShutdownTimeout   time.Duration
	HTTPSessionTimeout    time.Duration
	HTTPMaxBodyBytes      int64

	// MCPAPIMode определяет policy-режим execute (plan_only/readonly/write/sandbox).
	MCPAPIMode string
	// UpstreamBaseURL и UpstreamSandboxBaseURL задают приоритетные базовые URL для execute.
	UpstreamBaseURL        string
	UpstreamSandboxBaseURL string
	// Allowed*/Denied* управляют allow/deny списками методов и operationId.
	AllowedMethods      []string
	DeniedMethods       []string
	AllowedOperationIDs []string
	DeniedOperationIDs  []string
	// RequireConfirmationForWrite включает human-in-the-loop перед write-методами.
	RequireConfirmationForWrite bool
	// Global/per-principal лимиты защищают upstream от перегрузки.
	MaxCallsPerMinute              int
	MaxConcurrentCalls             int
	MaxCallsPerMinutePerPrincipal  int
	MaxConcurrentCallsPerPrincipal int
	// HTTPTimeout/MaxRequestBytes/MaxResponseBytes ограничивают отдельный upstream вызов.
	HTTPTimeout      time.Duration
	MaxRequestBytes  int64
	MaxResponseBytes int64
	// UserAgent используется при исходящих HTTP запросах execute.
	UserAgent string
	// AuditLog и redaction-поля управляют безопасным аудитом вызовов.
	AuditLog         bool
	RedactHeaders    []string
	RedactJSONFields []string
	// UpstreamAuthMode выбирает схему авторизации к реальному API.
	UpstreamAuthMode     string
	UpstreamBearerToken  string
	UpstreamAPIKeyHeader string
	UpstreamAPIKeyValue  string

	// UPSTREAM_OAUTH_* используется только для outbound OAuth client_credentials.
	UpstreamOAuthTokenURL      string
	UpstreamOAuthClientID      string
	UpstreamOAuthClientSecret  string
	UpstreamOAuthScopes        string
	UpstreamOAuthAudience      string
	UpstreamOAuthTokenCacheTTL time.Duration

	// ValidateRequest/ValidateResponse включают контрактные проверки swagger в execute.
	ValidateRequest  bool
	ValidateResponse bool
	// ConfirmationTTL задает срок жизни confirmationId в in-memory store.
	ConfirmationTTL time.Duration

	// AllowedHosts и BlockPrivateNetworks формируют SSRF guardrails.
	UpstreamAllowedHosts []string
	SwaggerAllowedHosts  []string
	BlockPrivateNetworks bool

	// CompatibilityWarnings содержит предупреждения о legacy ENV fallback.
	CompatibilityWarnings []string
}

// Load читает переменные окружения и применяет значения по умолчанию.
func Load() Config {
	warnings := make([]string, 0)
	maxCallsPerMinute := getIntEnv("MAX_CALLS_PER_MINUTE", 60)
	maxConcurrentCalls := getIntEnv("MAX_CONCURRENT_CALLS", 10)

	cfg := Config{
		Transport:           strings.ToLower(getEnv("TRANSPORT", "stdio")),
		HTTPAddr:            getEnv("HTTP_ADDR", ":8080"),
		Version:             getEnv("VERSION", "dev"),
		LogLevel:            strings.ToLower(getEnv("LOG_LEVEL", "info")),
		CorrelationIDHeader: strings.TrimSpace(getEnv("CORRELATION_ID_HEADER", "X-Correlation-Id")),
		MetricsAuthRequired: getBoolEnv("METRICS_AUTH_REQUIRED", false),

		SwaggerPath:        strings.TrimSpace(os.Getenv("SWAGGER_PATH")),
		SwaggerFormat:      strings.ToLower(getEnv("SWAGGER_FORMAT", "auto")),
		SwaggerBaseURL:     strings.TrimSpace(os.Getenv("SWAGGER_BASE_URL")),
		SwaggerReload:      getBoolEnv("SWAGGER_RELOAD", false),
		SwaggerCacheTTL:    getDurationEnv("SWAGGER_CACHE_TTL", 5*time.Minute),
		SwaggerHTTPTimeout: getDurationEnv("SWAGGER_HTTP_TIMEOUT", 10*time.Second),
		SwaggerMaxBytes:    getInt64Env("SWAGGER_MAX_BYTES", 5*1024*1024),
		SwaggerUserAgent:   getEnv("SWAGGER_USER_AGENT", "MCP-Swagger-Loader/1.0"),

		CORSAllowedOrigins: splitList(os.Getenv("CORS_ALLOWED_ORIGINS")),

		InboundOAuthIssuer:                getEnvWithLegacy("INBOUND_OAUTH_ISSUER", "OAUTH_ISSUER", "", &warnings),
		InboundOAuthAudience:              getEnvWithLegacy("INBOUND_OAUTH_AUDIENCE", "OAUTH_AUDIENCE", "", &warnings),
		InboundOAuthJWKSURL:               getEnvWithLegacy("INBOUND_OAUTH_JWKS_URL", "OAUTH_JWKS_URL", "", &warnings),
		InboundOAuthJWKSCacheTTL:          getDurationEnvWithLegacy("INBOUND_OAUTH_JWKS_CACHE_TTL", "OAUTH_JWKS_CACHE_TTL", 5*time.Minute, &warnings),
		InboundOAuthIntrospectionURL:      getEnvWithLegacy("INBOUND_OAUTH_INTROSPECTION_URL", "OAUTH_INTROSPECTION_URL", "", &warnings),
		InboundOAuthClientID:              getEnvWithLegacy("INBOUND_OAUTH_CLIENT_ID", "OAUTH_CLIENT_ID", "", &warnings),
		InboundOAuthClientSecret:          getEnvWithLegacy("INBOUND_OAUTH_CLIENT_SECRET", "OAUTH_CLIENT_SECRET", "", &warnings),
		InboundOAuthRequiredScopes:        splitList(getEnvWithLegacy("INBOUND_OAUTH_REQUIRED_SCOPES", "OAUTH_REQUIRED_SCOPES", "", &warnings)),
		InboundOAuthIntrospectionCacheTTL: getDurationEnvWithLegacy("INBOUND_OAUTH_INTROSPECTION_CACHE_TTL", "OAUTH_INTROSPECTION_CACHE_TTL", 45*time.Second, &warnings),

		HTTPReadTimeout:       getDurationEnv("HTTP_READ_TIMEOUT", 15*time.Second),
		HTTPReadHeaderTimeout: getDurationEnv("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
		HTTPWriteTimeout:      getDurationEnv("HTTP_WRITE_TIMEOUT", 30*time.Second),
		HTTPIdleTimeout:       getDurationEnv("HTTP_IDLE_TIMEOUT", 60*time.Second),
		HTTPShutdownTimeout:   getDurationEnv("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
		HTTPSessionTimeout:    getDurationEnv("HTTP_SESSION_TIMEOUT", 2*time.Minute),
		HTTPMaxBodyBytes:      getInt64Env("HTTP_MAX_BODY_BYTES", 1<<20),

		MCPAPIMode:                     strings.ToLower(getEnv("MCP_API_MODE", "plan_only")),
		UpstreamBaseURL:                strings.TrimSpace(os.Getenv("UPSTREAM_BASE_URL")),
		UpstreamSandboxBaseURL:         strings.TrimSpace(os.Getenv("UPSTREAM_SANDBOX_BASE_URL")),
		AllowedMethods:                 normalizeUpperList(splitList(getEnv("ALLOWED_METHODS", "GET,HEAD,OPTIONS"))),
		DeniedMethods:                  normalizeUpperList(splitList(getEnv("DENIED_METHODS", "DELETE"))),
		AllowedOperationIDs:            splitList(os.Getenv("ALLOWED_OPERATION_IDS")),
		DeniedOperationIDs:             splitList(os.Getenv("DENIED_OPERATION_IDS")),
		RequireConfirmationForWrite:    getBoolEnv("REQUIRE_CONFIRMATION_FOR_WRITE", false),
		MaxCallsPerMinute:              maxCallsPerMinute,
		MaxConcurrentCalls:             maxConcurrentCalls,
		MaxCallsPerMinutePerPrincipal:  getIntEnv("MAX_CALLS_PER_MINUTE_PER_PRINCIPAL", maxCallsPerMinute),
		MaxConcurrentCallsPerPrincipal: getIntEnv("MAX_CONCURRENT_CALLS_PER_PRINCIPAL", maxConcurrentCalls),
		HTTPTimeout:                    getDurationEnv("HTTP_TIMEOUT", 30*time.Second),
		MaxRequestBytes:                getInt64Env("MAX_REQUEST_BYTES", 1<<20),
		MaxResponseBytes:               getInt64Env("MAX_RESPONSE_BYTES", 2<<20),
		UserAgent:                      getEnv("USER_AGENT", "MCP-Swagger-Agent/1.0"),
		AuditLog:                       getBoolEnv("AUDIT_LOG", true),
		RedactHeaders:                  splitList(getEnv("REDACT_HEADERS", "Authorization,Cookie,X-API-Key")),
		RedactJSONFields:               splitList(getEnv("REDACT_JSON_FIELDS", "password,token,secret,apiKey,access_token,refresh_token")),
		UpstreamAuthMode:               strings.ToLower(getEnv("UPSTREAM_AUTH_MODE", "none")),
		UpstreamBearerToken:            strings.TrimSpace(os.Getenv("UPSTREAM_BEARER_TOKEN")),
		UpstreamAPIKeyHeader:           strings.TrimSpace(getEnv("UPSTREAM_API_KEY_HEADER", "X-API-Key")),
		UpstreamAPIKeyValue:            strings.TrimSpace(os.Getenv("UPSTREAM_API_KEY_VALUE")),

		UpstreamOAuthTokenURL:      getEnvWithLegacy("UPSTREAM_OAUTH_TOKEN_URL", "OAUTH_TOKEN_URL", "", &warnings),
		UpstreamOAuthClientID:      getEnvWithLegacy("UPSTREAM_OAUTH_CLIENT_ID", "OAUTH_CLIENT_ID", "", &warnings),
		UpstreamOAuthClientSecret:  getEnvWithLegacy("UPSTREAM_OAUTH_CLIENT_SECRET", "OAUTH_CLIENT_SECRET", "", &warnings),
		UpstreamOAuthScopes:        getEnvWithLegacy("UPSTREAM_OAUTH_SCOPES", "OAUTH_SCOPES", "", &warnings),
		UpstreamOAuthAudience:      getEnvWithLegacy("UPSTREAM_OAUTH_AUDIENCE", "OAUTH_AUDIENCE", "", &warnings),
		UpstreamOAuthTokenCacheTTL: getDurationEnvWithLegacy("UPSTREAM_OAUTH_TOKEN_CACHE_TTL", "OAUTH_TOKEN_CACHE_TTL", 0, &warnings),

		ValidateRequest:  getBoolEnv("VALIDATE_REQUEST", true),
		ValidateResponse: getBoolEnv("VALIDATE_RESPONSE", true),
		ConfirmationTTL:  getDurationEnv("CONFIRMATION_TTL", 10*time.Minute),

		UpstreamAllowedHosts: splitList(os.Getenv("UPSTREAM_ALLOWED_HOSTS")),
		SwaggerAllowedHosts:  splitList(os.Getenv("SWAGGER_ALLOWED_HOSTS")),
		BlockPrivateNetworks: getBoolEnv("BLOCK_PRIVATE_NETWORKS", true),
	}

	if cfg.Transport == "" {
		cfg.Transport = "stdio"
	}
	cfg.SwaggerFormat = cfg.ResolveSwaggerFormat()
	cfg.CompatibilityWarnings = append([]string(nil), warnings...)
	return cfg
}

// OAuthMode возвращает режим inbound OAuth-валидации, определенный по заполненным полям.
func (c Config) OAuthMode() string {
	if c.InboundOAuthIntrospectionURL != "" {
		return "introspection"
	}
	if c.InboundOAuthJWKSURL != "" {
		return "jwks"
	}
	return "none"
}

// ResolveSwaggerFormat определяет фактический формат swagger (json/yaml/auto) с автоопределением.
func (c Config) ResolveSwaggerFormat() string {
	mode := strings.ToLower(strings.TrimSpace(c.SwaggerFormat))
	if mode == "json" || mode == "yaml" {
		return mode
	}

	ext := ""
	if parsed, isURL := parseURLSource(c.SwaggerPath); isURL {
		ext = strings.ToLower(strings.TrimSpace(path.Ext(parsed.Path)))
	} else {
		ext = strings.ToLower(strings.TrimSpace(filepath.Ext(c.SwaggerPath)))
	}
	switch ext {
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return "auto"
	}
}

// parseURLSource разбирает входные данные и возвращает нормализованное представление.
func parseURLSource(source string) (*url.URL, bool) {
	raw := strings.TrimSpace(source)
	if raw == "" {
		return nil, false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Scheme) == "" {
		return nil, false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	prefix := scheme + "://"
	if strings.HasPrefix(strings.ToLower(raw), prefix) || scheme == "file" {
		return parsed, true
	}
	return nil, false
}

// getEnv возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// getEnvWithLegacy возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func getEnvWithLegacy(newKey, legacyKey, fallback string, warnings *[]string) string {
	newValue := strings.TrimSpace(os.Getenv(newKey))
	if newValue != "" {
		return newValue
	}
	legacyValue := strings.TrimSpace(os.Getenv(legacyKey))
	if legacyValue != "" {
		addLegacyWarning(warnings, legacyKey, newKey)
		return legacyValue
	}
	return fallback
}

// getDurationEnv возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func getDurationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}

// getDurationEnvWithLegacy возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func getDurationEnvWithLegacy(newKey, legacyKey string, fallback time.Duration, warnings *[]string) time.Duration {
	newValue := strings.TrimSpace(os.Getenv(newKey))
	if newValue != "" {
		d, err := time.ParseDuration(newValue)
		if err != nil {
			return fallback
		}
		return d
	}
	legacyValue := strings.TrimSpace(os.Getenv(legacyKey))
	if legacyValue == "" {
		return fallback
	}
	d, err := time.ParseDuration(legacyValue)
	if err != nil {
		return fallback
	}
	addLegacyWarning(warnings, legacyKey, newKey)
	return d
}

// getInt64Env возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func getInt64Env(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

// getIntEnv возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func getIntEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

// getBoolEnv возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func getBoolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// splitList выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func splitList(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		name := strings.TrimSpace(f)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeUpperList нормализует входные данные к канонической форме, используемой в модуле.
func normalizeUpperList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		value := strings.ToUpper(strings.TrimSpace(item))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// addLegacyWarning выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func addLegacyWarning(warnings *[]string, legacyKey, newKey string) {
	if warnings == nil {
		return
	}
	message := "deprecated env " + legacyKey + " is in use; migrate to " + newKey
	for _, existing := range *warnings {
		if existing == message {
			return
		}
	}
	*warnings = append(*warnings, message)
}

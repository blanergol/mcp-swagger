package config

import (
	"strings"
	"testing"
)

// oauthEnvKeys хранит служебное значение, используемое внутри текущего пакета.
var oauthEnvKeys = []string{
	"INBOUND_OAUTH_ISSUER",
	"INBOUND_OAUTH_AUDIENCE",
	"INBOUND_OAUTH_JWKS_URL",
	"INBOUND_OAUTH_JWKS_CACHE_TTL",
	"INBOUND_OAUTH_INTROSPECTION_URL",
	"INBOUND_OAUTH_CLIENT_ID",
	"INBOUND_OAUTH_CLIENT_SECRET",
	"INBOUND_OAUTH_REQUIRED_SCOPES",
	"INBOUND_OAUTH_INTROSPECTION_CACHE_TTL",
	"UPSTREAM_OAUTH_TOKEN_URL",
	"UPSTREAM_OAUTH_CLIENT_ID",
	"UPSTREAM_OAUTH_CLIENT_SECRET",
	"UPSTREAM_OAUTH_SCOPES",
	"UPSTREAM_OAUTH_AUDIENCE",
	"UPSTREAM_OAUTH_TOKEN_CACHE_TTL",
	"OAUTH_ISSUER",
	"OAUTH_AUDIENCE",
	"OAUTH_JWKS_URL",
	"OAUTH_JWKS_CACHE_TTL",
	"OAUTH_INTROSPECTION_URL",
	"OAUTH_CLIENT_ID",
	"OAUTH_CLIENT_SECRET",
	"OAUTH_REQUIRED_SCOPES",
	"OAUTH_INTROSPECTION_CACHE_TTL",
	"OAUTH_TOKEN_URL",
	"OAUTH_SCOPES",
	"OAUTH_TOKEN_CACHE_TTL",
	"UPSTREAM_ALLOWED_HOSTS",
	"SWAGGER_ALLOWED_HOSTS",
	"BLOCK_PRIVATE_NETWORKS",
	"CONFIRMATION_TTL",
	"MAX_CALLS_PER_MINUTE",
	"MAX_CONCURRENT_CALLS",
	"MAX_CALLS_PER_MINUTE_PER_PRINCIPAL",
	"MAX_CONCURRENT_CALLS_PER_PRINCIPAL",
	"METRICS_AUTH_REQUIRED",
	"CORRELATION_ID_HEADER",
	"SWAGGER_HTTP_TIMEOUT",
	"SWAGGER_MAX_BYTES",
	"SWAGGER_USER_AGENT",
}

// resetOAuthEnv выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func resetOAuthEnv(t *testing.T) {
	t.Helper()
	for _, key := range oauthEnvKeys {
		t.Setenv(key, "")
	}
}

// TestLoadPrefersNewInboundEnvOverLegacy проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadPrefersNewInboundEnvOverLegacy(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("INBOUND_OAUTH_CLIENT_ID", "inbound-new")
	t.Setenv("OAUTH_CLIENT_ID", "legacy-shared")
	// в этом тесте исходящая конфигурация остается независимой.
	t.Setenv("UPSTREAM_OAUTH_CLIENT_ID", "upstream-new")

	cfg := Load()

	if cfg.InboundOAuthClientID != "inbound-new" {
		t.Fatalf("expected inbound client id from INBOUND_OAUTH_CLIENT_ID, got %q", cfg.InboundOAuthClientID)
	}
	if cfg.UpstreamOAuthClientID != "upstream-new" {
		t.Fatalf("expected upstream client id from UPSTREAM_OAUTH_CLIENT_ID, got %q", cfg.UpstreamOAuthClientID)
	}
	if hasWarning(cfg.CompatibilityWarnings, "OAUTH_CLIENT_ID") {
		t.Fatalf("did not expect legacy warning for OAUTH_CLIENT_ID when new envs are set: %v", cfg.CompatibilityWarnings)
	}
}

// TestLoadPrefersNewUpstreamEnvOverLegacy проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadPrefersNewUpstreamEnvOverLegacy(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("UPSTREAM_OAUTH_TOKEN_URL", "https://new-upstream/token")
	t.Setenv("UPSTREAM_OAUTH_AUDIENCE", "new-upstream-aud")
	t.Setenv("OAUTH_TOKEN_URL", "https://legacy/token")
	t.Setenv("OAUTH_AUDIENCE", "legacy-aud")
	// в этом тесте входящая конфигурация остается независимой.
	t.Setenv("INBOUND_OAUTH_AUDIENCE", "inbound-aud")

	cfg := Load()

	if cfg.UpstreamOAuthTokenURL != "https://new-upstream/token" {
		t.Fatalf("expected upstream token url from UPSTREAM_OAUTH_TOKEN_URL, got %q", cfg.UpstreamOAuthTokenURL)
	}
	if cfg.UpstreamOAuthAudience != "new-upstream-aud" {
		t.Fatalf("expected upstream audience from UPSTREAM_OAUTH_AUDIENCE, got %q", cfg.UpstreamOAuthAudience)
	}
	if cfg.InboundOAuthAudience != "inbound-aud" {
		t.Fatalf("expected inbound audience from INBOUND_OAUTH_AUDIENCE, got %q", cfg.InboundOAuthAudience)
	}
}

// TestLoadInboundOutboundNoCollision проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadInboundOutboundNoCollision(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("INBOUND_OAUTH_CLIENT_ID", "inbound-client")
	t.Setenv("INBOUND_OAUTH_AUDIENCE", "inbound-aud")
	t.Setenv("UPSTREAM_OAUTH_CLIENT_ID", "upstream-client")
	t.Setenv("UPSTREAM_OAUTH_AUDIENCE", "upstream-aud")
	t.Setenv("OAUTH_CLIENT_ID", "legacy-shared")
	t.Setenv("OAUTH_AUDIENCE", "legacy-shared-aud")

	cfg := Load()

	if cfg.InboundOAuthClientID != "inbound-client" {
		t.Fatalf("unexpected inbound client id: %q", cfg.InboundOAuthClientID)
	}
	if cfg.UpstreamOAuthClientID != "upstream-client" {
		t.Fatalf("unexpected upstream client id: %q", cfg.UpstreamOAuthClientID)
	}
	if cfg.InboundOAuthAudience != "inbound-aud" {
		t.Fatalf("unexpected inbound audience: %q", cfg.InboundOAuthAudience)
	}
	if cfg.UpstreamOAuthAudience != "upstream-aud" {
		t.Fatalf("unexpected upstream audience: %q", cfg.UpstreamOAuthAudience)
	}
	if len(cfg.CompatibilityWarnings) != 0 {
		t.Fatalf("expected no legacy warnings when new envs are set, got: %v", cfg.CompatibilityWarnings)
	}
}

// TestLoadLegacyFallbackWithWarnings проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadLegacyFallbackWithWarnings(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("OAUTH_CLIENT_ID", "legacy-client")

	cfg := Load()

	if cfg.InboundOAuthClientID != "legacy-client" {
		t.Fatalf("expected inbound fallback from legacy env, got %q", cfg.InboundOAuthClientID)
	}
	if cfg.UpstreamOAuthClientID != "legacy-client" {
		t.Fatalf("expected upstream fallback from legacy env, got %q", cfg.UpstreamOAuthClientID)
	}
	if !hasWarning(cfg.CompatibilityWarnings, "OAUTH_CLIENT_ID") {
		t.Fatalf("expected legacy warning for OAUTH_CLIENT_ID, got: %v", cfg.CompatibilityWarnings)
	}
}

// TestLoadSSRFEnvParsing проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadSSRFEnvParsing(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("UPSTREAM_ALLOWED_HOSTS", "api.example.com,*.svc.example.net")
	t.Setenv("SWAGGER_ALLOWED_HOSTS", "specs.example.com")
	t.Setenv("BLOCK_PRIVATE_NETWORKS", "true")

	cfg := Load()

	if len(cfg.UpstreamAllowedHosts) != 2 {
		t.Fatalf("expected 2 upstream allowed hosts, got %d", len(cfg.UpstreamAllowedHosts))
	}
	if cfg.UpstreamAllowedHosts[0] != "api.example.com" {
		t.Fatalf("unexpected upstream allowed host[0]: %q", cfg.UpstreamAllowedHosts[0])
	}
	if cfg.SwaggerAllowedHosts[0] != "specs.example.com" {
		t.Fatalf("unexpected swagger allowed host: %q", cfg.SwaggerAllowedHosts[0])
	}
	if !cfg.BlockPrivateNetworks {
		t.Fatalf("expected BlockPrivateNetworks=true")
	}
}

// TestLoadConfirmationTTL проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadConfirmationTTL(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("CONFIRMATION_TTL", "2m30s")

	cfg := Load()
	if cfg.ConfirmationTTL.String() != "2m30s" {
		t.Fatalf("expected confirmation ttl 2m30s, got %s", cfg.ConfirmationTTL)
	}
}

// TestLoadLegacyFallbackWarningsForInboundAndUpstream проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadLegacyFallbackWarningsForInboundAndUpstream(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("OAUTH_ISSUER", "https://legacy-issuer.example.com")
	t.Setenv("OAUTH_TOKEN_URL", "https://legacy-issuer.example.com/token")

	cfg := Load()

	if cfg.InboundOAuthIssuer != "https://legacy-issuer.example.com" {
		t.Fatalf("expected inbound issuer fallback from legacy env, got %q", cfg.InboundOAuthIssuer)
	}
	if cfg.UpstreamOAuthTokenURL != "https://legacy-issuer.example.com/token" {
		t.Fatalf("expected upstream token url fallback from legacy env, got %q", cfg.UpstreamOAuthTokenURL)
	}
	if !hasWarning(cfg.CompatibilityWarnings, "OAUTH_ISSUER") {
		t.Fatalf("expected legacy warning for OAUTH_ISSUER, got: %v", cfg.CompatibilityWarnings)
	}
	if !hasWarning(cfg.CompatibilityWarnings, "OAUTH_TOKEN_URL") {
		t.Fatalf("expected legacy warning for OAUTH_TOKEN_URL, got: %v", cfg.CompatibilityWarnings)
	}
}

// TestLoadPerPrincipalLimitsDefaultToGlobal проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadPerPrincipalLimitsDefaultToGlobal(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("MAX_CALLS_PER_MINUTE", "77")
	t.Setenv("MAX_CONCURRENT_CALLS", "13")
	t.Setenv("MAX_CALLS_PER_MINUTE_PER_PRINCIPAL", "")
	t.Setenv("MAX_CONCURRENT_CALLS_PER_PRINCIPAL", "")

	cfg := Load()

	if cfg.MaxCallsPerMinute != 77 {
		t.Fatalf("expected global max calls per minute=77, got %d", cfg.MaxCallsPerMinute)
	}
	if cfg.MaxConcurrentCalls != 13 {
		t.Fatalf("expected global max concurrent calls=13, got %d", cfg.MaxConcurrentCalls)
	}
	if cfg.MaxCallsPerMinutePerPrincipal != 77 {
		t.Fatalf("expected per-principal calls fallback to global=77, got %d", cfg.MaxCallsPerMinutePerPrincipal)
	}
	if cfg.MaxConcurrentCallsPerPrincipal != 13 {
		t.Fatalf("expected per-principal concurrent fallback to global=13, got %d", cfg.MaxConcurrentCallsPerPrincipal)
	}
}

// TestLoadPerPrincipalLimitsCanOverrideGlobal проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadPerPrincipalLimitsCanOverrideGlobal(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("MAX_CALLS_PER_MINUTE", "60")
	t.Setenv("MAX_CONCURRENT_CALLS", "10")
	t.Setenv("MAX_CALLS_PER_MINUTE_PER_PRINCIPAL", "20")
	t.Setenv("MAX_CONCURRENT_CALLS_PER_PRINCIPAL", "3")

	cfg := Load()

	if cfg.MaxCallsPerMinute != 60 {
		t.Fatalf("expected global max calls per minute=60, got %d", cfg.MaxCallsPerMinute)
	}
	if cfg.MaxConcurrentCalls != 10 {
		t.Fatalf("expected global max concurrent calls=10, got %d", cfg.MaxConcurrentCalls)
	}
	if cfg.MaxCallsPerMinutePerPrincipal != 20 {
		t.Fatalf("expected per-principal max calls per minute=20, got %d", cfg.MaxCallsPerMinutePerPrincipal)
	}
	if cfg.MaxConcurrentCallsPerPrincipal != 3 {
		t.Fatalf("expected per-principal max concurrent calls=3, got %d", cfg.MaxConcurrentCallsPerPrincipal)
	}
}

// TestLoadMetricsAuthRequired проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadMetricsAuthRequired(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("METRICS_AUTH_REQUIRED", "true")

	cfg := Load()
	if !cfg.MetricsAuthRequired {
		t.Fatalf("expected MetricsAuthRequired=true")
	}
}

// TestLoadSwaggerHTTPLoaderConfig проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadSwaggerHTTPLoaderConfig(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("SWAGGER_HTTP_TIMEOUT", "3s")
	t.Setenv("SWAGGER_MAX_BYTES", "2097152")
	t.Setenv("SWAGGER_USER_AGENT", "Custom-Swagger-Loader/2.0")

	cfg := Load()
	if cfg.SwaggerHTTPTimeout.String() != "3s" {
		t.Fatalf("expected SwaggerHTTPTimeout=3s, got %s", cfg.SwaggerHTTPTimeout)
	}
	if cfg.SwaggerMaxBytes != 2097152 {
		t.Fatalf("expected SwaggerMaxBytes=2097152, got %d", cfg.SwaggerMaxBytes)
	}
	if cfg.SwaggerUserAgent != "Custom-Swagger-Loader/2.0" {
		t.Fatalf("expected SwaggerUserAgent=Custom-Swagger-Loader/2.0, got %q", cfg.SwaggerUserAgent)
	}
}

// TestLoadCorrelationIDHeader проверяет ожидаемое поведение в тестовом сценарии.
func TestLoadCorrelationIDHeader(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("CORRELATION_ID_HEADER", "X-Request-ID")

	cfg := Load()
	if cfg.CorrelationIDHeader != "X-Request-ID" {
		t.Fatalf("expected CorrelationIDHeader=X-Request-ID, got %q", cfg.CorrelationIDHeader)
	}
}

// TestValidateRejectsUnsupportedSwaggerURLScheme проверяет ожидаемое поведение в тестовом сценарии.
func TestValidateRejectsUnsupportedSwaggerURLScheme(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("SWAGGER_PATH", "file:///tmp/openapi.yaml")

	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for unsupported SWAGGER_PATH scheme")
	}
	if !strings.Contains(err.Error(), "only http/https are allowed") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// TestValidateSandboxRequiresSandboxBaseURL проверяет ожидаемое поведение в тестовом сценарии.
func TestValidateSandboxRequiresSandboxBaseURL(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("SWAGGER_PATH", "./openapi.yaml")
	t.Setenv("MCP_API_MODE", "sandbox")

	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for sandbox mode without UPSTREAM_SANDBOX_BASE_URL")
	}
	if !strings.Contains(err.Error(), "UPSTREAM_SANDBOX_BASE_URL") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// TestValidateStreamableRequiresInboundOAuth проверяет ожидаемое поведение в тестовом сценарии.
func TestValidateStreamableRequiresInboundOAuth(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("SWAGGER_PATH", "./openapi.yaml")
	t.Setenv("TRANSPORT", "streamable")

	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for streamable mode without inbound oauth")
	}
	if !strings.Contains(err.Error(), "inbound OAuth") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// TestValidateRejectsInvalidModeAndHTTPMethods проверяет ожидаемое поведение в тестовом сценарии.
func TestValidateRejectsInvalidModeAndHTTPMethods(t *testing.T) {
	resetOAuthEnv(t)
	t.Setenv("SWAGGER_PATH", "./openapi.yaml")
	t.Setenv("MCP_API_MODE", "execute_all")
	t.Setenv("ALLOWED_METHODS", "GET,NOTVERB")
	t.Setenv("DENIED_METHODS", "BADVERB")

	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for invalid mode/methods")
	}
	if !strings.Contains(err.Error(), "MCP_API_MODE") {
		t.Fatalf("expected mode validation error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ALLOWED_METHODS") || !strings.Contains(err.Error(), "DENIED_METHODS") {
		t.Fatalf("expected methods validation error, got: %v", err)
	}
}

// hasWarning проверяет наличие требуемого значения в текущем контексте.
func hasWarning(warnings []string, envName string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, envName) {
			return true
		}
	}
	return false
}

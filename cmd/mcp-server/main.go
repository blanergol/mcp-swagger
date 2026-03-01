package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/blanergol/mcp-swagger/config"
	"github.com/blanergol/mcp-swagger/internal/audit"
	authn "github.com/blanergol/mcp-swagger/internal/auth"
	"github.com/blanergol/mcp-swagger/internal/confirmation"
	"github.com/blanergol/mcp-swagger/internal/httpclient"
	"github.com/blanergol/mcp-swagger/internal/metrics"
	"github.com/blanergol/mcp-swagger/internal/netguard"
	"github.com/blanergol/mcp-swagger/internal/policy"
	"github.com/blanergol/mcp-swagger/internal/prompt"
	resource "github.com/blanergol/mcp-swagger/internal/resouce"
	"github.com/blanergol/mcp-swagger/internal/server"
	"github.com/blanergol/mcp-swagger/internal/server/stdio"
	"github.com/blanergol/mcp-swagger/internal/server/streamable"
	"github.com/blanergol/mcp-swagger/internal/swagger"
	"github.com/blanergol/mcp-swagger/internal/tool"
	"github.com/blanergol/mcp-swagger/internal/upstreamauth"
	"github.com/blanergol/mcp-swagger/internal/usecase"
)

// main инициализирует зависимости, валидирует конфигурацию и запускает выбранный транспорт сервера.
func main() {
	cfg, err := loadConfigFromArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}

	configureLogger(cfg.LogLevel)
	for _, warning := range cfg.CompatibilityWarnings {
		slog.Warn("legacy oauth env fallback in use", "warning", warning)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	swaggerGuard := buildSwaggerGuard(cfg)
	upstreamGuard := buildUpstreamGuard(cfg)
	if err := validateConfiguredTargets(context.Background(), cfg, swaggerGuard, upstreamGuard); err != nil {
		log.Fatal(err)
	}

	swaggerStore := buildSwaggerStore(cfg, swaggerGuard)
	if err := validateSwaggerTargets(context.Background(), cfg, swaggerStore, upstreamGuard); err != nil {
		log.Fatal(err)
	}
	resourceStore := buildResourceStore(cfg, swaggerStore)
	promptStore := prompt.NewMemoryStore()

	policyEvaluator := buildPolicyEvaluator(cfg)
	upstreamAuth, err := buildUpstreamAuthProvider(cfg)
	if err != nil {
		log.Fatal(err)
	}
	httpDoer := buildHTTPDoer(cfg, upstreamGuard)
	auditLogger := buildAuditLogger(cfg)
	metricsRecorder := metrics.NewPrometheusRecorder()
	confirmationStore := confirmation.NewMemoryStore(cfg.ConfirmationTTL)

	registry := tool.NewRegistry(
		tool.NewEchoTool(),
		tool.NewHealthTool(cfg.Version),
		tool.NewPolicyRequestConfirmationTool(confirmationStore),
		tool.NewPolicyConfirmTool(confirmationStore),
		tool.NewSwaggerGeneratePayloadTool(swaggerStore),
		tool.NewSwaggerPrepareRequestTool(swaggerStore, cfg.MaxRequestBytes, cfg.UserAgent),
		tool.NewSwaggerValidateRequestTool(swaggerStore),
		tool.NewSwaggerValidateResponseTool(swaggerStore),
		tool.NewSwaggerSearchTool(swaggerStore),
		tool.NewSwaggerPlanCallTool(swaggerStore),
		tool.NewSwaggerExecuteTool(tool.SwaggerExecuteDependencies{
			Store:         swaggerStore,
			Policy:        policyEvaluator,
			AuthProvider:  upstreamAuth,
			HTTPDoer:      httpDoer,
			Auditor:       auditLogger,
			Metrics:       metricsRecorder,
			Confirmations: confirmationStore,
			Options: tool.SwaggerExecuteOptions{
				Mode:                   cfg.MCPAPIMode,
				UpstreamBaseURL:        cfg.UpstreamBaseURL,
				UpstreamSandboxBaseURL: cfg.UpstreamSandboxBaseURL,
				ValidateRequest:        cfg.ValidateRequest,
				ValidateResponse:       cfg.ValidateResponse,
				MaxRequestBytes:        cfg.MaxRequestBytes,
				MaxResponseBytes:       cfg.MaxResponseBytes,
				UserAgent:              cfg.UserAgent,
				CorrelationIDHeader:    cfg.CorrelationIDHeader,
				ValidateURL:            upstreamGuard.ValidateURL,
			},
		}),
	)

	service := usecase.NewService(registry, promptStore, resourceStore, swaggerStore)

	transport, err := buildTransport(cfg, service, registry, promptStore, resourceStore, metricsRecorder)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := transport.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTPShutdownTimeout)
	defer cancel()
	if err := transport.Stop(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("transport stop failed: %v", err)
	}
}

// buildResourceStore собирает зависимость или конфигурационный объект для текущего слоя.
func buildResourceStore(cfg config.Config, swaggerStore swagger.Store) resource.Store {
	stores := []resource.Store{resource.NewDocsStore(), resource.NewMemoryStore()}
	if strings.TrimSpace(cfg.SwaggerPath) != "" {
		stores = append([]resource.Store{resource.NewSwaggerStore(swaggerStore)}, stores...)
	}
	return resource.NewCompositeStore(stores...)
}

// buildSwaggerStore собирает зависимость или конфигурационный объект для текущего слоя.
func buildSwaggerStore(cfg config.Config, swaggerGuard *netguard.Guard) swagger.Store {
	if strings.TrimSpace(cfg.SwaggerPath) == "" {
		return swagger.NewNoopStore()
	}
	loaderOptions := []swagger.SourceLoaderOption{
		swagger.WithHTTPTimeout(cfg.SwaggerHTTPTimeout),
		swagger.WithMaxBytes(cfg.SwaggerMaxBytes),
		swagger.WithUserAgent(cfg.SwaggerUserAgent),
	}
	if swaggerGuard != nil {
		loaderOptions = append(loaderOptions, swagger.WithURLValidator(swaggerGuard.ValidateURL))
	}
	loader := swagger.NewSourceLoader(cfg.SwaggerPath, nil, loaderOptions...)
	parser := swagger.NewOpenAPIParser(cfg.ResolveSwaggerFormat())
	resolver := swagger.NewOpenAPIResolver(cfg.SwaggerBaseURL)
	return swagger.NewCachedStore(loader, parser, resolver, swagger.CachedStoreOptions{
		Reload:   cfg.SwaggerReload,
		CacheTTL: cfg.SwaggerCacheTTL,
	})
}

// buildPolicyEvaluator собирает зависимость или конфигурационный объект для текущего слоя.
func buildPolicyEvaluator(cfg config.Config) policy.Evaluator {
	return policy.NewEvaluator(policy.Config{
		Mode:                        cfg.MCPAPIMode,
		AllowedMethods:              cfg.AllowedMethods,
		DeniedMethods:               cfg.DeniedMethods,
		AllowedOperationIDs:         cfg.AllowedOperationIDs,
		DeniedOperationIDs:          cfg.DeniedOperationIDs,
		RequireConfirmationForWrite: cfg.RequireConfirmationForWrite,
	})
}

// buildHTTPDoer собирает зависимость или конфигурационный объект для текущего слоя.
func buildHTTPDoer(cfg config.Config, upstreamGuard *netguard.Guard) httpclient.Doer {
	var validator func(context.Context, string) error
	if upstreamGuard != nil {
		validator = upstreamGuard.ValidateURL
	}
	return httpclient.New(httpclient.Options{
		Timeout:                 cfg.HTTPTimeout,
		MaxConcurrent:           cfg.MaxConcurrentCalls,
		MaxCallsPerMinute:       cfg.MaxCallsPerMinute,
		MaxConcurrentPerKey:     cfg.MaxConcurrentCallsPerPrincipal,
		MaxCallsPerMinutePerKey: cfg.MaxCallsPerMinutePerPrincipal,
		MaxRequestBytes:         cfg.MaxRequestBytes,
		ValidateURL:             validator,
	})
}

// buildAuditLogger собирает зависимость или конфигурационный объект для текущего слоя.
func buildAuditLogger(cfg config.Config) audit.Logger {
	return audit.NewLogger(cfg.AuditLog, cfg.RedactHeaders, cfg.RedactJSONFields)
}

// buildUpstreamAuthProvider собирает зависимость или конфигурационный объект для текущего слоя.
func buildUpstreamAuthProvider(cfg config.Config) (upstreamauth.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.UpstreamAuthMode)) {
	case "", "none":
		return upstreamauth.NewNoopProvider(), nil
	case "static_bearer":
		return upstreamauth.NewStaticBearerProvider(cfg.UpstreamBearerToken)
	case "api_key":
		return upstreamauth.NewAPIKeyProvider(cfg.UpstreamAPIKeyHeader, cfg.UpstreamAPIKeyValue)
	case "oauth_client_credentials":
		return upstreamauth.NewOAuthClientCredentialsProvider(upstreamauth.OAuthClientCredentialsOptions{
			TokenURL:     cfg.UpstreamOAuthTokenURL,
			ClientID:     cfg.UpstreamOAuthClientID,
			ClientSecret: cfg.UpstreamOAuthClientSecret,
			Scopes:       cfg.UpstreamOAuthScopes,
			Audience:     cfg.UpstreamOAuthAudience,
			CacheTTL:     cfg.UpstreamOAuthTokenCacheTTL,
		})
	default:
		return nil, errors.New("unsupported UPSTREAM_AUTH_MODE: use none|oauth_client_credentials|static_bearer|api_key")
	}
}

// buildTransport собирает зависимость или конфигурационный объект для текущего слоя.
func buildTransport(
	cfg config.Config,
	svc usecase.Service,
	registry tool.Registry,
	promptStore prompt.Store,
	resourceStore resource.Store,
	metricsRecorder metrics.Recorder,
) (server.Transport, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Transport)) {
	case "", "stdio":
		return stdio.New(cfg, svc, registry, promptStore, resourceStore), nil
	case "streamable":
		validator, err := buildValidator(cfg)
		if err != nil {
			return nil, err
		}
		return streamable.New(cfg, svc, registry, promptStore, resourceStore, validator, metricsRecorder), nil
	default:
		return nil, errors.New("unsupported TRANSPORT: use stdio or streamable")
	}
}

// buildValidator собирает зависимость или конфигурационный объект для текущего слоя.
func buildValidator(cfg config.Config) (authn.Validator, error) {
	switch cfg.OAuthMode() {
	case "jwks":
		return authn.NewJWTJWKSValidator(authn.JWTJWKSOptions{
			Issuer:         cfg.InboundOAuthIssuer,
			Audience:       cfg.InboundOAuthAudience,
			JWKSURL:        cfg.InboundOAuthJWKSURL,
			RequiredScopes: cfg.InboundOAuthRequiredScopes,
			CacheTTL:       cfg.InboundOAuthJWKSCacheTTL,
		})
	case "introspection":
		return authn.NewIntrospectionValidator(authn.IntrospectionOptions{
			IntrospectionURL: cfg.InboundOAuthIntrospectionURL,
			ClientID:         cfg.InboundOAuthClientID,
			ClientSecret:     cfg.InboundOAuthClientSecret,
			Issuer:           cfg.InboundOAuthIssuer,
			Audience:         cfg.InboundOAuthAudience,
			RequiredScopes:   cfg.InboundOAuthRequiredScopes,
			CacheTTL:         cfg.InboundOAuthIntrospectionCacheTTL,
		})
	default:
		return nil, errors.New("streamable transport requires inbound OAuth config (set INBOUND_OAUTH_JWKS_URL or INBOUND_OAUTH_INTROSPECTION_URL)")
	}
}

// buildSwaggerGuard собирает зависимость или конфигурационный объект для текущего слоя.
func buildSwaggerGuard(cfg config.Config) *netguard.Guard {
	return netguard.New(netguard.Config{
		AllowedHosts:         cfg.SwaggerAllowedHosts,
		BlockPrivateNetworks: cfg.BlockPrivateNetworks,
	})
}

// buildUpstreamGuard собирает зависимость или конфигурационный объект для текущего слоя.
func buildUpstreamGuard(cfg config.Config) *netguard.Guard {
	return netguard.New(netguard.Config{
		AllowedHosts:         cfg.UpstreamAllowedHosts,
		BlockPrivateNetworks: cfg.BlockPrivateNetworks,
	})
}

// validateConfiguredTargets выполняет проверку входных данных и возвращает ошибку при нарушении инвариантов.
func validateConfiguredTargets(
	ctx context.Context,
	cfg config.Config,
	swaggerGuard *netguard.Guard,
	upstreamGuard *netguard.Guard,
) error {
	if upstreamGuard == nil || swaggerGuard == nil {
		return nil
	}

	if value := strings.TrimSpace(cfg.UpstreamBaseURL); value != "" {
		if err := upstreamGuard.ValidateURL(ctx, value); err != nil {
			return fmt.Errorf("UPSTREAM_BASE_URL blocked by policy: %w", err)
		}
	}
	if value := strings.TrimSpace(cfg.UpstreamSandboxBaseURL); value != "" {
		if err := upstreamGuard.ValidateURL(ctx, value); err != nil {
			return fmt.Errorf("UPSTREAM_SANDBOX_BASE_URL blocked by policy: %w", err)
		}
	}
	if value := strings.TrimSpace(cfg.SwaggerBaseURL); value != "" {
		if err := upstreamGuard.ValidateURL(ctx, value); err != nil {
			return fmt.Errorf("SWAGGER_BASE_URL blocked by policy: %w", err)
		}
	}

	if isHTTPURL(cfg.SwaggerPath) {
		if err := swaggerGuard.ValidateURL(ctx, cfg.SwaggerPath); err != nil {
			return fmt.Errorf("SWAGGER_PATH blocked by policy: %w", err)
		}
	}
	return nil
}

// validateSwaggerTargets выполняет проверку входных данных и возвращает ошибку при нарушении инвариантов.
func validateSwaggerTargets(
	ctx context.Context,
	cfg config.Config,
	swaggerStore swagger.Store,
	upstreamGuard *netguard.Guard,
) error {
	if upstreamGuard == nil {
		return nil
	}
	if strings.TrimSpace(cfg.SwaggerPath) == "" {
		return nil
	}

	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	endpoints, err := swaggerStore.ListEndpoints(checkCtx)
	if err != nil {
		return fmt.Errorf("failed to load swagger during startup validation: %w", err)
	}

	seen := make(map[string]struct{})
	validate := func(label, rawURL string) error {
		urlValue := strings.TrimSpace(rawURL)
		if urlValue == "" {
			return nil
		}
		if _, ok := seen[urlValue]; ok {
			return nil
		}
		seen[urlValue] = struct{}{}
		if err := upstreamGuard.ValidateURL(checkCtx, urlValue); err != nil {
			return fmt.Errorf("%s blocked by policy: %w", label, err)
		}
		return nil
	}

	for _, endpoint := range endpoints {
		for _, serverURL := range endpoint.Servers {
			if err := validate("swagger server for operation "+endpoint.OperationID, serverURL); err != nil {
				return err
			}
		}
		if err := validate("swagger baseURL for operation "+endpoint.OperationID, endpoint.BaseURL); err != nil {
			return err
		}
	}
	return nil
}

// isHTTPURL возвращает true только когда вход удовлетворяет правилам, используемым в текущей проверке.
func isHTTPURL(source string) bool {
	raw := strings.TrimSpace(source)
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(raw), scheme+"://")
}

// configureLogger выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func configureLogger(level string) {
	var slogLevel slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel})
	slog.SetDefault(slog.New(h))
}

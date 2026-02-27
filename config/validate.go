package config

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/blanergol/mcp-swagger/internal/netguard"
)

// validHTTPMethods хранит служебное значение, используемое внутри текущего пакета.
var validHTTPMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodHead:    {},
	http.MethodOptions: {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodPatch:   {},
	http.MethodDelete:  {},
	http.MethodTrace:   {},
	http.MethodConnect: {},
}

// Validate проверяет static config для fail-fast startup errors.
func (c Config) Validate() error {
	errs := make([]string, 0)

	transport := strings.ToLower(strings.TrimSpace(c.Transport))
	mode := strings.ToLower(strings.TrimSpace(c.MCPAPIMode))

	if strings.TrimSpace(c.SwaggerPath) == "" {
		errs = append(errs, "SWAGGER_PATH is required")
	}
	if c.SwaggerHTTPTimeout <= 0 {
		errs = append(errs, "SWAGGER_HTTP_TIMEOUT must be > 0")
	}
	if c.SwaggerMaxBytes <= 0 {
		errs = append(errs, "SWAGGER_MAX_BYTES must be > 0")
	}

	switch transport {
	case "", "stdio", "streamable":
	default:
		errs = append(errs, fmt.Sprintf("TRANSPORT %q is unsupported (use stdio|streamable)", c.Transport))
	}

	switch mode {
	case "", "plan_only", "execute_readonly", "execute_write", "sandbox":
	default:
		errs = append(errs, fmt.Sprintf("MCP_API_MODE %q is invalid", c.MCPAPIMode))
	}

	if transport == "streamable" && c.OAuthMode() == "none" {
		errs = append(errs, "TRANSPORT=streamable requires inbound OAuth config (INBOUND_OAUTH_JWKS_URL or INBOUND_OAUTH_INTROSPECTION_URL)")
	}

	if mode == "sandbox" && strings.TrimSpace(c.UpstreamSandboxBaseURL) == "" {
		errs = append(errs, "MCP_API_MODE=sandbox requires UPSTREAM_SANDBOX_BASE_URL")
	}

	invalidAllowedMethods := invalidMethods(c.AllowedMethods)
	if len(invalidAllowedMethods) > 0 {
		errs = append(errs, fmt.Sprintf("ALLOWED_METHODS contains invalid verbs: %s", strings.Join(invalidAllowedMethods, ", ")))
	}
	invalidDeniedMethods := invalidMethods(c.DeniedMethods)
	if len(invalidDeniedMethods) > 0 {
		errs = append(errs, fmt.Sprintf("DENIED_METHODS contains invalid verbs: %s", strings.Join(invalidDeniedMethods, ", ")))
	}

	swaggerGuard := netguard.New(netguard.Config{
		AllowedHosts:         c.SwaggerAllowedHosts,
		BlockPrivateNetworks: c.BlockPrivateNetworks,
	})
	upstreamGuard := netguard.New(netguard.Config{
		AllowedHosts:         c.UpstreamAllowedHosts,
		BlockPrivateNetworks: c.BlockPrivateNetworks,
	})

	ctx := context.Background()
	if parsed, isURL := parseURLSource(c.SwaggerPath); isURL {
		scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
		if scheme != "http" && scheme != "https" {
			errs = append(errs, fmt.Sprintf("SWAGGER_PATH URL scheme %q is unsupported (only http/https are allowed)", parsed.Scheme))
		}
	}
	if isHTTPURL(c.SwaggerPath) {
		if err := swaggerGuard.ValidateURL(ctx, c.SwaggerPath); err != nil {
			errs = append(errs, fmt.Sprintf("SWAGGER_PATH is blocked by host policy: %v", err))
		}
	}

	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "UPSTREAM_BASE_URL", value: c.UpstreamBaseURL},
		{name: "UPSTREAM_SANDBOX_BASE_URL", value: c.UpstreamSandboxBaseURL},
		{name: "SWAGGER_BASE_URL", value: c.SwaggerBaseURL},
	} {
		if strings.TrimSpace(item.value) == "" {
			continue
		}
		if err := upstreamGuard.ValidateURL(ctx, item.value); err != nil {
			errs = append(errs, fmt.Sprintf("%s is blocked by host policy: %v", item.name, err))
		}
	}

	if selected, selectedName := c.selectedUpstreamBaseURL(); selected != "" && len(c.UpstreamAllowedHosts) > 0 {
		if err := upstreamGuard.ValidateURL(ctx, selected); err != nil {
			errs = append(errs, fmt.Sprintf("selected upstream host (%s) is blocked by allowlist/policy: %v", selectedName, err))
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration: %s", strings.Join(errs, "; "))
}

// selectedUpstreamBaseURL выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (c Config) selectedUpstreamBaseURL() (value string, source string) {
	mode := strings.ToLower(strings.TrimSpace(c.MCPAPIMode))
	if mode == "sandbox" {
		return strings.TrimSpace(c.UpstreamSandboxBaseURL), "UPSTREAM_SANDBOX_BASE_URL"
	}
	if v := strings.TrimSpace(c.UpstreamBaseURL); v != "" {
		return v, "UPSTREAM_BASE_URL"
	}
	if v := strings.TrimSpace(c.SwaggerBaseURL); v != "" {
		return v, "SWAGGER_BASE_URL"
	}
	return "", ""
}

// invalidMethods выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func invalidMethods(methods []string) []string {
	if len(methods) == 0 {
		return nil
	}
	invalid := make([]string, 0)
	for _, method := range methods {
		value := strings.ToUpper(strings.TrimSpace(method))
		if value == "" {
			continue
		}
		if _, ok := validHTTPMethods[value]; !ok {
			invalid = append(invalid, value)
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	sort.Strings(invalid)
	return invalid
}

// isHTTPURL возвращает true только когда вход удовлетворяет правилам, используемым в текущей проверке.
func isHTTPURL(source string) bool {
	parsed, ok := parseURLSource(source)
	if !ok || parsed == nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	return scheme == "http" || scheme == "https"
}

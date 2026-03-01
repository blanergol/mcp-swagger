package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/blanergol/mcp-swagger/config"
)

// stringSliceFlag stores repeatable string flag values.
type stringSliceFlag []string

// String returns current flag value.
func (f *stringSliceFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

// Set appends a value for repeatable flag usage.
func (f *stringSliceFlag) Set(value string) error {
	*f = append(*f, strings.TrimSpace(value))
	return nil
}

type envSnapshot struct {
	key    string
	value  string
	exists bool
}

// loadConfigFromArgs loads env config and applies CLI overrides.
func loadConfigFromArgs(args []string) (config.Config, error) {
	cfg := config.Load()

	fs := flag.NewFlagSet("mcp-server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	transport := fs.String("transport", cfg.Transport, "MCP transport: stdio|streamable")
	httpAddr := fs.String("http-addr", cfg.HTTPAddr, "HTTP bind address (for streamable transport)")
	version := fs.String("version", cfg.Version, "Service version exposed in metadata/health")
	logLevel := fs.String("log-level", cfg.LogLevel, "Log level: debug|info|warn|error")

	swaggerPath := fs.String("swagger-path", cfg.SwaggerPath, "Path or URL to OpenAPI/Swagger spec")
	swaggerFormat := fs.String("swagger-format", cfg.SwaggerFormat, "Swagger format: auto|json|yaml")
	swaggerBaseURL := fs.String("swagger-base-url", cfg.SwaggerBaseURL, "Override base URL for resolved operations")
	swaggerReload := fs.Bool("swagger-reload", cfg.SwaggerReload, "Reload swagger source on each request")

	mcpAPIMode := fs.String("mcp-api-mode", cfg.MCPAPIMode, "MCP API mode: plan_only|execute_readonly|execute_write|sandbox")
	upstreamBaseURL := fs.String("upstream-base-url", cfg.UpstreamBaseURL, "Primary upstream base URL")
	upstreamSandboxBaseURL := fs.String("upstream-sandbox-base-url", cfg.UpstreamSandboxBaseURL, "Sandbox upstream base URL")

	var setOverrides stringSliceFlag
	fs.Var(&setOverrides, "set", "Set any config ENV as KEY=VALUE (repeatable), e.g. --set HTTP_TIMEOUT=45s")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: %s [flags]\n\n", os.Args[0])
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return config.Config{}, err
	}
	if len(fs.Args()) > 0 {
		return config.Config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	hasFlag := map[string]struct{}{}
	fs.Visit(func(f *flag.Flag) {
		hasFlag[f.Name] = struct{}{}
	})

	if len(setOverrides) > 0 {
		restore, err := applySetEnvOverrides(setOverrides)
		if err != nil {
			return config.Config{}, err
		}
		defer restore()
		cfg = config.Load()
	}

	if _, ok := hasFlag["transport"]; ok {
		cfg.Transport = strings.ToLower(strings.TrimSpace(*transport))
	}
	if _, ok := hasFlag["http-addr"]; ok {
		cfg.HTTPAddr = strings.TrimSpace(*httpAddr)
	}
	if _, ok := hasFlag["version"]; ok {
		cfg.Version = strings.TrimSpace(*version)
	}
	if _, ok := hasFlag["log-level"]; ok {
		cfg.LogLevel = strings.ToLower(strings.TrimSpace(*logLevel))
	}
	if _, ok := hasFlag["swagger-path"]; ok {
		cfg.SwaggerPath = strings.TrimSpace(*swaggerPath)
	}
	if _, ok := hasFlag["swagger-format"]; ok {
		cfg.SwaggerFormat = strings.ToLower(strings.TrimSpace(*swaggerFormat))
	}
	if _, ok := hasFlag["swagger-base-url"]; ok {
		cfg.SwaggerBaseURL = strings.TrimSpace(*swaggerBaseURL)
	}
	if _, ok := hasFlag["swagger-reload"]; ok {
		cfg.SwaggerReload = *swaggerReload
	}
	if _, ok := hasFlag["mcp-api-mode"]; ok {
		cfg.MCPAPIMode = strings.ToLower(strings.TrimSpace(*mcpAPIMode))
	}
	if _, ok := hasFlag["upstream-base-url"]; ok {
		cfg.UpstreamBaseURL = strings.TrimSpace(*upstreamBaseURL)
	}
	if _, ok := hasFlag["upstream-sandbox-base-url"]; ok {
		cfg.UpstreamSandboxBaseURL = strings.TrimSpace(*upstreamSandboxBaseURL)
	}

	cfg.SwaggerFormat = cfg.ResolveSwaggerFormat()
	return cfg, nil
}

func applySetEnvOverrides(assignments []string) (func(), error) {
	snapshots := make([]envSnapshot, 0, len(assignments))
	seenKeys := make(map[string]struct{}, len(assignments))

	for _, assignment := range assignments {
		key, value, err := parseEnvAssignment(assignment)
		if err != nil {
			return nil, err
		}

		if _, seen := seenKeys[key]; !seen {
			currentValue, exists := os.LookupEnv(key)
			snapshots = append(snapshots, envSnapshot{
				key:    key,
				value:  currentValue,
				exists: exists,
			})
			seenKeys[key] = struct{}{}
		}

		if err := os.Setenv(key, value); err != nil {
			return nil, fmt.Errorf("failed to apply --set for %s: %w", key, err)
		}
	}

	restore := func() {
		for _, snapshot := range snapshots {
			if snapshot.exists {
				_ = os.Setenv(snapshot.key, snapshot.value)
				continue
			}
			_ = os.Unsetenv(snapshot.key)
		}
	}
	return restore, nil
}

func parseEnvAssignment(raw string) (string, string, error) {
	assignment := strings.TrimSpace(raw)
	if assignment == "" {
		return "", "", fmt.Errorf("invalid --set value %q: expected KEY=VALUE", raw)
	}

	parts := strings.SplitN(assignment, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid --set value %q: expected KEY=VALUE", raw)
	}

	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", fmt.Errorf("invalid --set value %q: key is empty", raw)
	}
	return key, parts[1], nil
}

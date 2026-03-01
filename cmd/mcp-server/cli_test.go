package main

import (
	"strings"
	"testing"
)

func TestLoadConfigFromArgsNamedFlagsOverrideEnv(t *testing.T) {
	t.Setenv("TRANSPORT", "stdio")
	t.Setenv("SWAGGER_PATH", "./env-openapi.yaml")
	t.Setenv("MCP_API_MODE", "plan_only")
	t.Setenv("LOG_LEVEL", "info")

	cfg, err := loadConfigFromArgs([]string{
		"--transport=streamable",
		"--swagger-path=./cli-openapi.yaml",
		"--mcp-api-mode=execute_readonly",
		"--log-level=debug",
	})
	if err != nil {
		t.Fatalf("loadConfigFromArgs returned error: %v", err)
	}

	if cfg.Transport != "streamable" {
		t.Fatalf("expected transport=streamable, got %q", cfg.Transport)
	}
	if cfg.SwaggerPath != "./cli-openapi.yaml" {
		t.Fatalf("expected swagger-path from cli, got %q", cfg.SwaggerPath)
	}
	if cfg.MCPAPIMode != "execute_readonly" {
		t.Fatalf("expected mcp-api-mode from cli, got %q", cfg.MCPAPIMode)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("expected log-level=debug, got %q", cfg.LogLevel)
	}
}

func TestLoadConfigFromArgsSetOverridesAreApplied(t *testing.T) {
	t.Setenv("TRANSPORT", "stdio")
	t.Setenv("SWAGGER_PATH", "./env-openapi.yaml")

	cfg, err := loadConfigFromArgs([]string{
		"--set", "TRANSPORT=streamable",
		"--set", "SWAGGER_PATH=./set-openapi.yaml",
		"--set", "MCP_API_MODE=execute_write",
	})
	if err != nil {
		t.Fatalf("loadConfigFromArgs returned error: %v", err)
	}

	if cfg.Transport != "streamable" {
		t.Fatalf("expected transport from --set, got %q", cfg.Transport)
	}
	if cfg.SwaggerPath != "./set-openapi.yaml" {
		t.Fatalf("expected swagger-path from --set, got %q", cfg.SwaggerPath)
	}
	if cfg.MCPAPIMode != "execute_write" {
		t.Fatalf("expected mcp-api-mode from --set, got %q", cfg.MCPAPIMode)
	}
}

func TestLoadConfigFromArgsNamedFlagBeatsSet(t *testing.T) {
	t.Setenv("TRANSPORT", "stdio")

	cfg, err := loadConfigFromArgs([]string{
		"--set", "TRANSPORT=streamable",
		"--transport=stdio",
	})
	if err != nil {
		t.Fatalf("loadConfigFromArgs returned error: %v", err)
	}

	if cfg.Transport != "stdio" {
		t.Fatalf("expected --transport to override --set, got %q", cfg.Transport)
	}
}

func TestLoadConfigFromArgsRejectsInvalidSetFormat(t *testing.T) {
	_, err := loadConfigFromArgs([]string{"--set", "MALFORMED_VALUE"})
	if err == nil {
		t.Fatal("expected error for invalid --set format")
	}
	if !strings.Contains(err.Error(), "KEY=VALUE") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseEnvAssignmentAllowsEmptyValue(t *testing.T) {
	key, value, err := parseEnvAssignment("UPSTREAM_BASE_URL=")
	if err != nil {
		t.Fatalf("parseEnvAssignment returned error: %v", err)
	}
	if key != "UPSTREAM_BASE_URL" {
		t.Fatalf("expected key=UPSTREAM_BASE_URL, got %q", key)
	}
	if value != "" {
		t.Fatalf("expected empty value, got %q", value)
	}
}

package tool

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// TestSwaggerSearchBySchemaUsage проверяет ожидаемое поведение в тестовом сценарии.
func TestSwaggerSearchBySchemaUsage(t *testing.T) {
	t.Parallel()

	searchTool := NewSwaggerSearchTool(newSwaggerStoreForSearchTests(t))
	out, err := searchTool.Execute(context.Background(), map[string]any{
		"params": map[string]any{
			"query": map[string]any{
				"schema":  "CreateUserRequest",
				"include": []string{"endpoints", "usage"},
			},
		},
	})
	if err != nil {
		t.Fatalf("swagger.search execution failed: %v", err)
	}

	data := mustToolResultData(t, out)
	results := asResultObjectSlice(data["results"])
	if len(results) == 0 {
		t.Fatalf("expected non-empty results for schema search, got %#v", data["results"])
	}

	foundCreateUser := false
	for _, entry := range results {
		if strings.TrimSpace(valueAsString(entry["operationId"])) != "createUser" {
			continue
		}
		foundCreateUser = true
		reasons := asStringSlice(entry["matchReason"])
		if !containsSubstring(reasons, "schema used in request body") {
			t.Fatalf("expected matchReason to mention schema request usage, got %#v", reasons)
		}
		if score, ok := entry["score"].(float64); !ok || score <= 0 {
			t.Fatalf("expected positive score, got %#v", entry["score"])
		}
	}
	if !foundCreateUser {
		t.Fatalf("schema search must include createUser operation, got %#v", results)
	}
}

// TestSwaggerSearchByErrorStatus проверяет ожидаемое поведение в тестовом сценарии.
func TestSwaggerSearchByErrorStatus(t *testing.T) {
	t.Parallel()

	searchTool := NewSwaggerSearchTool(newSwaggerStoreForSearchTests(t))
	out, err := searchTool.Execute(context.Background(), map[string]any{
		"status":  404,
		"include": []string{"endpoints"},
	})
	if err != nil {
		t.Fatalf("swagger.search execution failed: %v", err)
	}

	data := mustToolResultData(t, out)
	results := asResultObjectSlice(data["results"])
	if len(results) == 0 {
		t.Fatalf("expected non-empty results for status=404 search, got %#v", data["results"])
	}

	for _, entry := range results {
		reasons := asStringSlice(entry["matchReason"])
		if containsSubstring(reasons, "error response status 404") {
			return
		}
	}
	t.Fatalf("expected at least one result with error response status 404 reason, got %#v", results)
}

// asResultObjectSlice выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func asResultObjectSlice(value any) []map[string]any {
	switch v := value.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if entry, ok := item.(map[string]any); ok {
				out = append(out, entry)
			}
		}
		return out
	default:
		return nil
	}
}

// newSwaggerStoreForSearchTests инициализирует внутреннюю реализацию с безопасными значениями по умолчанию.
func newSwaggerStoreForSearchTests(t *testing.T) swagger.Store {
	t.Helper()

	source := filepath.Join("..", "swagger", "testdata", "openapi.yaml")
	loader := swagger.NewSourceLoader(source, nil)
	parser := swagger.NewOpenAPIParser("auto")
	resolver := swagger.NewOpenAPIResolver("")
	store := swagger.NewCachedStore(loader, parser, resolver, swagger.CachedStoreOptions{
		Reload:   false,
		CacheTTL: time.Minute,
	})

	if _, err := store.ListEndpoints(context.Background()); err != nil {
		t.Fatalf("failed to preload swagger testdata %s: %v", source, err)
	}
	return store
}

package tool

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// TestGeneratePayloadMinimalObjectWithConstraints проверяет ожидаемое поведение в тестовом сценарии.
func TestGeneratePayloadMinimalObjectWithConstraints(t *testing.T) {
	t.Parallel()

	tool := NewSwaggerGeneratePayloadTool(newSwaggerStoreForPayloadTests(t))

	out, err := tool.Execute(context.Background(), map[string]any{
		"operationId": "createUserPayload",
		"params": map[string]any{
			"query": map[string]any{
				"strategy": "minimal",
				"seed":     42,
			},
		},
	})
	if err != nil {
		t.Fatalf("execute generate_payload: %v", err)
	}

	result := mustToolResultData(t, out)
	body, ok := result["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected object body, got %T", result["body"])
	}

	name, _ := body["name"].(string)
	if len(name) < 3 {
		t.Fatalf("generated name must satisfy minLength=3, got %q", name)
	}
	if role, _ := body["role"].(string); role != "admin" {
		t.Fatalf("generated enum value must use first option, got %q", role)
	}
	if age, ok := numericAsFloat64(body["age"]); !ok || age < 18 {
		t.Fatalf("generated age must satisfy minimum=18, got %#v", body["age"])
	}

	contact, ok := body["contact"].(map[string]any)
	if !ok {
		t.Fatalf("generated contact must be object, got %T", body["contact"])
	}
	email, _ := contact["email"].(string)
	if !strings.Contains(email, "@") {
		t.Fatalf("generated oneOf[0] contact email must look like email, got %q", email)
	}

	warnings := asStringSlice(result["warnings"])
	if !containsSubstring(warnings, "oneOf") {
		t.Fatalf("warnings must mention oneOf branch selection, got %#v", warnings)
	}
}

// TestGeneratePayloadExampleStrategyUsesExample проверяет ожидаемое поведение в тестовом сценарии.
func TestGeneratePayloadExampleStrategyUsesExample(t *testing.T) {
	t.Parallel()

	tool := NewSwaggerGeneratePayloadTool(newSwaggerStoreForPayloadTests(t))

	out, err := tool.Execute(context.Background(), map[string]any{
		"operationId": "createNotePayload",
		"params": map[string]any{
			"query": map[string]any{
				"strategy": "example",
			},
		},
	})
	if err != nil {
		t.Fatalf("execute generate_payload: %v", err)
	}

	result := mustToolResultData(t, out)
	body, ok := result["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected example body object, got %T", result["body"])
	}
	if body["title"] != "Example note title" {
		t.Fatalf("expected schema example title, got %#v", body["title"])
	}
	if body["priority"] != float64(3) {
		t.Fatalf("expected schema example priority=3, got %#v", body["priority"])
	}
	assertEmptyArrayValue(t, result["warnings"], "warnings")
}

// TestGeneratePayloadPrimitiveAndArray проверяет ожидаемое поведение в тестовом сценарии.
func TestGeneratePayloadPrimitiveAndArray(t *testing.T) {
	t.Parallel()

	tool := NewSwaggerGeneratePayloadTool(newSwaggerStoreForPayloadTests(t))

	primitiveOut, err := tool.Execute(context.Background(), map[string]any{
		"operationId": "setFlagPayload",
	})
	if err != nil {
		t.Fatalf("execute generate_payload primitive: %v", err)
	}
	primitiveData := mustToolResultData(t, primitiveOut)
	if _, ok := primitiveData["body"].(bool); !ok {
		t.Fatalf("expected boolean payload body, got %T", primitiveData["body"])
	}

	arrayOut, err := tool.Execute(context.Background(), map[string]any{
		"operationId": "setLimitPayload",
		"params": map[string]any{
			"query": map[string]any{
				"strategy": "minimal",
			},
		},
	})
	if err != nil {
		t.Fatalf("execute generate_payload array: %v", err)
	}
	arrayData := mustToolResultData(t, arrayOut)
	items, ok := arrayData["body"].([]any)
	if !ok {
		t.Fatalf("expected array payload body, got %T", arrayData["body"])
	}
	if len(items) < 2 {
		t.Fatalf("expected minItems=2, got %d", len(items))
	}
	if first, ok := numericAsFloat64(items[0]); !ok || first < 5 {
		t.Fatalf("expected array item minimum=5, got %#v", items[0])
	}
}

// TestGeneratePayloadReturnsWarningWhenOperationHasNoRequestBody verifies graceful behavior for body-less operations.
func TestGeneratePayloadReturnsWarningWhenOperationHasNoRequestBody(t *testing.T) {
	t.Parallel()

	tool := NewSwaggerGeneratePayloadTool(newSwaggerStoreForPayloadTests(t))

	out, err := tool.Execute(context.Background(), map[string]any{
		"operationId": "getStatusPayload",
	})
	if err != nil {
		t.Fatalf("execute generate_payload for body-less operation: %v", err)
	}

	result := mustToolResultData(t, out)
	if body, exists := result["body"]; !exists || body != nil {
		t.Fatalf("expected nil body for body-less operation, got %#v", result["body"])
	}
	warnings := asStringSlice(result["warnings"])
	if !containsSubstring(warnings, "does not define request body schema") {
		t.Fatalf("warnings must explain missing request body schema, got %#v", warnings)
	}
}

// TestGeneratePayloadAppliesOverrides проверяет ожидаемое поведение в тестовом сценарии.
func TestGeneratePayloadAppliesOverrides(t *testing.T) {
	t.Parallel()

	tool := NewSwaggerGeneratePayloadTool(newSwaggerStoreForPayloadTests(t))

	out, err := tool.Execute(context.Background(), map[string]any{
		"operationId": "createUserPayload",
		"params": map[string]any{
			"query": map[string]any{
				"strategy": "minimal",
				"overrides": map[string]any{
					"role": "user",
					"contact": map[string]any{
						"email": "override@example.com",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute generate_payload: %v", err)
	}

	result := mustToolResultData(t, out)
	body, ok := result["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected object body, got %T", result["body"])
	}
	if body["role"] != "user" {
		t.Fatalf("expected overrides.role=user, got %#v", body["role"])
	}
	contact, _ := body["contact"].(map[string]any)
	if contact["email"] != "override@example.com" {
		t.Fatalf("expected overrides.contact.email applied, got %#v", contact["email"])
	}
}

// TestParseGeneratePayloadInputSupportsParamsQueryAndTopLevelFallback проверяет ожидаемое поведение в тестовом сценарии.
func TestParseGeneratePayloadInputSupportsParamsQueryAndTopLevelFallback(t *testing.T) {
	t.Parallel()

	parsed, err := parseGeneratePayloadInput(map[string]any{
		"operationId": "createUserPayload",
		"params": map[string]any{
			"query": map[string]any{
				"seed":     7,
				"strategy": "maximal",
				"overrides": map[string]any{
					"name": "alice",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("parseGeneratePayloadInput(params.query): %v", err)
	}
	if parsed.OperationID != "createUserPayload" || parsed.Seed != 7 || parsed.Strategy != payloadStrategyMaximal {
		t.Fatalf("unexpected parsed payload input: %+v", parsed)
	}
	if parsed.Overrides["name"] != "alice" {
		t.Fatalf("overrides were not parsed from params.query: %#v", parsed.Overrides)
	}

	legacyParsed, err := parseGeneratePayloadInput(map[string]any{
		"operationId": "createUserPayload",
		"seed":        11,
		"strategy":    "example",
		"overrides": map[string]any{
			"role": "user",
		},
	})
	if err != nil {
		t.Fatalf("parseGeneratePayloadInput(top-level): %v", err)
	}
	if legacyParsed.Seed != 11 || legacyParsed.Strategy != payloadStrategyExample {
		t.Fatalf("unexpected parsed top-level payload input: %+v", legacyParsed)
	}
	if legacyParsed.Overrides["role"] != "user" {
		t.Fatalf("overrides were not parsed from top-level: %#v", legacyParsed.Overrides)
	}
}

// newSwaggerStoreForPayloadTests инициализирует внутреннюю реализацию с безопасными значениями по умолчанию.
func newSwaggerStoreForPayloadTests(t *testing.T) swagger.Store {
	t.Helper()

	source := filepath.Join("testdata", "openapi_payload.yaml")
	loader := swagger.NewSourceLoader(source, nil)
	parser := swagger.NewOpenAPIParser("auto")
	resolver := swagger.NewOpenAPIResolver("")
	store := swagger.NewCachedStore(loader, parser, resolver, swagger.CachedStoreOptions{
		Reload:   false,
		CacheTTL: time.Minute,
	})

	_, err := store.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("load swagger testdata %s: %v", source, err)
	}
	return store
}

// mustToolResultData выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func mustToolResultData(t *testing.T, value any) map[string]any {
	t.Helper()

	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("tool output must be object, got %T", value)
	}
	okValue, ok := result["ok"].(bool)
	if !ok || !okValue {
		t.Fatalf("tool output is not ok: %#v", result)
	}
	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("tool output data must be object, got %T", result["data"])
	}
	return data
}

// asStringSlice выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func asStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(valueAsString(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// containsSubstring выполняет проверку соответствия по правилам текущего модуля.
func containsSubstring(items []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item), needle) {
			return true
		}
	}
	return false
}

// numericAsFloat64 выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func numericAsFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	default:
		return 0, false
	}
}

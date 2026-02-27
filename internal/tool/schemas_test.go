package tool

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/blanergol/mcp-swagger/internal/confirmation"
	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// requiredToolSchemas хранит служебное значение, используемое внутри текущего пакета.
var requiredToolSchemas = []string{
	ToolSwaggerSearch,
	ToolSwaggerPlanCall,
	ToolSwaggerHTTPGeneratePayload,
	ToolSwaggerHTTPPrepareRequest,
	ToolSwaggerHTTPValidateReq,
	ToolSwaggerHTTPExecute,
	ToolSwaggerHTTPValidateResp,
	ToolPolicyRequestConfirmation,
	ToolPolicyConfirm,
}

// TestToolSchemasCatalogContainsRequiredTools проверяет ожидаемое поведение в тестовом сценарии.
func TestToolSchemasCatalogContainsRequiredTools(t *testing.T) {
	t.Parallel()

	catalog := SchemasCatalog()
	for _, name := range requiredToolSchemas {
		bundle, ok := catalog[name]
		if !ok {
			t.Fatalf("tool schema %q is missing from catalog", name)
		}
		if bundle.InputSchema == nil {
			t.Fatalf("input schema for %q is nil", name)
		}
		if bundle.OutputSchema == nil {
			t.Fatalf("output schema for %q is nil", name)
		}
	}
}

// TestToolSchemaMethodsMatchCatalog проверяет ожидаемое поведение в тестовом сценарии.
func TestToolSchemaMethodsMatchCatalog(t *testing.T) {
	t.Parallel()

	swaggerStore := swagger.NewNoopStore()
	confirmStore := confirmation.NewMemoryStore(10 * time.Minute)
	tools := []Tool{
		NewSwaggerSearchTool(swaggerStore),
		NewSwaggerPlanCallTool(swaggerStore),
		NewSwaggerGeneratePayloadTool(swaggerStore),
		NewSwaggerPrepareRequestTool(swaggerStore, 1<<20, "test-agent"),
		NewSwaggerValidateRequestTool(swaggerStore),
		NewSwaggerExecuteTool(SwaggerExecuteDependencies{
			Store: swaggerStore,
		}),
		NewSwaggerValidateResponseTool(swaggerStore),
		NewPolicyRequestConfirmationTool(confirmStore),
		NewPolicyConfirmTool(confirmStore),
	}

	catalog := SchemasCatalog()
	for _, tt := range tools {
		tt := tt
		t.Run(tt.Name(), func(t *testing.T) {
			bundle, ok := catalog[tt.Name()]
			if !ok {
				t.Fatalf("missing schema for tool %q", tt.Name())
			}

			assertJSONShapeEqual(t, bundle.InputSchema, tt.InputSchema(), "inputSchema")
			assertJSONShapeEqual(t, bundle.OutputSchema, tt.OutputSchema(), "outputSchema")
		})
	}
}

// TestToolOutputSchemasUseUnifiedEnvelope проверяет ожидаемое поведение в тестовом сценарии.
func TestToolOutputSchemasUseUnifiedEnvelope(t *testing.T) {
	t.Parallel()

	catalog := SchemasCatalog()
	for _, name := range requiredToolSchemas {
		out := normalizeJSONValue(t, catalog[name].OutputSchema)
		outMap, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("output schema for %q must be object", name)
		}
		required, _ := outMap["required"].([]any)
		requiredKeys := make([]string, 0, len(required))
		for _, item := range required {
			if s, ok := item.(string); ok {
				requiredKeys = append(requiredKeys, s)
			}
		}
		for _, key := range []string{"ok", "data", "error"} {
			if !slices.Contains(requiredKeys, key) {
				t.Fatalf("output schema for %q must require %q", name, key)
			}
		}
	}
}

// TestToolsReturnUnifiedEnvelopeOnExecution проверяет ожидаемое поведение в тестовом сценарии.
func TestToolsReturnUnifiedEnvelopeOnExecution(t *testing.T) {
	t.Parallel()

	swaggerStore := swagger.NewNoopStore()
	confirmStore := confirmation.NewMemoryStore(5 * time.Minute)
	tools := []Tool{
		NewSwaggerSearchTool(swaggerStore),
		NewSwaggerPlanCallTool(swaggerStore),
		NewSwaggerGeneratePayloadTool(swaggerStore),
		NewSwaggerPrepareRequestTool(swaggerStore, 1<<20, "test-agent"),
		NewSwaggerValidateRequestTool(swaggerStore),
		NewSwaggerExecuteTool(SwaggerExecuteDependencies{
			Store: swaggerStore,
		}),
		NewSwaggerValidateResponseTool(swaggerStore),
		NewPolicyRequestConfirmationTool(confirmStore),
		NewPolicyConfirmTool(confirmStore),
	}

	for _, tt := range tools {
		tt := tt
		t.Run(tt.Name(), func(t *testing.T) {
			out, err := tt.Execute(context.Background(), map[string]any{})
			if err != nil {
				t.Fatalf("Execute returned unexpected error: %v", err)
			}
			result, ok := out.(map[string]any)
			if !ok {
				t.Fatalf("tool output must be object, got %T", out)
			}
			if _, ok := result["ok"]; !ok {
				t.Fatalf("tool output must contain \"ok\": %#v", result)
			}
			if _, ok := result["data"]; !ok {
				t.Fatalf("tool output must contain \"data\": %#v", result)
			}
			if _, ok := result["error"]; !ok {
				t.Fatalf("tool output must contain \"error\": %#v", result)
			}
		})
	}
}

// TestToolSchemasDocumentContainsCatalog проверяет ожидаемое поведение в тестовом сценарии.
func TestToolSchemasDocumentContainsCatalog(t *testing.T) {
	t.Parallel()

	doc := SchemasDocument()
	toolsRaw, ok := doc["tools"].(map[string]any)
	if !ok {
		t.Fatalf("tools document must include tools object: %#v", doc)
	}
	for _, name := range requiredToolSchemas {
		value, ok := toolsRaw[name]
		if !ok {
			t.Fatalf("tools document missing %q", name)
		}
		entry, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("tools document entry must be object for %q", name)
		}
		if _, ok := entry["inputSchema"]; !ok {
			t.Fatalf("tools document entry missing inputSchema for %q", name)
		}
		if _, ok := entry["outputSchema"]; !ok {
			t.Fatalf("tools document entry missing outputSchema for %q", name)
		}
	}
}

// assertJSONShapeEqual выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func assertJSONShapeEqual(t *testing.T, expected, got any, label string) {
	t.Helper()
	expectedNorm := normalizeJSONValue(t, expected)
	gotNorm := normalizeJSONValue(t, got)
	if !reflect.DeepEqual(expectedNorm, gotNorm) {
		t.Fatalf("%s mismatch\nexpected: %#v\ngot:      %#v", label, expectedNorm, gotNorm)
	}
}

// normalizeJSONValue нормализует входные данные к канонической форме, используемой в модуле.
func normalizeJSONValue(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json value: %v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal json value: %v", err)
	}
	return out
}

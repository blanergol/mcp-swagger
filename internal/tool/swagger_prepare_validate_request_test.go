package tool

import (
	"context"
	"testing"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// TestPrepareRequestReturnsEmptyValidationErrorsArray verifies that validation.errors is always an array.
func TestPrepareRequestReturnsEmptyValidationErrorsArray(t *testing.T) {
	t.Parallel()

	store := &executeTestStore{
		endpoint: swagger.ResolvedOperation{
			Method:       "GET",
			BaseURL:      "https://api.example.com",
			PathTemplate: "/inventory",
			URLTemplate:  "https://api.example.com/inventory",
			OperationID:  "getInventory",
			Responses: swagger.ResponseGroups{
				Success: []swagger.Response{{Status: 200}},
			},
		},
	}
	tool := NewSwaggerPrepareRequestTool(store, 1<<20, "test-agent")

	out, err := tool.Execute(context.Background(), map[string]any{
		"operationId": "getInventory",
	})
	if err != nil {
		t.Fatalf("prepare_request returned unexpected error: %v", err)
	}

	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output type: %T", out)
	}
	if okValue, _ := result["ok"].(bool); !okValue {
		t.Fatalf("tool returned not-ok: %#v", result)
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("result.data must be object, got %T", result["data"])
	}
	validation, ok := data["validation"].(map[string]any)
	if !ok {
		t.Fatalf("result.data.validation must be object, got %T", data["validation"])
	}
	assertEmptyArrayValue(t, validation["errors"], "validation.errors")
}

// TestValidateRequestReturnsEmptyErrorsArray verifies that errors is always an array.
func TestValidateRequestReturnsEmptyErrorsArray(t *testing.T) {
	t.Parallel()

	store := &executeTestStore{
		endpoint: swagger.ResolvedOperation{
			Method:       "GET",
			BaseURL:      "https://api.example.com",
			PathTemplate: "/inventory",
			URLTemplate:  "https://api.example.com/inventory",
			OperationID:  "getInventory",
			Responses: swagger.ResponseGroups{
				Success: []swagger.Response{{Status: 200}},
			},
		},
	}
	tool := NewSwaggerValidateRequestTool(store)

	out, err := tool.Execute(context.Background(), map[string]any{
		"operationId": "getInventory",
	})
	if err != nil {
		t.Fatalf("validate_request returned unexpected error: %v", err)
	}

	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output type: %T", out)
	}
	if okValue, _ := result["ok"].(bool); !okValue {
		t.Fatalf("tool returned not-ok: %#v", result)
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("result.data must be object, got %T", result["data"])
	}
	assertEmptyArrayValue(t, data["errors"], "errors")
}

// assertEmptyArrayValue validates that the value is a non-nil empty array.
func assertEmptyArrayValue(t *testing.T, value any, field string) {
	t.Helper()

	switch v := value.(type) {
	case []string:
		if v == nil {
			t.Fatalf("%s must be an array, got nil []string", field)
		}
		if len(v) != 0 {
			t.Fatalf("%s must be empty array, got %d items", field, len(v))
		}
	case []any:
		if v == nil {
			t.Fatalf("%s must be an array, got nil []any", field)
		}
		if len(v) != 0 {
			t.Fatalf("%s must be empty array, got %d items", field, len(v))
		}
	default:
		t.Fatalf("%s must be an array, got %T", field, value)
	}
}

package tool

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// TestValidateResponseBodyEncoding проверяет ожидаемое поведение в тестовом сценарии.
func TestValidateResponseBodyEncoding(t *testing.T) {
	t.Parallel()

	store := &executeTestStore{
		endpoint: swagger.ResolvedOperation{
			Method:       "GET",
			PathTemplate: "/payload",
			OperationID:  "getPayload",
			Responses: swagger.ResponseGroups{
				Success: []swagger.Response{{Status: 200}},
			},
		},
	}
	tool := NewSwaggerValidateResponseTool(store)

	tests := []struct {
		name             string
		input            map[string]any
		expectedType     string
		expectedEncoding string
		assertBody       func(t *testing.T, body any)
	}{
		{
			name: "json",
			input: map[string]any{
				"operationId": "getPayload",
				"params": map[string]any{
					"query":   map[string]any{"status": 200},
					"headers": map[string]any{"Content-Type": "application/json"},
					"body":    map[string]any{"id": "123"},
				},
			},
			expectedType:     "application/json",
			expectedEncoding: "json",
			assertBody: func(t *testing.T, body any) {
				t.Helper()
				m, ok := body.(map[string]any)
				if !ok {
					t.Fatalf("json body must be map, got %T", body)
				}
				if m["id"] != "123" {
					t.Fatalf("unexpected json body id: %#v", m["id"])
				}
			},
		},
		{
			name: "text",
			input: map[string]any{
				"operationId": "getPayload",
				"params": map[string]any{
					"query":   map[string]any{"status": 200},
					"headers": map[string]any{"Content-Type": "text/plain; charset=utf-8"},
					"body":    "hello",
				},
			},
			expectedType:     "text/plain; charset=utf-8",
			expectedEncoding: "text",
			assertBody: func(t *testing.T, body any) {
				t.Helper()
				s, ok := body.(string)
				if !ok {
					t.Fatalf("text body must be string, got %T", body)
				}
				if s != "hello" {
					t.Fatalf("unexpected text body: %q", s)
				}
			},
		},
		{
			name: "binary",
			input: map[string]any{
				"operationId":  "getPayload",
				"contentType":  "application/octet-stream",
				"bodyEncoding": "base64",
				"params": map[string]any{
					"query": map[string]any{"status": 200},
					"body":  base64.StdEncoding.EncodeToString([]byte{0xde, 0xad, 0xbe, 0xef}),
				},
			},
			expectedType:     "application/octet-stream",
			expectedEncoding: "base64",
			assertBody: func(t *testing.T, body any) {
				t.Helper()
				s, ok := body.(string)
				if !ok {
					t.Fatalf("binary body must be base64 string, got %T", body)
				}
				if s != base64.StdEncoding.EncodeToString([]byte{0xde, 0xad, 0xbe, 0xef}) {
					t.Fatalf("unexpected base64 body: %q", s)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			out, err := tool.Execute(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("validate_response returned unexpected error: %v", err)
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
				t.Fatalf("result.data must be object")
			}
			if gotType, _ := data["contentType"].(string); gotType != tc.expectedType {
				t.Fatalf("unexpected contentType: %q", gotType)
			}
			if gotEncoding, _ := data["bodyEncoding"].(string); gotEncoding != tc.expectedEncoding {
				t.Fatalf("unexpected bodyEncoding: %q", gotEncoding)
			}
			tc.assertBody(t, data["body"])

			if valid, _ := data["valid"].(bool); !valid {
				t.Fatalf("expected valid=true, got data=%#v", data)
			}
			assertEmptyArrayValue(t, data["errors"], "errors")
		})
	}
}

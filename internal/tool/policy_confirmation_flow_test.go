package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blanergol/mcp-swagger/internal/audit"
	"github.com/blanergol/mcp-swagger/internal/confirmation"
	"github.com/blanergol/mcp-swagger/internal/policy"
	"github.com/blanergol/mcp-swagger/internal/swagger"
	"github.com/blanergol/mcp-swagger/internal/upstreamauth"
)

// TestExecuteRequiresConfirmationWhenMissing проверяет ожидаемое поведение в тестовом сценарии.
func TestExecuteRequiresConfirmationWhenMissing(t *testing.T) {
	t.Parallel()

	var upstreamCalls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	store := &executeTestStore{
		endpoint: swagger.ResolvedOperation{
			Method:       "POST",
			BaseURL:      upstream.URL,
			PathTemplate: "/users",
			URLTemplate:  upstream.URL + "/users",
			OperationID:  "createUser",
			Responses: swagger.ResponseGroups{
				Success: []swagger.Response{{Status: http.StatusOK}},
			},
		},
	}
	confirmationStore := confirmation.NewMemoryStore(10 * time.Minute)
	executeTool := NewSwaggerExecuteTool(SwaggerExecuteDependencies{
		Store: store,
		Policy: policy.NewEvaluator(policy.Config{
			Mode:                        policy.ModeExecuteWrite,
			AllowedMethods:              []string{"POST"},
			RequireConfirmationForWrite: true,
		}),
		AuthProvider:  upstreamauth.NewNoopProvider(),
		HTTPDoer:      upstream.Client(),
		Auditor:       audit.NewLogger(false, nil, nil),
		Confirmations: confirmationStore,
		Options: SwaggerExecuteOptions{
			Mode:             policy.ModeExecuteWrite,
			ValidateRequest:  false,
			ValidateResponse: false,
			MaxRequestBytes:  1 << 20,
			MaxResponseBytes: 1 << 20,
			UserAgent:        "test-agent",
		},
	})

	out, err := executeTool.Execute(context.Background(), map[string]any{
		"operationId": "createUser",
		"params":      map[string]any{},
	})
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}

	result := mustResultMap(t, out)
	if okValue, _ := result["ok"].(bool); okValue {
		t.Fatalf("execute must require confirmation before write")
	}
	if code := errorCode(result["error"]); code != "confirmation_required" {
		t.Fatalf("expected confirmation_required, got %q", code)
	}
	if calls := atomic.LoadInt32(&upstreamCalls); calls != 0 {
		t.Fatalf("upstream must not be called before confirmation, calls=%d", calls)
	}
}

// TestRequestConfirmExecuteFlow проверяет ожидаемое поведение в тестовом сценарии.
func TestRequestConfirmExecuteFlow(t *testing.T) {
	t.Parallel()

	var upstreamCalls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"u-1"}`))
	}))
	defer upstream.Close()

	store := &executeTestStore{
		endpoint: swagger.ResolvedOperation{
			Method:       "POST",
			BaseURL:      upstream.URL,
			PathTemplate: "/users",
			URLTemplate:  upstream.URL + "/users",
			OperationID:  "createUser",
			Responses: swagger.ResponseGroups{
				Success: []swagger.Response{{Status: http.StatusOK}},
			},
		},
	}
	confirmationStore := confirmation.NewMemoryStore(10 * time.Minute)

	requestTool := NewPolicyRequestConfirmationTool(confirmationStore)
	confirmTool := NewPolicyConfirmTool(confirmationStore)
	executeTool := NewSwaggerExecuteTool(SwaggerExecuteDependencies{
		Store: store,
		Policy: policy.NewEvaluator(policy.Config{
			Mode:                        policy.ModeExecuteWrite,
			AllowedMethods:              []string{"POST"},
			RequireConfirmationForWrite: true,
		}),
		AuthProvider:  upstreamauth.NewNoopProvider(),
		HTTPDoer:      upstream.Client(),
		Auditor:       audit.NewLogger(false, nil, nil),
		Confirmations: confirmationStore,
		Options: SwaggerExecuteOptions{
			Mode:             policy.ModeExecuteWrite,
			ValidateRequest:  false,
			ValidateResponse: false,
			MaxRequestBytes:  1 << 20,
			MaxResponseBytes: 1 << 20,
			UserAgent:        "test-agent",
		},
	})

	requestOut, err := requestTool.Execute(context.Background(), map[string]any{
		"operationId": "createUser",
		"reason":      "write method requires explicit confirmation",
		"preparedRequestSummary": map[string]any{
			"operationId": "createUser",
			"method":      "POST",
			"finalURL":    upstream.URL + "/users",
		},
	})
	if err != nil {
		t.Fatalf("request_confirmation error: %v", err)
	}
	requestData := mustDataMap(t, requestOut)
	confirmationID, _ := requestData["confirmationId"].(string)
	if confirmationID == "" {
		t.Fatalf("expected confirmationId")
	}

	confirmOut, err := confirmTool.Execute(context.Background(), map[string]any{
		"confirmationId": confirmationID,
		"approve":        true,
	})
	if err != nil {
		t.Fatalf("confirm error: %v", err)
	}
	confirmData := mustDataMap(t, confirmOut)
	if approved, _ := confirmData["approved"].(bool); !approved {
		t.Fatalf("expected approved=true")
	}

	executeOut, err := executeTool.Execute(context.Background(), map[string]any{
		"operationId":    "createUser",
		"confirmationId": confirmationID,
		"params": map[string]any{
			"body": map[string]any{"email": "alice@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	executeResult := mustResultMap(t, executeOut)
	if okValue, _ := executeResult["ok"].(bool); !okValue {
		t.Fatalf("execute should pass after confirmation: %#v", executeResult)
	}
	if calls := atomic.LoadInt32(&upstreamCalls); calls != 1 {
		t.Fatalf("expected single upstream call, got %d", calls)
	}
}

// mustResultMap выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func mustResultMap(t *testing.T, out any) map[string]any {
	t.Helper()
	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected output type %T", out)
	}
	return result
}

// mustDataMap выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func mustDataMap(t *testing.T, out any) map[string]any {
	t.Helper()
	result := mustResultMap(t, out)
	if okValue, _ := result["ok"].(bool); !okValue {
		t.Fatalf("expected ok result, got %#v", result)
	}
	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("result.data must be object, got %T", result["data"])
	}
	return data
}

// errorCode выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func errorCode(value any) string {
	switch v := value.(type) {
	case toolError:
		return v.Code
	case map[string]any:
		text, _ := v["code"].(string)
		return text
	default:
		return ""
	}
}

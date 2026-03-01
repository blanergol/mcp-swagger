package tool

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blanergol/mcp-swagger/internal/audit"
	"github.com/blanergol/mcp-swagger/internal/correlation"
	"github.com/blanergol/mcp-swagger/internal/policy"
	"github.com/blanergol/mcp-swagger/internal/swagger"
	"github.com/blanergol/mcp-swagger/internal/upstreamauth"
)

// executeTestStore хранит промежуточные данные инструмента между этапами подготовки и валидации.
type executeTestStore struct {
	endpoint swagger.ResolvedOperation
}

// captureDoer хранит промежуточные данные инструмента между этапами подготовки и валидации.
type captureDoer struct {
	lastRequest *http.Request
	response    *http.Response
	err         error
}

// Do выполняет основную операцию с применением защитных ограничений текущего слоя.
func (d *captureDoer) Do(req *http.Request) (*http.Response, error) {
	d.lastRequest = req
	if d.err != nil {
		return nil, d.err
	}
	if d.response != nil {
		return d.response, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}

// captureAuditLogger хранит промежуточные данные инструмента между этапами подготовки и валидации.
type captureAuditLogger struct {
	entries []audit.Entry
}

// LogCall выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (l *captureAuditLogger) LogCall(_ context.Context, entry audit.Entry) error {
	l.entries = append(l.entries, entry)
	return nil
}

// ListEndpoints возвращает коллекцию доступных элементов в детерминированном порядке.
func (s *executeTestStore) ListEndpoints(context.Context) ([]swagger.ResolvedOperation, error) {
	return []swagger.ResolvedOperation{s.endpoint}, nil
}

// ListEndpointsByMethod возвращает коллекцию доступных элементов в детерминированном порядке.
func (s *executeTestStore) ListEndpointsByMethod(_ context.Context, method string) ([]swagger.ResolvedOperation, error) {
	if s.endpoint.Method == method {
		return []swagger.ResolvedOperation{s.endpoint}, nil
	}
	return nil, nil
}

// GetEndpointByOperationID возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func (s *executeTestStore) GetEndpointByOperationID(_ context.Context, opID string) (swagger.ResolvedOperation, error) {
	if s.endpoint.OperationID == opID {
		return s.endpoint, nil
	}
	return swagger.ResolvedOperation{}, swagger.ErrNotFound
}

// GetSchemaByName возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func (s *executeTestStore) GetSchemaByName(context.Context, string) (any, error) {
	return nil, swagger.ErrNotFound
}

// Lookup выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (s *executeTestStore) Lookup(context.Context, string) (any, error) {
	return nil, swagger.ErrNotFound
}

// TestExecuteReturnsValidationMismatchWithoutFailure проверяет ожидаемое поведение в тестовом сценарии.
func TestExecuteReturnsValidationMismatchWithoutFailure(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"alice"}`))
	}))
	defer upstream.Close()

	store := &executeTestStore{
		endpoint: swagger.ResolvedOperation{
			Method:       "GET",
			BaseURL:      upstream.URL,
			PathTemplate: "/users/{id}",
			URLTemplate:  upstream.URL + "/users/{id}",
			OperationID:  "getUserByID",
			PathParams: []swagger.Param{
				{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
			},
			Responses: swagger.ResponseGroups{
				Success: []swagger.Response{
					{
						Status: http.StatusOK,
						BodySchema: map[string]any{
							"type":     "object",
							"required": []string{"id"},
							"properties": map[string]any{
								"id": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
	}

	executeTool := NewSwaggerExecuteTool(SwaggerExecuteDependencies{
		Store: store,
		Policy: policy.NewEvaluator(policy.Config{
			Mode:           policy.ModeExecuteReadonly,
			AllowedMethods: []string{"GET", "HEAD", "OPTIONS"},
		}),
		AuthProvider: upstreamauth.NewNoopProvider(),
		HTTPDoer:     upstream.Client(),
		Auditor:      audit.NewLogger(false, nil, nil),
		Options: SwaggerExecuteOptions{
			Mode:             policy.ModeExecuteReadonly,
			ValidateRequest:  true,
			ValidateResponse: true,
			MaxRequestBytes:  1 << 20,
			MaxResponseBytes: 1 << 20,
			UserAgent:        "test-agent",
		},
	})

	out, err := executeTool.Execute(context.Background(), map[string]any{
		"operationId": "getUserByID",
		"params": map[string]any{
			"path": map[string]any{"id": "123"},
		},
	})
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}

	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected execute output type: %T", out)
	}
	if okValue, _ := result["ok"].(bool); !okValue {
		t.Fatalf("execute should not fail when response validation mismatches: %#v", result)
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("result.data must be an object: %#v", result["data"])
	}

	responseValidation, ok := data["responseValidation"].(map[string]any)
	if !ok {
		t.Fatalf("responseValidation must be present when VALIDATE_RESPONSE=true: %#v", data)
	}
	if valid, _ := responseValidation["valid"].(bool); valid {
		t.Fatalf("responseValidation.valid must be false on schema mismatch: %#v", responseValidation)
	}

	switch errs := responseValidation["errors"].(type) {
	case []string:
		if len(errs) == 0 {
			t.Fatalf("responseValidation.errors must contain mismatch details")
		}
	case []any:
		if len(errs) == 0 {
			t.Fatalf("responseValidation.errors must contain mismatch details")
		}
	default:
		t.Fatalf("responseValidation.errors has unexpected type %T", responseValidation["errors"])
	}

	if errField := result["error"]; errField != nil {
		t.Fatalf("top-level error must be nil on validation mismatch, got: %#v", errField)
	}

	if gotStatus, _ := data["status"].(int); gotStatus != http.StatusOK {
		// значение map может быть float64 после внешнего marshal; внутри этого пути инструмента ожидается int.
		// оставляем строгую проверку, чтобы гарантировать стандартный успешный payload статуса.
		t.Fatalf("unexpected status in result data: %#v", data["status"])
	}
}

// TestExecuteReturnsEmptyResponseValidationErrorsArrayWhenValid verifies empty-array contract for responseValidation.errors.
func TestExecuteReturnsEmptyResponseValidationErrorsArrayWhenValid(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"123"}`))
	}))
	defer upstream.Close()

	store := &executeTestStore{
		endpoint: swagger.ResolvedOperation{
			Method:       "GET",
			BaseURL:      upstream.URL,
			PathTemplate: "/users/{id}",
			URLTemplate:  upstream.URL + "/users/{id}",
			OperationID:  "getUserByID",
			PathParams: []swagger.Param{
				{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
			},
			Responses: swagger.ResponseGroups{
				Success: []swagger.Response{
					{
						Status: http.StatusOK,
						BodySchema: map[string]any{
							"type":     "object",
							"required": []string{"id"},
							"properties": map[string]any{
								"id": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
	}

	executeTool := NewSwaggerExecuteTool(SwaggerExecuteDependencies{
		Store: store,
		Policy: policy.NewEvaluator(policy.Config{
			Mode:           policy.ModeExecuteReadonly,
			AllowedMethods: []string{"GET", "HEAD", "OPTIONS"},
		}),
		AuthProvider: upstreamauth.NewNoopProvider(),
		HTTPDoer:     upstream.Client(),
		Auditor:      audit.NewLogger(false, nil, nil),
		Options: SwaggerExecuteOptions{
			Mode:             policy.ModeExecuteReadonly,
			ValidateRequest:  true,
			ValidateResponse: true,
			MaxRequestBytes:  1 << 20,
			MaxResponseBytes: 1 << 20,
			UserAgent:        "test-agent",
		},
	})

	out, err := executeTool.Execute(context.Background(), map[string]any{
		"operationId": "getUserByID",
		"params": map[string]any{
			"path": map[string]any{"id": "123"},
		},
	})
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}

	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected execute output type: %T", out)
	}
	if okValue, _ := result["ok"].(bool); !okValue {
		t.Fatalf("execute returned not-ok: %#v", result)
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("result.data must be object: %#v", result["data"])
	}
	responseValidation, ok := data["responseValidation"].(map[string]any)
	if !ok {
		t.Fatalf("responseValidation must be object: %#v", data["responseValidation"])
	}
	if valid, _ := responseValidation["valid"].(bool); !valid {
		t.Fatalf("responseValidation.valid must be true: %#v", responseValidation)
	}
	assertEmptyArrayValue(t, responseValidation["errors"], "responseValidation.errors")
}

// TestExecuteResponseBodyEncoding проверяет ожидаемое поведение в тестовом сценарии.
func TestExecuteResponseBodyEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		contentType      string
		payload          []byte
		expectedType     string
		expectedEncoding string
		assertBody       func(t *testing.T, body any)
	}{
		{
			name:             "json",
			contentType:      "application/json",
			payload:          []byte(`{"id":"123","active":true}`),
			expectedType:     "application/json",
			expectedEncoding: "json",
			assertBody: func(t *testing.T, body any) {
				t.Helper()
				m, ok := body.(map[string]any)
				if !ok {
					t.Fatalf("json body must be object, got %T", body)
				}
				if m["id"] != "123" {
					t.Fatalf("unexpected json body id: %#v", m["id"])
				}
			},
		},
		{
			name:             "text",
			contentType:      "text/plain; charset=utf-8",
			payload:          []byte("ok\n"),
			expectedType:     "text/plain; charset=utf-8",
			expectedEncoding: "text",
			assertBody: func(t *testing.T, body any) {
				t.Helper()
				s, ok := body.(string)
				if !ok {
					t.Fatalf("text body must be string, got %T", body)
				}
				if s != "ok\n" {
					t.Fatalf("unexpected text body: %q", s)
				}
			},
		},
		{
			name:             "binary",
			contentType:      "application/octet-stream",
			payload:          []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01},
			expectedType:     "application/octet-stream",
			expectedEncoding: "base64",
			assertBody: func(t *testing.T, body any) {
				t.Helper()
				s, ok := body.(string)
				if !ok {
					t.Fatalf("binary body must be base64 string, got %T", body)
				}
				if s != base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01}) {
					t.Fatalf("unexpected base64 body: %q", s)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(tc.payload)
			}))
			defer upstream.Close()

			store := &executeTestStore{
				endpoint: swagger.ResolvedOperation{
					Method:       "GET",
					BaseURL:      upstream.URL,
					PathTemplate: "/payload",
					URLTemplate:  upstream.URL + "/payload",
					OperationID:  "getPayload",
					Responses: swagger.ResponseGroups{
						Success: []swagger.Response{{Status: http.StatusOK}},
					},
				},
			}

			executeTool := NewSwaggerExecuteTool(SwaggerExecuteDependencies{
				Store: store,
				Policy: policy.NewEvaluator(policy.Config{
					Mode:           policy.ModeExecuteReadonly,
					AllowedMethods: []string{"GET", "HEAD", "OPTIONS"},
				}),
				AuthProvider: upstreamauth.NewNoopProvider(),
				HTTPDoer:     upstream.Client(),
				Auditor:      audit.NewLogger(false, nil, nil),
				Options: SwaggerExecuteOptions{
					Mode:             policy.ModeExecuteReadonly,
					ValidateRequest:  true,
					ValidateResponse: false,
					MaxRequestBytes:  1 << 20,
					MaxResponseBytes: 1 << 20,
					UserAgent:        "test-agent",
				},
			})

			out, err := executeTool.Execute(context.Background(), map[string]any{
				"operationId": "getPayload",
				"params":      map[string]any{},
			})
			if err != nil {
				t.Fatalf("execute returned unexpected error: %v", err)
			}

			result, ok := out.(map[string]any)
			if !ok {
				t.Fatalf("unexpected execute output type: %T", out)
			}
			if okValue, _ := result["ok"].(bool); !okValue {
				t.Fatalf("execute returned not-ok: %#v", result)
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
		})
	}
}

// TestExecuteInjectsCorrelationIDAndAudit проверяет ожидаемое поведение в тестовом сценарии.
func TestExecuteInjectsCorrelationIDAndAudit(t *testing.T) {
	t.Parallel()

	store := &executeTestStore{
		endpoint: swagger.ResolvedOperation{
			Method:       "GET",
			BaseURL:      "https://api.example.com",
			PathTemplate: "/users/{id}",
			URLTemplate:  "https://api.example.com/users/{id}",
			OperationID:  "getUserByID",
			PathParams: []swagger.Param{
				{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
			},
			Responses: swagger.ResponseGroups{
				Success: []swagger.Response{{Status: http.StatusOK}},
			},
		},
	}
	doer := &captureDoer{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		},
	}
	auditor := &captureAuditLogger{}
	executeTool := NewSwaggerExecuteTool(SwaggerExecuteDependencies{
		Store: store,
		Policy: policy.NewEvaluator(policy.Config{
			Mode:           policy.ModeExecuteReadonly,
			AllowedMethods: []string{"GET", "HEAD", "OPTIONS"},
		}),
		AuthProvider: upstreamauth.NewNoopProvider(),
		HTTPDoer:     doer,
		Auditor:      auditor,
		Options: SwaggerExecuteOptions{
			Mode:                policy.ModeExecuteReadonly,
			ValidateRequest:     true,
			ValidateResponse:    false,
			MaxRequestBytes:     1 << 20,
			MaxResponseBytes:    1 << 20,
			UserAgent:           "test-agent",
			CorrelationIDHeader: "X-Correlation-Id",
		},
	})

	ctx := correlation.ContextWithID(context.Background(), "cid-streamable-123")
	out, err := executeTool.Execute(ctx, map[string]any{
		"operationId": "getUserByID",
		"params": map[string]any{
			"path": map[string]any{"id": "123"},
		},
	})
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected execute output type: %T", out)
	}
	if okValue, _ := result["ok"].(bool); !okValue {
		t.Fatalf("execute should return ok=true, got %#v", result)
	}

	if doer.lastRequest == nil {
		t.Fatalf("expected outbound request to be captured")
	}
	if got := doer.lastRequest.Header.Get("X-Correlation-Id"); got != "cid-streamable-123" {
		t.Fatalf("expected propagated correlation id, got %q", got)
	}

	if len(auditor.entries) == 0 {
		t.Fatalf("expected audit entries to be written")
	}
	last := auditor.entries[len(auditor.entries)-1]
	if last.CorrelationID != "cid-streamable-123" {
		t.Fatalf("expected audit correlation id, got %q", last.CorrelationID)
	}
}

// TestExecuteGeneratesCorrelationIDWhenMissing проверяет ожидаемое поведение в тестовом сценарии.
func TestExecuteGeneratesCorrelationIDWhenMissing(t *testing.T) {
	t.Parallel()

	store := &executeTestStore{
		endpoint: swagger.ResolvedOperation{
			Method:       "GET",
			BaseURL:      "https://api.example.com",
			PathTemplate: "/payload",
			URLTemplate:  "https://api.example.com/payload",
			OperationID:  "getPayload",
			Responses: swagger.ResponseGroups{
				Success: []swagger.Response{{Status: http.StatusOK}},
			},
		},
	}
	doer := &captureDoer{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		},
	}
	executeTool := NewSwaggerExecuteTool(SwaggerExecuteDependencies{
		Store: store,
		Policy: policy.NewEvaluator(policy.Config{
			Mode:           policy.ModeExecuteReadonly,
			AllowedMethods: []string{"GET", "HEAD", "OPTIONS"},
		}),
		AuthProvider: upstreamauth.NewNoopProvider(),
		HTTPDoer:     doer,
		Auditor:      audit.NewLogger(false, nil, nil),
		Options: SwaggerExecuteOptions{
			Mode:                policy.ModeExecuteReadonly,
			ValidateRequest:     true,
			ValidateResponse:    false,
			MaxRequestBytes:     1 << 20,
			MaxResponseBytes:    1 << 20,
			UserAgent:           "test-agent",
			CorrelationIDHeader: "X-Correlation-Id",
		},
	})

	out, err := executeTool.Execute(context.Background(), map[string]any{
		"operationId": "getPayload",
	})
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected execute output type: %T", out)
	}
	if okValue, _ := result["ok"].(bool); !okValue {
		t.Fatalf("execute should return ok=true, got %#v", result)
	}
	if doer.lastRequest == nil {
		t.Fatalf("expected outbound request to be captured")
	}
	if got := strings.TrimSpace(doer.lastRequest.Header.Get("X-Correlation-Id")); got == "" {
		t.Fatalf("expected generated correlation id header to be present")
	}
}

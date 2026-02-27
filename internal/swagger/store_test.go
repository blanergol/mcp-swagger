package swagger

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestLoaderAndParser_JSONAndYAML проверяет ожидаемое поведение в тестовом сценарии.
func TestLoaderAndParser_JSONAndYAML(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name   string
		path   string
		format string
	}{
		{name: "yaml", path: filepath.Join("testdata", "openapi.yaml"), format: "yaml"},
		{name: "json", path: filepath.Join("testdata", "openapi.json"), format: "json"},
		{name: "auto-yaml", path: filepath.Join("testdata", "openapi.yaml"), format: "auto"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			loader := NewSourceLoader(tc.path, nil)
			payload, err := loader.Load(ctx)
			if err != nil {
				t.Fatalf("load failed: %v", err)
			}
			if len(payload) == 0 {
				t.Fatalf("payload is empty")
			}

			parser := NewOpenAPIParser(tc.format)
			doc, err := parser.Parse(ctx, payload)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			if doc == nil || doc.Spec == nil {
				t.Fatalf("parsed document is nil")
			}
			if got, want := doc.Spec.OpenAPI, "3.0.3"; got != want {
				t.Fatalf("openapi version mismatch: got %q want %q", got, want)
			}
		})
	}
}

// TestStore_ListEndpoints проверяет ожидаемое поведение в тестовом сценарии.
func TestStore_ListEndpoints(t *testing.T) {
	t.Parallel()
	store := newTestStore(t, filepath.Join("testdata", "openapi.yaml"), "auto")

	endpoints, err := store.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListEndpoints failed: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(endpoints))
	}

	byMethod, err := store.ListEndpointsByMethod(context.Background(), "GET")
	if err != nil {
		t.Fatalf("ListEndpointsByMethod failed: %v", err)
	}
	if len(byMethod) != 1 {
		t.Fatalf("expected 1 GET endpoint, got %d", len(byMethod))
	}
	if byMethod[0].OperationID != "getUser" {
		t.Fatalf("unexpected operationId: %q", byMethod[0].OperationID)
	}
}

// TestStore_GetEndpointByOperationID проверяет ожидаемое поведение в тестовом сценарии.
func TestStore_GetEndpointByOperationID(t *testing.T) {
	t.Parallel()
	store := newTestStore(t, filepath.Join("testdata", "openapi.yaml"), "auto")

	endpoint, err := store.GetEndpointByOperationID(context.Background(), "createUser")
	if err != nil {
		t.Fatalf("GetEndpointByOperationID failed: %v", err)
	}
	if endpoint.Method != "POST" {
		t.Fatalf("expected POST, got %s", endpoint.Method)
	}
	if endpoint.PathTemplate != "/users" {
		t.Fatalf("expected /users pathTemplate, got %s", endpoint.PathTemplate)
	}
	if endpoint.URLTemplate != "https://api.example.com/users" {
		t.Fatalf("unexpected urlTemplate: %s", endpoint.URLTemplate)
	}
	if endpoint.Request.BodySchema == nil {
		t.Fatalf("expected request body schema")
	}
	if containsRef(endpoint.Request.BodySchema) {
		t.Fatalf("request body schema still contains $ref")
	}

	if len(endpoint.Responses.Errors) == 0 {
		t.Fatalf("expected at least one error response")
	}
	if containsRef(endpoint.Responses.Errors[0].BodySchema) {
		t.Fatalf("error response schema still contains $ref")
	}
}

// TestStore_EndpointDTOHasTemplateFields проверяет ожидаемое поведение в тестовом сценарии.
func TestStore_EndpointDTOHasTemplateFields(t *testing.T) {
	t.Parallel()
	store := newTestStore(t, filepath.Join("testdata", "openapi.yaml"), "auto")

	endpoint, err := store.GetEndpointByOperationID(context.Background(), "getUser")
	if err != nil {
		t.Fatalf("GetEndpointByOperationID failed: %v", err)
	}
	if endpoint.PathTemplate != "/users/{id}" {
		t.Fatalf("unexpected pathTemplate: %q", endpoint.PathTemplate)
	}
	if endpoint.BaseURL != "https://api.example.com" {
		t.Fatalf("unexpected baseURL: %q", endpoint.BaseURL)
	}
	if endpoint.URLTemplate != "https://api.example.com/users/{id}" {
		t.Fatalf("unexpected urlTemplate: %q", endpoint.URLTemplate)
	}
}

// TestStore_GetSchemaByName проверяет ожидаемое поведение в тестовом сценарии.
func TestStore_GetSchemaByName(t *testing.T) {
	t.Parallel()
	store := newTestStore(t, filepath.Join("testdata", "openapi.yaml"), "auto")

	schema, err := store.GetSchemaByName(context.Background(), "User")
	if err != nil {
		t.Fatalf("GetSchemaByName failed: %v", err)
	}
	if schema == nil {
		t.Fatalf("schema is nil")
	}
	if containsRef(schema) {
		t.Fatalf("schema still contains $ref")
	}
}

// TestStore_LookupPointer проверяет ожидаемое поведение в тестовом сценарии.
func TestStore_LookupPointer(t *testing.T) {
	t.Parallel()
	store := newTestStore(t, filepath.Join("testdata", "openapi.yaml"), "auto")

	obj, err := store.Lookup(context.Background(), "/paths/~1users~1{id}/get")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	m, ok := obj.(map[string]any)
	if !ok {
		t.Fatalf("lookup result is not object")
	}
	if got := m["operationId"]; got != "getUser" {
		t.Fatalf("unexpected operationId from lookup: %#v", got)
	}
}

// TestStore_RefResolutionInResponse проверяет ожидаемое поведение в тестовом сценарии.
func TestStore_RefResolutionInResponse(t *testing.T) {
	t.Parallel()
	store := newTestStore(t, filepath.Join("testdata", "openapi.yaml"), "auto")

	endpoint, err := store.GetEndpointByOperationID(context.Background(), "getUser")
	if err != nil {
		t.Fatalf("GetEndpointByOperationID failed: %v", err)
	}
	if len(endpoint.Responses.Success) == 0 {
		t.Fatalf("expected success responses")
	}
	body := endpoint.Responses.Success[0].BodySchema
	if body == nil {
		t.Fatalf("success body schema is nil")
	}
	if containsRef(body) {
		t.Fatalf("success body schema still contains $ref")
	}
}

// newTestStore инициализирует внутреннюю реализацию с безопасными значениями по умолчанию.
func newTestStore(t *testing.T, source, format string) *CachedStore {
	t.Helper()
	loader := NewSourceLoader(source, nil)
	parser := NewOpenAPIParser(format)
	resolver := NewOpenAPIResolver("")
	return NewCachedStore(loader, parser, resolver, CachedStoreOptions{
		Reload:   false,
		CacheTTL: time.Minute,
	})
}

// containsRef выполняет проверку соответствия по правилам текущего модуля.
func containsRef(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		if _, ok := v["$ref"]; ok {
			return true
		}
		for _, item := range v {
			if containsRef(item) {
				return true
			}
		}
		return false
	case []any:
		for _, item := range v {
			if containsRef(item) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

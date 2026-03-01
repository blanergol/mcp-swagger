package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blanergol/mcp-swagger/internal/prompt"
	resource "github.com/blanergol/mcp-swagger/internal/resouce"
	"github.com/blanergol/mcp-swagger/internal/swagger"
	"github.com/blanergol/mcp-swagger/internal/tool"
)

type testTool struct {
	name        string
	description string
	exec        func(ctx context.Context, input any) (any, error)
}

func (t *testTool) Name() string                                        { return t.name }
func (t *testTool) Description() string                                 { return t.description }
func (t *testTool) InputSchema() any                                    { return map[string]any{"type": "object"} }
func (t *testTool) OutputSchema() any                                   { return map[string]any{"type": "object"} }
func (t *testTool) Execute(ctx context.Context, input any) (any, error) { return t.exec(ctx, input) }

type testRegistry struct{ tools map[string]tool.Tool }

func (r *testRegistry) List() []tool.Tool {
	out := make([]tool.Tool, 0, len(r.tools))
	for _, item := range r.tools {
		out = append(out, item)
	}
	return out
}
func (r *testRegistry) Get(name string) (tool.Tool, bool) {
	item, ok := r.tools[name]
	return item, ok
}

type testPromptStore struct {
	template string
	err      error
}

func (s *testPromptStore) Get(string) (string, error) { return s.template, s.err }
func (s *testPromptStore) List() []prompt.Descriptor  { return nil }

type testResourceStore struct {
	list []resource.Descriptor
	item resource.Item
	err  error
}

func (s *testResourceStore) List(context.Context) ([]resource.Descriptor, error) {
	return s.list, s.err
}
func (s *testResourceStore) Get(context.Context, string) (resource.Item, error) { return s.item, s.err }

type noopSwaggerStore struct{}

func (noopSwaggerStore) ListEndpoints(context.Context) ([]swagger.ResolvedOperation, error) {
	return nil, nil
}
func (noopSwaggerStore) ListEndpointsByMethod(context.Context, string) ([]swagger.ResolvedOperation, error) {
	return nil, nil
}
func (noopSwaggerStore) GetEndpointByOperationID(context.Context, string) (swagger.ResolvedOperation, error) {
	return swagger.ResolvedOperation{}, nil
}
func (noopSwaggerStore) GetSchemaByName(context.Context, string) (any, error) { return nil, nil }
func (noopSwaggerStore) Lookup(context.Context, string) (any, error)          { return nil, nil }

// TestServiceCallToolNormalizesNilInput guards contract that nil input is converted to empty object.
func TestServiceCallToolNormalizesNilInput(t *testing.T) {
	t.Parallel()

	var captured any
	registry := &testRegistry{tools: map[string]tool.Tool{
		"echo": &testTool{name: "echo", description: "echo", exec: func(_ context.Context, input any) (any, error) {
			captured = input
			return map[string]any{"ok": true}, nil
		}},
	}}

	svc := NewService(registry, &testPromptStore{}, &testResourceStore{}, noopSwaggerStore{})
	_, err := svc.CallTool(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("call tool returned unexpected error: %v", err)
	}

	inputMap, ok := captured.(map[string]any)
	if !ok {
		t.Fatalf("expected map input for nil normalization, got %T", captured)
	}
	if len(inputMap) != 0 {
		t.Fatalf("expected empty normalized input, got %#v", inputMap)
	}
}

// TestServiceCallToolUnknownGuards verifies unknown tools fail with explicit error.
func TestServiceCallToolUnknownGuards(t *testing.T) {
	t.Parallel()

	svc := NewService(&testRegistry{tools: map[string]tool.Tool{}}, &testPromptStore{}, &testResourceStore{}, noopSwaggerStore{})
	_, err := svc.CallTool(context.Background(), "missing", map[string]any{})
	if err == nil {
		t.Fatalf("expected error for missing tool")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("unexpected error for missing tool: %v", err)
	}
}

// TestServiceGetPromptTemplateHandling protects template rendering and parse-error wrapping.
func TestServiceGetPromptTemplateHandling(t *testing.T) {
	t.Parallel()

	t.Run("renders with missing key as zero", func(t *testing.T) {
		svc := NewService(&testRegistry{}, &testPromptStore{template: "hello {{.name}}/{{.missing}}"}, &testResourceStore{}, noopSwaggerStore{})
		got, err := svc.GetPrompt(context.Background(), "welcome", map[string]string{"name": "neo"})
		if err != nil {
			t.Fatalf("get prompt returned unexpected error: %v", err)
		}
		if got != "hello neo/" {
			t.Fatalf("unexpected rendered template: %q", got)
		}
	})

	t.Run("parse errors are wrapped", func(t *testing.T) {
		svc := NewService(&testRegistry{}, &testPromptStore{template: "{{"}, &testResourceStore{}, noopSwaggerStore{})
		_, err := svc.GetPrompt(context.Background(), "welcome", nil)
		if err == nil {
			t.Fatalf("expected template parse error")
		}
		if !strings.Contains(err.Error(), "parse prompt template") {
			t.Fatalf("expected parse error wrapper, got %v", err)
		}
	})

	t.Run("prompt store errors pass through", func(t *testing.T) {
		expected := errors.New("prompt backend unavailable")
		svc := NewService(&testRegistry{}, &testPromptStore{err: expected}, &testResourceStore{}, noopSwaggerStore{})
		_, err := svc.GetPrompt(context.Background(), "welcome", nil)
		if !errors.Is(err, expected) {
			t.Fatalf("expected backend error passthrough, got %v", err)
		}
	})
}

// TestServiceListContracts protects tool/resource listing and forwarding behavior.
func TestServiceListContracts(t *testing.T) {
	t.Parallel()

	registry := &testRegistry{tools: map[string]tool.Tool{
		"health": &testTool{name: "health", description: "returns health", exec: func(context.Context, any) (any, error) {
			return map[string]any{"status": "ok"}, nil
		}},
	}}
	resourceStore := &testResourceStore{
		list: []resource.Descriptor{{ID: "r1", URI: "resource://r1"}},
		item: resource.Item{Descriptor: resource.Descriptor{ID: "r1"}, Text: "payload"},
	}

	svc := NewService(registry, &testPromptStore{}, resourceStore, noopSwaggerStore{})

	toolsOut, err := svc.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools returned unexpected error: %v", err)
	}
	if len(toolsOut) != 1 || toolsOut[0].Name != "health" {
		t.Fatalf("unexpected tools descriptor output: %#v", toolsOut)
	}

	resourcesOut, err := svc.ListResources(context.Background())
	if err != nil {
		t.Fatalf("list resources returned unexpected error: %v", err)
	}
	if len(resourcesOut) != 1 || resourcesOut[0].ID != "r1" {
		t.Fatalf("unexpected resources output: %#v", resourcesOut)
	}

	itemOut, err := svc.GetResource(context.Background(), "r1")
	if err != nil {
		t.Fatalf("get resource returned unexpected error: %v", err)
	}
	if itemOut.Text != "payload" {
		t.Fatalf("unexpected resource payload: %#v", itemOut)
	}
}

package resource

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/blanergol/mcp-swagger/internal/tool"
)

// TestDocsStoreListAndGet проверяет ожидаемое поведение в тестовом сценарии.
func TestDocsStoreListAndGet(t *testing.T) {
	t.Parallel()

	store := NewDocsStore()
	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list docs resources: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("unexpected docs resource count: got=%d want=1", len(list))
	}
	if list[0].ID != "docs:tool-schemas" {
		t.Fatalf("unexpected docs resource id: %q", list[0].ID)
	}

	item, err := store.Get(context.Background(), "docs:tool-schemas")
	if err != nil {
		t.Fatalf("get docs resource by id: %v", err)
	}
	assertToolSchemaDocument(t, item.Text)

	itemByURI, err := store.Get(context.Background(), "docs://tool-schemas")
	if err != nil {
		t.Fatalf("get docs resource by uri: %v", err)
	}
	assertToolSchemaDocument(t, itemByURI.Text)
}

// assertToolSchemaDocument выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func assertToolSchemaDocument(t *testing.T, payload string) {
	t.Helper()

	doc := map[string]any{}
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		t.Fatalf("decode docs payload: %v", err)
	}
	toolsRaw, ok := doc["tools"].(map[string]any)
	if !ok {
		t.Fatalf("docs payload missing tools map: %#v", doc)
	}

	required := []string{
		tool.ToolSwaggerSearch,
		tool.ToolSwaggerPlanCall,
		tool.ToolSwaggerHTTPGeneratePayload,
		tool.ToolSwaggerHTTPPrepareRequest,
		tool.ToolSwaggerHTTPValidateReq,
		tool.ToolSwaggerHTTPExecute,
		tool.ToolSwaggerHTTPValidateResp,
		tool.ToolPolicyRequestConfirmation,
		tool.ToolPolicyConfirm,
	}
	for _, name := range required {
		entryRaw, ok := toolsRaw[name]
		if !ok {
			t.Fatalf("docs payload missing schema for %q", name)
		}
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			t.Fatalf("schema entry for %q must be object", name)
		}
		if _, ok := entry["inputSchema"]; !ok {
			t.Fatalf("schema entry for %q missing inputSchema", name)
		}
		if _, ok := entry["outputSchema"]; !ok {
			t.Fatalf("schema entry for %q missing outputSchema", name)
		}
	}
}

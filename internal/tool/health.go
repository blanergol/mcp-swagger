package tool

import "context"

// HealthTool возвращает status и app version.
type HealthTool struct {
	version string
}

// NewHealthTool создает health tool.
func NewHealthTool(version string) *HealthTool {
	return &HealthTool{version: version}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *HealthTool) Name() string {
	return "health"
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *HealthTool) Description() string {
	return "Returns service health status and version"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *HealthTool) InputSchema() any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *HealthTool) OutputSchema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status":  map[string]any{"type": "string"},
			"version": map[string]any{"type": "string"},
		},
		"required":             []string{"status", "version"},
		"additionalProperties": true,
	}
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *HealthTool) Execute(_ context.Context, _ any) (any, error) {
	return map[string]any{
		"status":  "ok",
		"version": t.version,
	}, nil
}

package tool

import (
	"context"
	"strings"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// SwaggerPlanCallTool формирует детерминированный план безопасного вызова операции.
type SwaggerPlanCallTool struct {
	store swagger.Store
}

// NewSwaggerPlanCallTool создает swagger.plan_call tool.
func NewSwaggerPlanCallTool(store swagger.Store) *SwaggerPlanCallTool {
	return &SwaggerPlanCallTool{store: store}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *SwaggerPlanCallTool) Name() string {
	return "swagger.plan_call"
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *SwaggerPlanCallTool) Description() string {
	return "Builds a step-by-step tool plan to call a swagger operation safely"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *SwaggerPlanCallTool) InputSchema() any {
	return toolInputSchema(t.Name())
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *SwaggerPlanCallTool) OutputSchema() any {
	return toolOutputSchema(t.Name())
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *SwaggerPlanCallTool) Execute(ctx context.Context, input any) (any, error) {
	if t.store == nil {
		return errorResult("invalid_request", "swagger store is not configured", nil), nil
	}

	parsed, err := parsePlanCallInput(input)
	if err != nil {
		return errorResult("invalid_request", err.Error(), nil), nil
	}

	var endpoint swagger.ResolvedOperation
	if parsed.OperationID != "" {
		endpoint, err = t.store.GetEndpointByOperationID(ctx, parsed.OperationID)
		if err != nil {
			return errorResult("invalid_request", "operationId is not found in swagger", map[string]any{"operationId": parsed.OperationID}), nil
		}
	} else {
		endpoints, listErr := t.store.ListEndpoints(ctx)
		if listErr != nil {
			return errorResult("invalid_request", "failed to load swagger endpoints", map[string]any{"error": listErr.Error()}), nil
		}
		needle := strings.ToLower(strings.TrimSpace(parsed.Query + " " + parsed.Goal))
		for _, candidate := range endpoints {
			if needle == "" || matchesQuery(candidate, needle) {
				endpoint = candidate
				break
			}
		}
		if strings.TrimSpace(endpoint.OperationID) == "" {
			return errorResult("invalid_request", "unable to select an operation, provide operationId or a better query", nil), nil
		}
	}

	steps := []map[string]any{
		{
			"step": 1,
			"tool": "swagger.http.prepare_request",
			"why":  "Resolve URL/path params/query/body into a concrete HTTP request",
		},
		{
			"step": 2,
			"tool": "swagger.http.validate_request",
			"why":  "Validate request params/body against swagger contract before execution",
		},
		{
			"step": 3,
			"tool": "swagger.http.execute",
			"why":  "Run the real upstream HTTP call through MCP policy/auth/limits",
		},
		{
			"step": 4,
			"tool": "swagger.http.validate_response",
			"why":  "Compare actual response with swagger response schema",
		},
	}

	return okResult(map[string]any{
		"operation": map[string]any{
			"operationId":  endpoint.OperationID,
			"method":       endpoint.Method,
			"pathTemplate": endpoint.PathTemplate,
			"urlTemplate":  endpoint.URLTemplate,
			"baseURL":      endpoint.BaseURL,
			"summary":      endpoint.Summary,
		},
		"context": map[string]any{
			"operationId": parsed.OperationID,
			"query":       parsed.Query,
			"goal":        parsed.Goal,
		},
		"steps": steps,
	}), nil
}

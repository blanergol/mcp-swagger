package tool

import (
	"context"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// SwaggerValidateResponseTool валидирует response payload against resolved swagger operation.
type SwaggerValidateResponseTool struct {
	store swagger.Store
}

// NewSwaggerValidateResponseTool создает swagger.http.validate_response tool.
func NewSwaggerValidateResponseTool(store swagger.Store) *SwaggerValidateResponseTool {
	return &SwaggerValidateResponseTool{store: store}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *SwaggerValidateResponseTool) Name() string {
	return "swagger.http.validate_response"
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *SwaggerValidateResponseTool) Description() string {
	return "Strictly validates status/body against swagger operation response schema (no HTTP execution)"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *SwaggerValidateResponseTool) InputSchema() any {
	return toolInputSchema(t.Name())
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *SwaggerValidateResponseTool) OutputSchema() any {
	return toolOutputSchema(t.Name())
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *SwaggerValidateResponseTool) Execute(ctx context.Context, input any) (any, error) {
	if t.store == nil {
		return errorResult("invalid_request", "swagger store is not configured", nil), nil
	}

	parsed, err := parseValidateResponseInput(input)
	if err != nil {
		return errorResult("invalid_request", err.Error(), nil), nil
	}

	endpoint, err := t.store.GetEndpointByOperationID(ctx, parsed.OperationID)
	if err != nil {
		return errorResult("invalid_request", "operationId is not found in swagger", map[string]any{"operationId": parsed.OperationID}), nil
	}

	normalized := normalizeResponseBody(parsed.ContentType, parsed.BodyEncoding, parsed.Body)
	payload := bodyForValidation(normalized.Body)
	errs := validateResponse(endpoint, parsed.Status, payload)

	return okResult(map[string]any{
		"operationId":  parsed.OperationID,
		"status":       parsed.Status,
		"contentType":  normalized.ContentType,
		"bodyEncoding": normalized.BodyEncoding,
		"body":         normalized.Body,
		"valid":        len(errs) == 0,
		"errors":       errs,
	}), nil
}

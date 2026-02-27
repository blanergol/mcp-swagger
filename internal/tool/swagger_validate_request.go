package tool

import (
	"context"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// SwaggerValidateRequestTool валидирует request payload against resolved swagger operation.
type SwaggerValidateRequestTool struct {
	store swagger.Store
}

// NewSwaggerValidateRequestTool создает swagger.http.validate_request tool.
func NewSwaggerValidateRequestTool(store swagger.Store) *SwaggerValidateRequestTool {
	return &SwaggerValidateRequestTool{store: store}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *SwaggerValidateRequestTool) Name() string {
	return "swagger.http.validate_request"
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *SwaggerValidateRequestTool) Description() string {
	return "Validates request shape, params and body against swagger operation schema"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *SwaggerValidateRequestTool) InputSchema() any {
	return toolInputSchema(t.Name())
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *SwaggerValidateRequestTool) OutputSchema() any {
	return toolOutputSchema(t.Name())
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *SwaggerValidateRequestTool) Execute(ctx context.Context, input any) (any, error) {
	if t.store == nil {
		return errorResult("invalid_request", "swagger store is not configured", nil), nil
	}

	reqInput, err := parseExecuteInput(input)
	if err != nil {
		return errorResult("invalid_request", err.Error(), nil), nil
	}

	endpoint, prepared, err := prepareRequestForExecution(ctx, t.store, reqInput.OperationID, reqInput, prepareOptions{
		BaseURL:      reqInput.BaseURL,
		AllowNoBase:  true,
		MaxBodyBytes: 0,
	})
	if err != nil {
		if err.Error() == "no base URL" {
			return errorResult("no_base_url", err.Error(), map[string]any{"operationId": reqInput.OperationID}), nil
		}
		return errorResult("invalid_request", err.Error(), nil), nil
	}

	errs := validatePreparedRequest(endpoint, prepared)
	return okResult(map[string]any{
		"operationId": reqInput.OperationID,
		"valid":       len(errs) == 0,
		"errors":      errs,
	}), nil
}

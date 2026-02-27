package tool

import (
	"context"
	"fmt"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// SwaggerPrepareRequestTool prepares HTTP request data для a swagger operation.
type SwaggerPrepareRequestTool struct {
	store          swagger.Store
	maxRequestSize int64
	userAgent      string
}

// NewSwaggerPrepareRequestTool создает swagger.http.prepare_request tool.
func NewSwaggerPrepareRequestTool(store swagger.Store, maxRequestSize int64, userAgent string) *SwaggerPrepareRequestTool {
	return &SwaggerPrepareRequestTool{
		store:          store,
		maxRequestSize: maxRequestSize,
		userAgent:      userAgent,
	}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *SwaggerPrepareRequestTool) Name() string {
	return "swagger.http.prepare_request"
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *SwaggerPrepareRequestTool) Description() string {
	return "Builds a concrete HTTP request from swagger operationId and unified params"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *SwaggerPrepareRequestTool) InputSchema() any {
	return toolInputSchema(t.Name())
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *SwaggerPrepareRequestTool) OutputSchema() any {
	return toolOutputSchema(t.Name())
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *SwaggerPrepareRequestTool) Execute(ctx context.Context, input any) (any, error) {
	if t.store == nil {
		return errorResult("invalid_request", "swagger store is not configured", nil), nil
	}

	reqInput, err := parseExecuteInput(input)
	if err != nil {
		return errorResult("invalid_request", err.Error(), nil), nil
	}

	endpoint, err := t.store.GetEndpointByOperationID(ctx, reqInput.OperationID)
	if err != nil {
		return errorResult("invalid_request", "operationId is not found in swagger", map[string]any{"operationId": reqInput.OperationID}), nil
	}

	prepared, err := prepareEndpointRequest(endpoint, reqInput, prepareOptions{
		BaseURL:       reqInput.BaseURL,
		AllowNoBase:   true,
		MaxBodyBytes:  t.maxRequestSize,
		DefaultAgent:  t.userAgent,
		DefaultAccept: "application/json",
	})
	if err != nil {
		if err.Error() == "no base URL" {
			return errorResult("no_base_url", err.Error(), map[string]any{"operationId": reqInput.OperationID}), nil
		}
		return errorResult("invalid_request", err.Error(), nil), nil
	}

	validationIssues := validatePreparedRequest(endpoint, prepared)

	result := map[string]any{
		"operationId": prepared.OperationID,
		"method":      prepared.Method,
		"path":        prepared.Path,
		"finalURL":    prepared.URL,
		"headers":     prepared.Headers,
		"contentType": prepared.ContentType,
		"body":        prepared.BodyInput,
		"bodyBytes":   len(prepared.BodyBytes),
		"validation": map[string]any{
			"valid":  len(validationIssues) == 0,
			"errors": validationIssues,
		},
	}
	return okResult(result), nil
}

// prepareRequestForExecution выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func prepareRequestForExecution(
	ctx context.Context,
	store swagger.Store,
	opID string,
	input executeInput,
	opts prepareOptions,
) (swagger.ResolvedOperation, preparedRequest, error) {
	endpoint, err := store.GetEndpointByOperationID(ctx, opID)
	if err != nil {
		return swagger.ResolvedOperation{}, preparedRequest{}, fmt.Errorf("operation %q not found", opID)
	}
	prepared, err := prepareEndpointRequest(endpoint, input, opts)
	if err != nil {
		return swagger.ResolvedOperation{}, preparedRequest{}, err
	}
	return endpoint, prepared, nil
}

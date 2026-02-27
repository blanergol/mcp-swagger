package tool

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/blanergol/mcp-swagger/internal/confirmation"
)

// PolicyRequestConfirmationTool создает запрос подтверждения для human-in-the-loop сценария.
type PolicyRequestConfirmationTool struct {
	store confirmation.Store
}

// NewPolicyRequestConfirmationTool создает policy.request_confirmation tool.
func NewPolicyRequestConfirmationTool(store confirmation.Store) *PolicyRequestConfirmationTool {
	return &PolicyRequestConfirmationTool{store: store}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *PolicyRequestConfirmationTool) Name() string {
	return "policy.request_confirmation"
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *PolicyRequestConfirmationTool) Description() string {
	return "Creates a human-in-the-loop confirmation request for a pending API execution"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *PolicyRequestConfirmationTool) InputSchema() any {
	return toolInputSchema(t.Name())
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *PolicyRequestConfirmationTool) OutputSchema() any {
	return toolOutputSchema(t.Name())
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *PolicyRequestConfirmationTool) Execute(ctx context.Context, input any) (any, error) {
	if t.store == nil {
		return errorResult("invalid_request", "confirmation store is not configured", nil), nil
	}

	inMap, err := toAnyMap(input)
	if err != nil {
		return errorResult("invalid_request", "tool input must be an object", nil), nil
	}
	params, _ := toAnyMapOrNil(inMap["params"])

	operationID := strings.TrimSpace(valueAsString(firstNonNil(
		inMap["operationId"],
		inMap["operationID"],
		mapValue(params, "operationId"),
		mapValue(params, "operationID"),
	)))
	if operationID == "" {
		return errorResult("invalid_request", "operationId is required", nil), nil
	}

	summary := firstNonNil(
		inMap["preparedRequestSummary"],
		mapValue(params, "preparedRequestSummary"),
		inMap["summary"],
		mapValue(params, "summary"),
	)
	reason := strings.TrimSpace(valueAsString(firstNonNil(inMap["reason"], mapValue(params, "reason"))))

	method := strings.ToUpper(strings.TrimSpace(valueAsString(firstNonNil(inMap["method"], mapValue(params, "method")))))
	finalURL := strings.TrimSpace(valueAsString(firstNonNil(inMap["finalURL"], inMap["url"], mapValue(params, "finalURL"), mapValue(params, "url"))))
	if summaryMap, ok := summary.(map[string]any); ok {
		if method == "" {
			method = strings.ToUpper(strings.TrimSpace(valueAsString(firstNonNil(summaryMap["method"], summaryMap["httpMethod"]))))
		}
		if finalURL == "" {
			finalURL = strings.TrimSpace(valueAsString(firstNonNil(summaryMap["finalURL"], summaryMap["url"])))
		}
	}

	record, err := t.store.Request(ctx, confirmation.Request{
		OperationID:            operationID,
		Method:                 method,
		FinalURL:               finalURL,
		PreparedRequestSummary: summary,
		Reason:                 reason,
	})
	if err != nil {
		return errorResult("upstream_error", err.Error(), nil), nil
	}

	return okResult(map[string]any{
		"confirmationId": record.ID,
		"expiresAt":      record.ExpiresAt.UTC().Format(time.RFC3339),
		"summary": map[string]any{
			"operationId":            record.OperationID,
			"method":                 record.Method,
			"finalURL":               record.FinalURL,
			"reason":                 record.Reason,
			"preparedRequestSummary": record.PreparedRequestSummary,
		},
	}), nil
}

// PolicyConfirmTool хранит промежуточные данные инструмента между этапами подготовки и валидации.
type PolicyConfirmTool struct {
	store confirmation.Store
}

// NewPolicyConfirmTool создает policy.confirm tool.
func NewPolicyConfirmTool(store confirmation.Store) *PolicyConfirmTool {
	return &PolicyConfirmTool{store: store}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *PolicyConfirmTool) Name() string {
	return "policy.confirm"
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *PolicyConfirmTool) Description() string {
	return "Approves or rejects a pending confirmation request"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *PolicyConfirmTool) InputSchema() any {
	return toolInputSchema(t.Name())
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *PolicyConfirmTool) OutputSchema() any {
	return toolOutputSchema(t.Name())
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *PolicyConfirmTool) Execute(ctx context.Context, input any) (any, error) {
	if t.store == nil {
		return errorResult("invalid_request", "confirmation store is not configured", nil), nil
	}

	inMap, err := toAnyMap(input)
	if err != nil {
		return errorResult("invalid_request", "tool input must be an object", nil), nil
	}
	params, _ := toAnyMapOrNil(inMap["params"])

	confirmationID := strings.TrimSpace(valueAsString(firstNonNil(
		inMap["confirmationId"],
		inMap["confirmationID"],
		mapValue(params, "confirmationId"),
		mapValue(params, "confirmationID"),
	)))
	if confirmationID == "" {
		return errorResult("invalid_request", "confirmationId is required", nil), nil
	}

	approve, err := parseBoolAny(firstNonNil(inMap["approve"], mapValue(params, "approve")))
	if err != nil {
		return errorResult("invalid_request", err.Error(), nil), nil
	}

	record, err := t.store.Confirm(ctx, confirmationID, approve)
	if err != nil {
		code := "invalid_request"
		message := err.Error()
		if errors.Is(err, confirmation.ErrNotFound) {
			message = "confirmationId is not found"
		}
		if errors.Is(err, confirmation.ErrExpired) {
			message = "confirmation request has expired"
		}
		if errors.Is(err, confirmation.ErrConsumed) {
			message = "confirmation request is already consumed"
		}
		return errorResult(code, message, map[string]any{"confirmationId": confirmationID}), nil
	}

	return okResult(map[string]any{
		"confirmationId": record.ID,
		"approved":       record.Approved,
		"expiresAt":      record.ExpiresAt.UTC().Format(time.RFC3339),
	}), nil
}

// parseBoolAny разбирает входные данные и возвращает нормализованное представление.
func parseBoolAny(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y":
			return true, nil
		case "false", "0", "no", "n":
			return false, nil
		default:
			return false, errors.New("approve must be boolean")
		}
	case nil:
		return false, errors.New("approve must be provided")
	default:
		text := strings.ToLower(strings.TrimSpace(valueAsString(v)))
		if text == "true" {
			return true, nil
		}
		if text == "false" {
			return false, nil
		}
		return false, errors.New("approve must be boolean")
	}
}

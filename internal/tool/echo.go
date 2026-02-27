package tool

import (
	"context"
	"errors"
	"fmt"
)

// EchoTool возвращает user input back.
type EchoTool struct{}

// NewEchoTool создает echo tool.
func NewEchoTool() *EchoTool {
	return &EchoTool{}
}

// Name возвращает стабильный идентификатор сущности для регистрации и вызова через MCP.
func (t *EchoTool) Name() string {
	return "echo"
}

// Description возвращает краткое описание назначения сущности для клиента MCP.
func (t *EchoTool) Description() string {
	return "Echoes the provided message"
}

// InputSchema возвращает JSON Schema входа, который публикуется в tools/list.
func (t *EchoTool) InputSchema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "Message to echo",
			},
		},
		"required":             []string{"message"},
		"additionalProperties": false,
	}
}

// OutputSchema возвращает JSON Schema результата для машинной валидации клиентом.
func (t *EchoTool) OutputSchema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{"type": "string"},
		},
		"required":             []string{"message"},
		"additionalProperties": true,
	}
}

// Execute выполняет основной сценарий инструмента и возвращает унифицированный результат ok/data/error.
func (t *EchoTool) Execute(_ context.Context, input any) (any, error) {
	message, err := inputMessage(input)
	if err != nil {
		return nil, err
	}
	return map[string]any{"message": message}, nil
}

// inputMessage выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func inputMessage(input any) (string, error) {
	switch v := input.(type) {
	case map[string]any:
		raw, ok := v["message"]
		if !ok {
			return "", errors.New("message is required")
		}
		msg, ok := raw.(string)
		if !ok {
			return "", errors.New("message must be string")
		}
		if msg == "" {
			return "", errors.New("message cannot be empty")
		}
		return msg, nil
	case map[string]string:
		msg := v["message"]
		if msg == "" {
			return "", errors.New("message is required")
		}
		return msg, nil
	case string:
		if v == "" {
			return "", errors.New("message cannot be empty")
		}
		return v, nil
	default:
		return "", fmt.Errorf("unsupported input type %T", input)
	}
}

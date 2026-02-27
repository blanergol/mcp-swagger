package tool

import "encoding/json"

const (
	// ToolSwaggerSearch фиксирует официальное имя MCP tool, используемое в discovery и call.
	ToolSwaggerSearch = "swagger.search"
	// ToolSwaggerPlanCall фиксирует официальное имя MCP tool, используемое в discovery и call.
	ToolSwaggerPlanCall = "swagger.plan_call"
	// ToolSwaggerHTTPGeneratePayload фиксирует официальное имя MCP tool, используемое в discovery и call.
	ToolSwaggerHTTPGeneratePayload = "swagger.http.generate_payload"
	// ToolSwaggerHTTPPrepareRequest фиксирует официальное имя MCP tool, используемое в discovery и call.
	ToolSwaggerHTTPPrepareRequest = "swagger.http.prepare_request"
	// ToolSwaggerHTTPValidateReq фиксирует строковый маркер протокола/контракта, используемый в нескольких местах.
	ToolSwaggerHTTPValidateReq = "swagger.http.validate_request"
	// ToolSwaggerHTTPExecute фиксирует официальное имя MCP tool, используемое в discovery и call.
	ToolSwaggerHTTPExecute = "swagger.http.execute"
	// ToolSwaggerHTTPValidateResp фиксирует строковый маркер протокола/контракта, используемый в нескольких местах.
	ToolSwaggerHTTPValidateResp = "swagger.http.validate_response"
	// ToolPolicyRequestConfirmation фиксирует официальное имя MCP tool, используемое в discovery и call.
	ToolPolicyRequestConfirmation = "policy.request_confirmation"
	// ToolPolicyConfirm фиксирует официальное имя MCP tool, используемое в discovery и call.
	ToolPolicyConfirm = "policy.confirm"
)

// SchemaBundle хранит JSON Schema для входа и выхода инструмента.
type SchemaBundle struct {
	InputSchema  any `json:"inputSchema"`
	OutputSchema any `json:"outputSchema"`
}

// toolSchemas хранит служебное значение, используемое внутри текущего пакета.
var toolSchemas = map[string]SchemaBundle{
	ToolSwaggerSearch: {
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operationId": map[string]any{"type": "string"},
				"params": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"query":  map[string]any{"type": "string"},
								"q":      map[string]any{"type": "string"},
								"method": map[string]any{"type": "string"},
								"tag":    map[string]any{"type": "string"},
								"include": map[string]any{
									"type": "array",
									"items": map[string]any{
										"type": "string",
										"enum": []string{"endpoints", "schemas", "usage"},
									},
								},
								"schema": map[string]any{"type": "string"},
								"status": map[string]any{"type": "integer", "minimum": 100, "maximum": 599},
								"limit":  map[string]any{"type": "integer", "minimum": 1},
							},
							"additionalProperties": true,
						},
					},
					"additionalProperties": true,
				},
				"query":  map[string]any{"type": "string"},
				"method": map[string]any{"type": "string"},
				"tag":    map[string]any{"type": "string"},
				"include": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
						"enum": []string{"endpoints", "schemas", "usage"},
					},
				},
				"schema": map[string]any{"type": "string"},
				"status": map[string]any{"type": "integer", "minimum": 100, "maximum": 599},
				"limit":  map[string]any{"type": "integer", "minimum": 1},
			},
			"additionalProperties": true,
		},
		OutputSchema: toolResultSchema(map[string]any{
			"type": "object",
			"required": []string{
				"count",
				"filters",
				"results",
			},
			"properties": map[string]any{
				"count": map[string]any{"type": "integer", "minimum": 0},
				"filters": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
				},
				"results": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"operationId":  map[string]any{"type": "string"},
							"method":       map[string]any{"type": "string"},
							"pathTemplate": map[string]any{"type": "string"},
							"urlTemplate":  map[string]any{"type": "string"},
							"baseURL":      map[string]any{"type": "string"},
							"summary":      map[string]any{"type": "string"},
							"tags": map[string]any{
								"type":  "array",
								"items": map[string]any{"type": "string"},
							},
							"matchReason": map[string]any{
								"type":  "array",
								"items": map[string]any{"type": "string"},
							},
							"score": map[string]any{"type": "number"},
						},
						"required": []string{
							"operationId",
							"method",
							"pathTemplate",
							"urlTemplate",
							"baseURL",
							"summary",
							"tags",
							"matchReason",
							"score",
						},
						"additionalProperties": true,
					},
				},
				"schemas": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "object", "additionalProperties": true},
				},
				"usage": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
				},
			},
			"additionalProperties": true,
		}),
	},
	ToolSwaggerPlanCall: {
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operationId": map[string]any{"type": "string"},
				"params": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"query": map[string]any{"type": "string"},
								"q":     map[string]any{"type": "string"},
								"goal":  map[string]any{"type": "string"},
							},
							"additionalProperties": true,
						},
						"body": map[string]any{},
					},
					"additionalProperties": true,
				},
				"query": map[string]any{"type": "string"},
				"goal":  map[string]any{"type": "string"},
			},
			"additionalProperties": true,
		},
		OutputSchema: toolResultSchema(map[string]any{
			"type": "object",
			"required": []string{
				"operation",
				"context",
				"steps",
			},
			"properties": map[string]any{
				"operation": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
				},
				"context": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
				},
				"steps": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"step": map[string]any{"type": "integer"},
							"tool": map[string]any{"type": "string"},
							"why":  map[string]any{"type": "string"},
						},
						"required":             []string{"step", "tool", "why"},
						"additionalProperties": true,
					},
				},
			},
			"additionalProperties": true,
		}),
	},
	ToolSwaggerHTTPPrepareRequest: {
		InputSchema: prepareRequestInputSchema(),
		OutputSchema: toolResultSchema(map[string]any{
			"type": "object",
			"required": []string{
				"operationId",
				"method",
				"path",
				"finalURL",
				"headers",
				"contentType",
				"body",
				"bodyBytes",
				"validation",
			},
			"properties": map[string]any{
				"operationId": map[string]any{"type": "string"},
				"method":      map[string]any{"type": "string"},
				"path":        map[string]any{"type": "string"},
				"finalURL":    map[string]any{"type": "string"},
				"headers": map[string]any{
					"type":                 "object",
					"additionalProperties": map[string]any{"type": "string"},
				},
				"contentType": map[string]any{"type": "string"},
				"body":        map[string]any{},
				"bodyBytes":   map[string]any{"type": "integer", "minimum": 0},
				"validation": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"valid": map[string]any{"type": "boolean"},
						"errors": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required":             []string{"valid", "errors"},
					"additionalProperties": true,
				},
			},
			"additionalProperties": true,
		}),
	},
	ToolSwaggerHTTPGeneratePayload: {
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operationId": map[string]any{"type": "string"},
				"params": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"seed":     map[string]any{"type": "integer"},
								"strategy": map[string]any{"type": "string", "enum": []string{"minimal", "example", "maximal"}},
								"overrides": map[string]any{
									"type":                 "object",
									"additionalProperties": true,
								},
							},
							"additionalProperties": true,
						},
					},
					"additionalProperties": true,
				},
				"seed":      map[string]any{"type": "integer"},
				"strategy":  map[string]any{"type": "string", "enum": []string{"minimal", "example", "maximal"}},
				"overrides": map[string]any{"type": "object", "additionalProperties": true},
			},
			"required":             []string{"operationId"},
			"additionalProperties": true,
		},
		OutputSchema: toolResultSchema(map[string]any{
			"type": "object",
			"required": []string{
				"operationId",
				"strategy",
				"seed",
				"contentTypes",
				"body",
				"warnings",
			},
			"properties": map[string]any{
				"operationId": map[string]any{"type": "string"},
				"strategy":    map[string]any{"type": "string", "enum": []string{"minimal", "example", "maximal"}},
				"seed":        map[string]any{"type": "integer"},
				"contentTypes": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
				"body": map[string]any{},
				"warnings": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
			"additionalProperties": true,
		}),
	},
	ToolSwaggerHTTPValidateReq: {
		InputSchema: prepareRequestInputSchema(),
		OutputSchema: toolResultSchema(map[string]any{
			"type": "object",
			"required": []string{
				"operationId",
				"valid",
				"errors",
			},
			"properties": map[string]any{
				"operationId": map[string]any{"type": "string"},
				"valid":       map[string]any{"type": "boolean"},
				"errors": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
			"additionalProperties": true,
		}),
	},
	ToolSwaggerHTTPExecute: {
		InputSchema: executeInputSchema(),
		OutputSchema: toolResultSchema(map[string]any{
			"type": "object",
			"required": []string{
				"operationId",
				"method",
				"finalURL",
				"status",
				"headers",
				"contentType",
				"bodyEncoding",
				"body",
				"durationMs",
			},
			"properties": map[string]any{
				"operationId": map[string]any{"type": "string"},
				"method":      map[string]any{"type": "string"},
				"finalURL":    map[string]any{"type": "string"},
				"status":      map[string]any{"type": "integer"},
				"headers": map[string]any{
					"type":                 "object",
					"additionalProperties": map[string]any{"type": "string"},
				},
				"contentType": map[string]any{"type": "string"},
				"bodyEncoding": map[string]any{
					"type": "string",
					"enum": []string{"json", "text", "base64"},
				},
				"body":       map[string]any{},
				"durationMs": map[string]any{"type": "integer", "minimum": 0},
				"responseValidation": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"valid": map[string]any{"type": "boolean"},
						"errors": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required":             []string{"valid", "errors"},
					"additionalProperties": true,
				},
			},
			"additionalProperties": true,
		}),
	},
	ToolSwaggerHTTPValidateResp: {
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operationId": map[string]any{"type": "string"},
				"contentType": map[string]any{"type": "string"},
				"bodyEncoding": map[string]any{
					"type": "string",
					"enum": []string{"json", "text", "base64"},
				},
				"params": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query":   map[string]any{"type": "object", "additionalProperties": true},
						"headers": map[string]any{"type": "object", "additionalProperties": true},
						"body":    map[string]any{},
					},
					"required":             []string{"query"},
					"additionalProperties": true,
				},
				"status":  map[string]any{"type": "integer"},
				"headers": map[string]any{"type": "object", "additionalProperties": true},
				"body":    map[string]any{},
			},
			"required":             []string{"operationId"},
			"additionalProperties": true,
		},
		OutputSchema: toolResultSchema(map[string]any{
			"type": "object",
			"required": []string{
				"operationId",
				"status",
				"contentType",
				"bodyEncoding",
				"body",
				"valid",
				"errors",
			},
			"properties": map[string]any{
				"operationId": map[string]any{"type": "string"},
				"status":      map[string]any{"type": "integer"},
				"contentType": map[string]any{"type": "string"},
				"bodyEncoding": map[string]any{
					"type": "string",
					"enum": []string{"json", "text", "base64"},
				},
				"body":  map[string]any{},
				"valid": map[string]any{"type": "boolean"},
				"errors": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
			"additionalProperties": true,
		}),
	},
	ToolPolicyRequestConfirmation: {
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operationId":            map[string]any{"type": "string"},
				"preparedRequestSummary": map[string]any{},
				"reason":                 map[string]any{"type": "string"},
				"params": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
				},
			},
			"required":             []string{"operationId"},
			"additionalProperties": true,
		},
		OutputSchema: toolResultSchema(map[string]any{
			"type": "object",
			"required": []string{
				"confirmationId",
				"expiresAt",
				"summary",
			},
			"properties": map[string]any{
				"confirmationId": map[string]any{"type": "string"},
				"expiresAt":      map[string]any{"type": "string"},
				"summary": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
				},
			},
			"additionalProperties": true,
		}),
	},
	ToolPolicyConfirm: {
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"confirmationId": map[string]any{"type": "string"},
				"approve":        map[string]any{"type": "boolean"},
				"params": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
				},
			},
			"required":             []string{"confirmationId", "approve"},
			"additionalProperties": true,
		},
		OutputSchema: toolResultSchema(map[string]any{
			"type": "object",
			"required": []string{
				"confirmationId",
				"approved",
				"expiresAt",
			},
			"properties": map[string]any{
				"confirmationId": map[string]any{"type": "string"},
				"approved":       map[string]any{"type": "boolean"},
				"expiresAt":      map[string]any{"type": "string"},
			},
			"additionalProperties": true,
		}),
	},
}

// prepareRequestInputSchema выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func prepareRequestInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operationId": map[string]any{"type": "string"},
			"params": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "object", "additionalProperties": true},
					"query":   map[string]any{"type": "object", "additionalProperties": true},
					"headers": map[string]any{"type": "object", "additionalProperties": true},
					"body":    map[string]any{},
				},
				"additionalProperties": true,
			},
			"contentType": map[string]any{"type": "string"},
			"baseURL":     map[string]any{"type": "string"},
			"pathParams":  map[string]any{"type": "object", "additionalProperties": true},
			"queryParams": map[string]any{"type": "object", "additionalProperties": true},
			"headers":     map[string]any{"type": "object", "additionalProperties": true},
			"body":        map[string]any{},
		},
		"required":             []string{"operationId"},
		"additionalProperties": true,
	}
}

// executeInputSchema выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func executeInputSchema() map[string]any {
	schema := prepareRequestInputSchema()
	props, _ := schema["properties"].(map[string]any)
	props["confirmationId"] = map[string]any{"type": "string"}
	props["confirmationToken"] = map[string]any{"type": "string"}
	return schema
}

// toolResultSchema выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func toolResultSchema(dataSchema any) map[string]any {
	if dataSchema == nil {
		dataSchema = map[string]any{}
	}
	return map[string]any{
		"type": "object",
		"required": []string{
			"ok",
			"data",
			"error",
		},
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
			"data": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "null"},
					dataSchema,
				},
			},
			"error": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "null"},
					map[string]any{
						"type": "object",
						"required": []string{
							"code",
							"message",
						},
						"properties": map[string]any{
							"code":    map[string]any{"type": "string"},
							"message": map[string]any{"type": "string"},
							"details": map[string]any{},
						},
						"additionalProperties": true,
					},
				},
			},
		},
		"additionalProperties": false,
	}
}

// toolInputSchema выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func toolInputSchema(name string) any {
	schema, ok := toolSchemas[name]
	if !ok {
		return map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": true,
		}
	}
	return cloneSchema(schema.InputSchema)
}

// toolOutputSchema выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func toolOutputSchema(name string) any {
	schema, ok := toolSchemas[name]
	if !ok {
		return toolResultSchema(map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		})
	}
	return cloneSchema(schema.OutputSchema)
}

// SchemasCatalog возвращает каталог схем всех инструментов.
func SchemasCatalog() map[string]SchemaBundle {
	out := make(map[string]SchemaBundle, len(toolSchemas))
	for name, schemas := range toolSchemas {
		out[name] = SchemaBundle{
			InputSchema:  cloneSchema(schemas.InputSchema),
			OutputSchema: cloneSchema(schemas.OutputSchema),
		}
	}
	return out
}

// SchemasDocument формирует документ ресурса `docs:tool-schemas`.
func SchemasDocument() map[string]any {
	tools := make(map[string]any, len(toolSchemas))
	for name, schemas := range toolSchemas {
		tools[name] = map[string]any{
			"inputSchema":  cloneSchema(schemas.InputSchema),
			"outputSchema": cloneSchema(schemas.OutputSchema),
		}
	}
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"title":   "MCP Swagger Gateway Tool Schemas",
		"type":    "object",
		"properties": map[string]any{
			"tools": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
		"required": []string{"tools"},
		"tools":    tools,
	}
}

// cloneSchema создает независимую копию значения, чтобы избежать побочных эффектов мутаций.
func cloneSchema(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return value
	}
	return out
}

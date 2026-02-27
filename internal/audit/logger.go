package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

// Logger задает контракт записи структурированных аудит-событий.
type Logger interface {
	LogCall(ctx context.Context, entry Entry) error
}

// Entry описывает одно событие вызова upstream API.
type Entry struct {
	Timestamp time.Time `json:"timestamp"`

	Principal string   `json:"principal,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`

	OperationID   string `json:"operationId,omitempty"`
	Method        string `json:"method,omitempty"`
	URL           string `json:"url,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`

	RequestHeaders map[string]string `json:"requestHeaders,omitempty"`
	RequestBody    any               `json:"requestBody,omitempty"`

	ResponseStatus  int               `json:"responseStatus,omitempty"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	ResponseBody    any               `json:"responseBody,omitempty"`

	DurationMs int64  `json:"durationMs,omitempty"`
	Error      string `json:"error,omitempty"`
}

// SlogLogger пишет аудит-события через slog.Logger.
type SlogLogger struct {
	enabled          bool
	redactHeaders    map[string]struct{}
	redactJSONFields map[string]struct{}
}

// NewLogger создает audit logger.
func NewLogger(enabled bool, redactHeaders, redactJSONFields []string) Logger {
	return &SlogLogger{
		enabled:          enabled,
		redactHeaders:    toLowerSet(redactHeaders),
		redactJSONFields: toLowerSet(redactJSONFields),
	}
}

// LogCall записывает одно аудит-событие после редактирования чувствительных данных.
func (l *SlogLogger) LogCall(ctx context.Context, entry Entry) error {
	if l == nil || !l.enabled {
		return nil
	}

	record := map[string]any{
		"timestamp":     entry.Timestamp.UTC().Format(time.RFC3339Nano),
		"principal":     entry.Principal,
		"scopes":        entry.Scopes,
		"operation":     entry.OperationID,
		"method":        entry.Method,
		"url":           sanitizeURL(entry.URL),
		"correlationId": entry.CorrelationID,
		"request": map[string]any{
			"headers": redactHeaders(entry.RequestHeaders, l.redactHeaders),
			"body":    redactJSON(entry.RequestBody, l.redactJSONFields),
		},
		"response": map[string]any{
			"status":  entry.ResponseStatus,
			"headers": redactHeaders(entry.ResponseHeaders, l.redactHeaders),
			"body":    redactJSON(entry.ResponseBody, l.redactJSONFields),
		},
		"durationMs": entry.DurationMs,
		"error":      entry.Error,
	}

	slog.InfoContext(ctx, "audit_upstream_call", "entry", record)
	return nil
}

// sanitizeURL выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func sanitizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.User = nil
	if parsed.RawQuery != "" {
		q := parsed.Query()
		for key := range q {
			lower := strings.ToLower(strings.TrimSpace(key))
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "key") || strings.Contains(lower, "password") {
				q.Set(key, "[REDACTED]")
			}
		}
		parsed.RawQuery = q.Encode()
	}
	return parsed.String()
}

// redactHeaders выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func redactHeaders(headers map[string]string, redacted map[string]struct{}) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		if _, ok := redacted[strings.ToLower(strings.TrimSpace(key))]; ok {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = value
	}
	return out
}

// redactJSON выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func redactJSON(value any, fields map[string]struct{}) any {
	if value == nil {
		return nil
	}
	cloned := cloneAny(value)
	return redactNode(cloned, fields)
}

// redactNode выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func redactNode(value any, fields map[string]struct{}) any {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if _, ok := fields[strings.ToLower(strings.TrimSpace(key))]; ok {
				node[key] = "[REDACTED]"
				continue
			}
			node[key] = redactNode(child, fields)
		}
		return node
	case []any:
		for i := range node {
			node[i] = redactNode(node[i], fields)
		}
		return node
	case string:
		trimmed := strings.TrimSpace(node)
		if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
			var parsed any
			if err := json.Unmarshal([]byte(node), &parsed); err == nil {
				redacted := redactNode(parsed, fields)
				payload, err := json.Marshal(redacted)
				if err == nil {
					return string(payload)
				}
			}
		}
		return node
	default:
		return node
	}
}

// toLowerSet выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func toLowerSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		value := strings.ToLower(strings.TrimSpace(item))
		if value != "" {
			out[value] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cloneAny создает независимую копию значения, чтобы избежать побочных эффектов мутаций.
func cloneAny(value any) any {
	switch node := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(node))
		for key, child := range node {
			out[key] = cloneAny(child)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i := range node {
			out[i] = cloneAny(node[i])
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(node))
		for key, child := range node {
			out[key] = child
		}
		return out
	default:
		return node
	}
}

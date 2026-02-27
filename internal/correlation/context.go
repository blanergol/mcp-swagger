package correlation

import (
	"context"
	"strings"
)

// defaultHeaderName задает значение по умолчанию, используемое при пустой или неполной конфигурации.
const defaultHeaderName = "X-Correlation-Id"

// contextKey используется как приватный ключ context, чтобы избежать коллизий между пакетами.
type contextKey struct{}

// ContextWithID хранит correlation id in context.
func ContextWithID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, id)
}

// IDFromContext извлекает correlation id from context.
func IDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(contextKey{}).(string)
	id = strings.TrimSpace(id)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// HeaderName возвращает configured correlation id header or default.
func HeaderName(configured string) string {
	value := strings.TrimSpace(configured)
	if value == "" {
		return defaultHeaderName
	}
	return value
}

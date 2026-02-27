package swagger

import (
	"context"
	"fmt"
)

// NoopStore возвращает unavailable errors для всех операций.
type NoopStore struct{}

// NewNoopStore создает без действия swagger store.
func NewNoopStore() *NoopStore {
	return &NoopStore{}
}

// ListEndpoints возвращает коллекцию доступных элементов в детерминированном порядке.
func (s *NoopStore) ListEndpoints(context.Context) ([]ResolvedOperation, error) {
	return nil, fmt.Errorf("%w: SWAGGER_PATH is empty", ErrUnavailable)
}

// ListEndpointsByMethod возвращает коллекцию доступных элементов в детерминированном порядке.
func (s *NoopStore) ListEndpointsByMethod(context.Context, string) ([]ResolvedOperation, error) {
	return nil, fmt.Errorf("%w: SWAGGER_PATH is empty", ErrUnavailable)
}

// GetEndpointByOperationID возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func (s *NoopStore) GetEndpointByOperationID(context.Context, string) (ResolvedOperation, error) {
	return ResolvedOperation{}, fmt.Errorf("%w: SWAGGER_PATH is empty", ErrUnavailable)
}

// GetSchemaByName возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func (s *NoopStore) GetSchemaByName(context.Context, string) (any, error) {
	return nil, fmt.Errorf("%w: SWAGGER_PATH is empty", ErrUnavailable)
}

// Lookup выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (s *NoopStore) Lookup(context.Context, string) (any, error) {
	return nil, fmt.Errorf("%w: SWAGGER_PATH is empty", ErrUnavailable)
}

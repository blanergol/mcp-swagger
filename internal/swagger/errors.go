package swagger

import "errors"

var (
	// ErrNotFound означает, что объект не найден в swagger-хранилище.
	ErrNotFound = errors.New("swagger object not found")
	// ErrUnavailable означает, что источник swagger не настроен.
	ErrUnavailable = errors.New("swagger source is not configured")
)

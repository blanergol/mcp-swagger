package server

import "context"

// Transport задает жизненный цикл MCP-сервера на конкретном IO-транспорте.
type Transport interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

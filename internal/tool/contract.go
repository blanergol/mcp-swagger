package tool

import "context"

// Descriptor описывает метаданные инструмента для публикации через MCP.
type Descriptor struct {
	Name         string
	Description  string
	InputSchema  any
	OutputSchema any
}

// Tool задает контракт MCP-инструмента.
type Tool interface {
	Name() string
	Description() string
	InputSchema() any
	OutputSchema() any
	Execute(ctx context.Context, input any) (any, error)
}

// Registry задает контракт реестра инструментов.
type Registry interface {
	List() []Tool
	Get(name string) (Tool, bool)
}

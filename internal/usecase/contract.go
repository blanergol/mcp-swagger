package usecase

import (
	"context"

	resource "github.com/blanergol/mcp-swagger/internal/resouce"
	"github.com/blanergol/mcp-swagger/internal/tool"
)

// Service содержит MCP business use-cases independent from transport и SDK.
type Service interface {
	ListTools(ctx context.Context) ([]tool.Descriptor, error)
	CallTool(ctx context.Context, name string, input any) (any, error)
	ListResources(ctx context.Context) ([]resource.Descriptor, error)
	GetResource(ctx context.Context, id string) (resource.Item, error)
	GetPrompt(ctx context.Context, name string, vars map[string]string) (string, error)
}

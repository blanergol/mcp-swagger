package usecase

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/blanergol/mcp-swagger/internal/prompt"
	resource "github.com/blanergol/mcp-swagger/internal/resouce"
	"github.com/blanergol/mcp-swagger/internal/swagger"
	"github.com/blanergol/mcp-swagger/internal/tool"
)

// service связывает интерфейсные зависимости usecase без раскрытия конкретных реализаций.
type service struct {
	toolRegistry  tool.Registry
	promptStore   prompt.Store
	resourceStore resource.Store
	swaggerStore  swagger.Store
}

// NewService создает usecase service wired with abstractions.
func NewService(
	toolRegistry tool.Registry,
	promptStore prompt.Store,
	resourceStore resource.Store,
	swaggerStore swagger.Store,
) Service {
	return &service{
		toolRegistry:  toolRegistry,
		promptStore:   promptStore,
		resourceStore: resourceStore,
		swaggerStore:  swaggerStore,
	}
}

// ListTools возвращает коллекцию доступных элементов в детерминированном порядке.
func (s *service) ListTools(_ context.Context) ([]tool.Descriptor, error) {
	tools := s.toolRegistry.List()
	out := make([]tool.Descriptor, 0, len(tools))
	for _, t := range tools {
		out = append(out, tool.Descriptor{
			Name:         t.Name(),
			Description:  t.Description(),
			InputSchema:  t.InputSchema(),
			OutputSchema: t.OutputSchema(),
		})
	}
	return out, nil
}

// CallTool выполняет отдельный этап обработки, необходимый для корректной работы типа.
func (s *service) CallTool(ctx context.Context, name string, input any) (any, error) {
	t, ok := s.toolRegistry.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %q is not registered", name)
	}
	if input == nil {
		input = map[string]any{}
	}
	return t.Execute(ctx, input)
}

// ListResources возвращает коллекцию доступных элементов в детерминированном порядке.
func (s *service) ListResources(ctx context.Context) ([]resource.Descriptor, error) {
	return s.resourceStore.List(ctx)
}

// GetResource возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func (s *service) GetResource(ctx context.Context, id string) (resource.Item, error) {
	return s.resourceStore.Get(ctx, id)
}

// GetPrompt возвращает значение по ключу, сохраняя семантику not found для вызывающего слоя.
func (s *service) GetPrompt(_ context.Context, name string, vars map[string]string) (string, error) {
	tpl, err := s.promptStore.Get(name)
	if err != nil {
		return "", err
	}
	if vars == nil {
		vars = map[string]string{}
	}
	rendered, err := renderTemplate(name, tpl, vars)
	if err != nil {
		return "", err
	}
	return rendered, nil
}

// renderTemplate выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func renderTemplate(name, tpl string, vars map[string]string) (string, error) {
	t, err := template.New(name).Option("missingkey=zero").Parse(tpl)
	if err != nil {
		return "", fmt.Errorf("parse prompt template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("execute prompt template: %w", err)
	}
	return buf.String(), nil
}

package streamable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/blanergol/mcp-swagger/config"
	authn "github.com/blanergol/mcp-swagger/internal/auth"
	"github.com/blanergol/mcp-swagger/internal/metrics"
	"github.com/blanergol/mcp-swagger/internal/prompt"
	resource "github.com/blanergol/mcp-swagger/internal/resouce"
	"github.com/blanergol/mcp-swagger/internal/server"
	"github.com/blanergol/mcp-swagger/internal/tool"
	"github.com/blanergol/mcp-swagger/internal/usecase"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server предоставляет Streamable HTTP MCP transport.
type Server struct {
	cfg       config.Config
	usecase   usecase.Service
	registry  tool.Registry
	prompts   prompt.Store
	resources resource.Store
	validator authn.Validator
	metrics   metrics.Recorder

	mu         sync.Mutex
	httpServer *http.Server
	mcpServer  *mcp.Server
}

// New создает streamable HTTP transport implementation.
func New(
	cfg config.Config,
	usecaseSvc usecase.Service,
	registry tool.Registry,
	promptStore prompt.Store,
	resourceStore resource.Store,
	validator authn.Validator,
	metricsRecorder metrics.Recorder,
) server.Transport {
	if metricsRecorder == nil {
		metricsRecorder = metrics.NewNoopRecorder()
	}
	return &Server{
		cfg:       cfg,
		usecase:   usecaseSvc,
		registry:  registry,
		prompts:   promptStore,
		resources: resourceStore,
		validator: validator,
		metrics:   metricsRecorder,
	}
}

// Start запускает HTTP-сервер streamable транспорта и блокируется до остановки.
func (s *Server) Start(ctx context.Context) error {
	mcpServer, err := s.buildMCPServer()
	if err != nil {
		return err
	}
	streamableHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{
		SessionTimeout: s.cfg.HTTPSessionTimeout,
		JSONResponse:   true,
		Logger:         slog.Default(),
	})

	httpHandler := s.newHTTPHandler(streamableHandler)
	httpServer := &http.Server{
		Addr:              s.cfg.HTTPAddr,
		Handler:           httpHandler,
		ReadTimeout:       s.cfg.HTTPReadTimeout,
		ReadHeaderTimeout: s.cfg.HTTPReadHeaderTimeout,
		WriteTimeout:      s.cfg.HTTPWriteTimeout,
		IdleTimeout:       s.cfg.HTTPIdleTimeout,
	}

	s.mu.Lock()
	s.mcpServer = mcpServer
	s.httpServer = httpServer
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		err := httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.HTTPShutdownTimeout)
		defer cancel()
		_ = s.Stop(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// Stop корректно останавливает HTTP-сервер с учетом timeout из контекста.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	httpServer := s.httpServer
	s.mu.Unlock()

	if httpServer == nil {
		return nil
	}
	return httpServer.Shutdown(ctx)
}

// buildMCPServer собирает зависимость или конфигурационный объект для текущего слоя.
func (s *Server) buildMCPServer() (*mcp.Server, error) {
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "mcp-server",
		Version: s.cfg.Version,
	}, nil)

	if err := s.registerTools(mcpServer); err != nil {
		return nil, err
	}
	if err := s.registerPrompts(mcpServer); err != nil {
		return nil, err
	}
	if err := s.registerResources(mcpServer); err != nil {
		return nil, err
	}
	return mcpServer, nil
}

// registerTools регистрирует сущности в SDK-сервере согласно доступным дескрипторам usecase.
func (s *Server) registerTools(mcpServer *mcp.Server) error {
	descriptors, err := s.usecase.ListTools(context.Background())
	if err != nil {
		return err
	}

	for _, d := range descriptors {
		descriptor := d
		inputSchema := descriptor.InputSchema
		if inputSchema == nil {
			inputSchema = map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": true,
			}
		}
		outputSchema := descriptor.OutputSchema

		mcpServer.AddTool(&mcp.Tool{
			Name:         descriptor.Name,
			Description:  descriptor.Description,
			InputSchema:  inputSchema,
			OutputSchema: outputSchema,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			input := any(map[string]any{})
			if req != nil && req.Params != nil && len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
					return nil, fmt.Errorf("invalid tool input: %w", err)
				}
			}

			output, err := s.usecase.CallTool(ctx, descriptor.Name, input)
			if err != nil {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
				}, nil
			}

			payload, err := json.Marshal(output)
			if err != nil {
				return nil, fmt.Errorf("marshal tool output: %w", err)
			}

			result := &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
			}
			if json.Valid(payload) {
				result.StructuredContent = json.RawMessage(payload)
			}
			return result, nil
		})
	}

	return nil
}

// registerPrompts регистрирует сущности в SDK-сервере согласно доступным дескрипторам usecase.
func (s *Server) registerPrompts(mcpServer *mcp.Server) error {
	for _, p := range s.prompts.List() {
		promptDesc := p
		args := make([]*mcp.PromptArgument, 0, len(promptDesc.Arguments))
		for _, arg := range promptDesc.Arguments {
			args = append(args, &mcp.PromptArgument{Name: arg})
		}

		mcpServer.AddPrompt(&mcp.Prompt{
			Name:        promptDesc.Name,
			Description: promptDesc.Description,
			Arguments:   args,
		}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			vars := map[string]string{}
			if req != nil && req.Params != nil && req.Params.Arguments != nil {
				vars = req.Params.Arguments
			}
			text, err := s.usecase.GetPrompt(ctx, promptDesc.Name, vars)
			if err != nil {
				return nil, err
			}
			return &mcp.GetPromptResult{
				Description: promptDesc.Description,
				Messages: []*mcp.PromptMessage{{
					Role:    mcp.Role("user"),
					Content: &mcp.TextContent{Text: text},
				}},
			}, nil
		})
	}
	return nil
}

// registerResources регистрирует сущности в SDK-сервере согласно доступным дескрипторам usecase.
func (s *Server) registerResources(mcpServer *mcp.Server) error {
	descriptors, err := s.usecase.ListResources(context.Background())
	if err != nil {
		return err
	}

	for _, d := range descriptors {
		descriptor := d
		if descriptor.IsTemplate() {
			mcpServer.AddResourceTemplate(&mcp.ResourceTemplate{
				Name:        descriptor.Name,
				Description: descriptor.Description,
				URITemplate: descriptor.URITemplate,
				MIMEType:    descriptor.MIMEType,
			}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return s.readResource(ctx, descriptor, req)
			})
			continue
		}

		mcpServer.AddResource(&mcp.Resource{
			Name:        descriptor.Name,
			Description: descriptor.Description,
			URI:         descriptor.URI,
			MIMEType:    descriptor.MIMEType,
		}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return s.readResource(ctx, descriptor, req)
		})
	}

	return nil
}

// readResource читает данные и преобразует их в формат доменного слоя.
func (s *Server) readResource(ctx context.Context, descriptor resource.Descriptor, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	resourceID := descriptor.URI
	if req != nil && req.Params != nil && req.Params.URI != "" {
		resourceID = req.Params.URI
	}
	if resourceID == "" {
		resourceID = descriptor.ID
	}

	item, err := s.usecase.GetResource(ctx, resourceID)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			uri := descriptor.URI
			if req != nil && req.Params != nil && req.Params.URI != "" {
				uri = req.Params.URI
			}
			if uri == "" {
				uri = resourceID
			}
			return nil, mcp.ResourceNotFoundError(uri)
		}
		return nil, err
	}

	uri := descriptor.URI
	if req != nil && req.Params != nil && req.Params.URI != "" {
		uri = req.Params.URI
	}
	if uri == "" {
		uri = resourceID
	}
	mimeType := descriptor.MIMEType
	if item.Descriptor.MIMEType != "" {
		mimeType = item.Descriptor.MIMEType
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: mimeType,
			Text:     item.Text,
		}},
	}, nil
}

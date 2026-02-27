package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

const (
	// swaggerScheme фиксирует строковый маркер протокола/контракта, используемый в нескольких местах.
	swaggerScheme = "swagger://"

	// resourceAllEndpoints фиксирует константу контракта, используемую в нескольких точках пакета.
	resourceAllEndpoints = "swagger:endpoints"
)

// supportedMethods хранит служебное значение, используемое внутри текущего пакета.
var supportedMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}

// SwaggerStore публикует данные swagger.Store как MCP resources.
type SwaggerStore struct {
	swagger swagger.Store
}

// NewSwaggerStore создает resource.Store поверх swagger.Store.
func NewSwaggerStore(swaggerStore swagger.Store) *SwaggerStore {
	return &SwaggerStore{swagger: swaggerStore}
}

// List возвращает каталог swagger-ресурсов и шаблонов URI.
func (s *SwaggerStore) List(ctx context.Context) ([]Descriptor, error) {
	if s.swagger == nil {
		return nil, nil
	}

	endpoints, err := s.swagger.ListEndpoints(ctx)
	if err != nil {
		return nil, err
	}

	descriptors := make([]Descriptor, 0, 3+len(supportedMethods)+len(endpoints))
	descriptors = append(descriptors, Descriptor{
		ID:          resourceAllEndpoints,
		Name:        resourceAllEndpoints,
		Description: "All resolved endpoints from the loaded OpenAPI document",
		URI:         swaggerScheme + "endpoints",
		MIMEType:    "application/json",
	})

	for _, method := range supportedMethods {
		name := "swagger:endpoints:" + method
		descriptors = append(descriptors, Descriptor{
			ID:          name,
			Name:        name,
			Description: "Resolved endpoints filtered by HTTP method",
			URI:         swaggerScheme + "endpoints/" + method,
			MIMEType:    "application/json",
		})
	}

	descriptors = append(descriptors,
		Descriptor{
			ID:          "swagger:endpointByOperationId:{operationId}",
			Name:        "swagger:endpointByOperationId:{operationId}",
			Description: "Single endpoint resolved by operationId",
			URITemplate: swaggerScheme + "endpointByOperationId/{operationId}",
			MIMEType:    "application/json",
		},
		Descriptor{
			ID:          "swagger:schema:{name}",
			Name:        "swagger:schema:{name}",
			Description: "Schema component by name",
			URITemplate: swaggerScheme + "schema/{name}",
			MIMEType:    "application/json",
		},
		Descriptor{
			ID:          "swagger:lookup:{pointer}",
			Name:        "swagger:lookup:{pointer}",
			Description: "Generic lookup by JSON pointer/path-like expression",
			URITemplate: swaggerScheme + "lookup/{pointer}",
			MIMEType:    "application/json",
		},
	)

	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint.OperationID) == "" {
			continue
		}
		escapedID := url.PathEscape(endpoint.OperationID)
		descriptors = append(descriptors, Descriptor{
			ID:          "swagger:endpointByOperationId:" + endpoint.OperationID,
			Name:        "swagger:endpointByOperationId:" + endpoint.OperationID,
			Description: "Resolved endpoint for operationId",
			URI:         swaggerScheme + "endpointByOperationId/" + escapedID,
			MIMEType:    "application/json",
		})
	}

	schemaNames := s.listSchemaNames(ctx)
	for _, name := range schemaNames {
		descriptors = append(descriptors, Descriptor{
			ID:          "swagger:schema:" + name,
			Name:        "swagger:schema:" + name,
			Description: "Schema component",
			URI:         swaggerScheme + "schema/" + url.PathEscape(name),
			MIMEType:    "application/json",
		})
	}

	sort.Slice(descriptors, func(i, j int) bool {
		left := descriptors[i]
		right := descriptors[j]
		leftKey := left.URI
		if left.URITemplate != "" {
			leftKey = left.URITemplate
		}
		rightKey := right.URI
		if right.URITemplate != "" {
			rightKey = right.URITemplate
		}
		return leftKey < rightKey
	})

	return descriptors, nil
}

// Get разрешает swagger-backed resource content.
func (s *SwaggerStore) Get(ctx context.Context, id string) (Item, error) {
	if s.swagger == nil {
		return Item{}, ErrNotFound
	}
	parsed, err := parseSwaggerResourceKey(id)
	if err != nil {
		return Item{}, err
	}

	var (
		value      any
		descriptor Descriptor
	)

	switch parsed.Kind {
	case "all-endpoints":
		value, err = s.swagger.ListEndpoints(ctx)
		descriptor = Descriptor{
			ID:          resourceAllEndpoints,
			Name:        resourceAllEndpoints,
			Description: "All resolved endpoints from the loaded OpenAPI document",
			URI:         swaggerScheme + "endpoints",
			MIMEType:    "application/json",
		}
	case "method":
		method := strings.ToUpper(strings.TrimSpace(parsed.Arg))
		value, err = s.swagger.ListEndpointsByMethod(ctx, method)
		descriptor = Descriptor{
			ID:          "swagger:endpoints:" + method,
			Name:        "swagger:endpoints:" + method,
			Description: "Resolved endpoints filtered by HTTP method",
			URI:         swaggerScheme + "endpoints/" + method,
			MIMEType:    "application/json",
		}
	case "operation-id":
		value, err = s.swagger.GetEndpointByOperationID(ctx, parsed.Arg)
		descriptor = Descriptor{
			ID:          "swagger:endpointByOperationId:" + parsed.Arg,
			Name:        "swagger:endpointByOperationId:" + parsed.Arg,
			Description: "Single endpoint resolved by operationId",
			URI:         swaggerScheme + "endpointByOperationId/" + url.PathEscape(parsed.Arg),
			MIMEType:    "application/json",
		}
	case "schema":
		value, err = s.swagger.GetSchemaByName(ctx, parsed.Arg)
		descriptor = Descriptor{
			ID:          "swagger:schema:" + parsed.Arg,
			Name:        "swagger:schema:" + parsed.Arg,
			Description: "Schema component by name",
			URI:         swaggerScheme + "schema/" + url.PathEscape(parsed.Arg),
			MIMEType:    "application/json",
		}
	case "lookup":
		value, err = s.swagger.Lookup(ctx, parsed.Arg)
		descriptor = Descriptor{
			ID:          "swagger:lookup:" + parsed.Arg,
			Name:        "swagger:lookup:" + parsed.Arg,
			Description: "Generic lookup by JSON pointer/path-like expression",
			URI:         swaggerScheme + "lookup/" + url.PathEscape(parsed.Arg),
			MIMEType:    "application/json",
		}
	default:
		return Item{}, ErrNotFound
	}
	if err != nil {
		if errors.Is(err, swagger.ErrNotFound) {
			return Item{}, ErrNotFound
		}
		return Item{}, err
	}

	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return Item{}, fmt.Errorf("marshal swagger resource payload: %w", err)
	}

	return Item{Descriptor: descriptor, Text: string(payload)}, nil
}

// listSchemaNames возвращает коллекцию доступных элементов в детерминированном порядке.
func (s *SwaggerStore) listSchemaNames(ctx context.Context) []string {
	value, err := s.swagger.Lookup(ctx, "/components/schemas")
	if err != nil {
		return nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// parsedSwaggerKey используется как приватный ключ context, чтобы избежать коллизий между пакетами.
type parsedSwaggerKey struct {
	Kind string
	Arg  string
}

// parseSwaggerResourceKey разбирает входные данные и возвращает нормализованное представление.
func parseSwaggerResourceKey(raw string) (parsedSwaggerKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsedSwaggerKey{}, ErrNotFound
	}

	if strings.HasPrefix(raw, swaggerScheme) {
		return parseSwaggerURI(raw)
	}
	if strings.HasPrefix(raw, "swagger:") {
		return parseSwaggerName(raw)
	}
	return parsedSwaggerKey{}, ErrNotFound
}

// parseSwaggerURI разбирает входные данные и возвращает нормализованное представление.
func parseSwaggerURI(raw string) (parsedSwaggerKey, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return parsedSwaggerKey{}, err
	}
	if u.Scheme != "swagger" {
		return parsedSwaggerKey{}, ErrNotFound
	}

	host := strings.TrimSpace(u.Host)
	path := strings.TrimPrefix(u.EscapedPath(), "/")
	switch host {
	case "endpoints":
		if path == "" {
			return parsedSwaggerKey{Kind: "all-endpoints"}, nil
		}
		method, err := url.PathUnescape(path)
		if err != nil {
			method = path
		}
		return parsedSwaggerKey{Kind: "method", Arg: strings.ToUpper(strings.TrimSpace(method))}, nil
	case "endpointByOperationId":
		opID, err := url.PathUnescape(path)
		if err != nil {
			opID = path
		}
		opID = strings.TrimSpace(opID)
		if opID == "" {
			return parsedSwaggerKey{}, ErrNotFound
		}
		return parsedSwaggerKey{Kind: "operation-id", Arg: opID}, nil
	case "schema":
		name, err := url.PathUnescape(path)
		if err != nil {
			name = path
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return parsedSwaggerKey{}, ErrNotFound
		}
		return parsedSwaggerKey{Kind: "schema", Arg: name}, nil
	case "lookup":
		pointer, err := url.PathUnescape(path)
		if err != nil {
			pointer = path
		}
		pointer = strings.TrimSpace(pointer)
		if pointer == "" {
			return parsedSwaggerKey{}, ErrNotFound
		}
		if !strings.HasPrefix(pointer, "/") {
			pointer = "/" + pointer
		}
		return parsedSwaggerKey{Kind: "lookup", Arg: pointer}, nil
	default:
		return parsedSwaggerKey{}, ErrNotFound
	}
}

// parseSwaggerName разбирает входные данные и возвращает нормализованное представление.
func parseSwaggerName(raw string) (parsedSwaggerKey, error) {
	switch {
	case raw == resourceAllEndpoints:
		return parsedSwaggerKey{Kind: "all-endpoints"}, nil
	case strings.HasPrefix(raw, "swagger:endpoints:"):
		return parsedSwaggerKey{Kind: "method", Arg: strings.TrimPrefix(raw, "swagger:endpoints:")}, nil
	case strings.HasPrefix(raw, "swagger:endpointByOperationId:"):
		opID := strings.TrimPrefix(raw, "swagger:endpointByOperationId:")
		if opID == "" {
			return parsedSwaggerKey{}, ErrNotFound
		}
		return parsedSwaggerKey{Kind: "operation-id", Arg: opID}, nil
	case strings.HasPrefix(raw, "swagger:schema:"):
		name := strings.TrimPrefix(raw, "swagger:schema:")
		if name == "" {
			return parsedSwaggerKey{}, ErrNotFound
		}
		return parsedSwaggerKey{Kind: "schema", Arg: name}, nil
	case strings.HasPrefix(raw, "swagger:lookup:"):
		pointer := strings.TrimPrefix(raw, "swagger:lookup:")
		if pointer == "" {
			return parsedSwaggerKey{}, ErrNotFound
		}
		if !strings.HasPrefix(pointer, "/") {
			pointer = "/" + pointer
		}
		return parsedSwaggerKey{Kind: "lookup", Arg: pointer}, nil
	default:
		return parsedSwaggerKey{}, ErrNotFound
	}
}

package swagger

import "context"

// Loader загружает raw OpenAPI/Swagger bytes from source.
type Loader interface {
	Load(ctx context.Context) ([]byte, error)
}

// Parser парсит raw spec bytes into internal document model.
type Parser interface {
	Parse(ctx context.Context, raw []byte) (*Document, error)
}

// Resolver разрешает operation details including refs.
type Resolver interface {
	ResolveEndpoint(ctx context.Context, op Operation) (ResolvedOperation, error)
}

// Store предоставляет API выборки по распарсенному OpenAPI-документу.
type Store interface {
	ListEndpoints(ctx context.Context) ([]ResolvedOperation, error)
	ListEndpointsByMethod(ctx context.Context, method string) ([]ResolvedOperation, error)
	GetEndpointByOperationID(ctx context.Context, opID string) (ResolvedOperation, error)
	GetSchemaByName(ctx context.Context, name string) (any, error)
	Lookup(ctx context.Context, pointer string) (any, error)
}

// Document является parsed OpenAPI document with raw object representation.
type Document struct {
	Spec *OpenAPISpec
	Raw  any
}

// Operation идентифицирует операцию endpoint в OpenAPI.
type Operation struct {
	Method      string
	Path        string
	OperationID string

	PathItem *OpenAPIPathItem
	Value    *OpenAPIOperation
	Doc      *Document
}

// Param описывает параметр операции.
type Param struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
	Schema      any    `json:"schema,omitempty"`
}

// Request описывает тело входящего запроса операции.
type Request struct {
	ContentTypes []string `json:"contentTypes,omitempty"`
	BodySchema   any      `json:"bodySchema,omitempty"`
	Examples     any      `json:"examples,omitempty"`
}

// Response описывает успешный ответ операции.
type Response struct {
	Status       int      `json:"status"`
	Description  string   `json:"description,omitempty"`
	ContentTypes []string `json:"contentTypes,omitempty"`
	BodySchema   any      `json:"bodySchema,omitempty"`
}

// ErrorResponse описывает ответ с ошибкой (обычно 4xx/5xx).
type ErrorResponse struct {
	Status       int      `json:"status"`
	Description  string   `json:"description,omitempty"`
	ContentTypes []string `json:"contentTypes,omitempty"`
	BodySchema   any      `json:"bodySchema,omitempty"`
}

// ResponseGroups groups success и error responses.
type ResponseGroups struct {
	Success []Response      `json:"success,omitempty"`
	Errors  []ErrorResponse `json:"errors,omitempty"`
}

// ResolvedOperation является MCP-facing endpoint DTO.
type ResolvedOperation struct {
	Method       string `json:"method"`
	BaseURL      string `json:"baseURL,omitempty"`
	PathTemplate string `json:"pathTemplate"`
	URLTemplate  string `json:"urlTemplate,omitempty"`
	ExampleURL   string `json:"exampleURL,omitempty"`
	OperationID  string `json:"operationId"`

	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Deprecated  bool     `json:"deprecated"`

	PathParams   []Param `json:"pathParams,omitempty"`
	QueryParams  []Param `json:"queryParams,omitempty"`
	HeaderParams []Param `json:"headerParams,omitempty"`
	CookieParams []Param `json:"cookieParams,omitempty"`

	Request   Request        `json:"request"`
	Responses ResponseGroups `json:"responses"`

	Security any      `json:"security,omitempty"`
	Servers  []string `json:"servers,omitempty"`
}

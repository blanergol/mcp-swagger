package swagger

import "github.com/getkin/kin-openapi/openapi3"

// OpenAPISpec aliases the OpenAPI root document type.
type (
	// OpenAPISpec aliases openapi3.T for package-level contract types.
	OpenAPISpec = openapi3.T
	// OpenAPIPathItem aliases openapi3.PathItem for route-level metadata.
	OpenAPIPathItem = openapi3.PathItem
	// OpenAPIOperation aliases openapi3.Operation for operation-level metadata.
	OpenAPIOperation = openapi3.Operation
)

package swagger

import "github.com/getkin/kin-openapi/openapi3"

// Type aliases изолируют contract.go от прямой зависимости на имена типов openapi3.
type (
	OpenAPISpec      = openapi3.T
	OpenAPIPathItem  = openapi3.PathItem
	OpenAPIOperation = openapi3.Operation
)

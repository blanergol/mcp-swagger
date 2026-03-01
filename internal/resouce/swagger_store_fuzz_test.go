package resource

import "testing"

// FuzzParseSwaggerResourceKeyNoPanic protects parser robustness and output invariants.
func FuzzParseSwaggerResourceKeyNoPanic(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"swagger://endpoints",
		"swagger://endpoints/get",
		"swagger://endpoints/POST",
		"swagger://endpointByOperationId/getPet",
		"swagger://endpointByOperationId/get%20Pet",
		"swagger://schema/Pet",
		"swagger://schema/My%20Type",
		"swagger://lookup/components/schemas/Pet",
		"swagger://lookup/%2Fcomponents%2Fschemas%2FPet",
		"swagger:endpoints",
		"swagger:endpoints:GET",
		"swagger:endpointByOperationId:getPet",
		"swagger:schema:Pet",
		"swagger:lookup:/components/schemas/Pet",
		"swagger:lookup:components/schemas/Pet",
		"swagger://unknown/k",
		"swagger:unknown:v",
		"http://example.com",
		"file:///tmp/openapi.yaml",
		"swagger://lookup",
		"swagger://schema",
		"swagger://endpointByOperationId",
		"swagger:schema:",
		"swagger:lookup:",
		"swagger:endpointByOperationId:",
		"swagger://endpoints/%20patch%20",
		"swagger://lookup/%2e%2e/%2e%2e/etc/passwd",
		"swagger://schema/%00",
		"swagger://lookup/路径/组件",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		parsed, err := parseSwaggerResourceKey(raw)
		if err != nil {
			return
		}
		switch parsed.Kind {
		case "all-endpoints", "method", "operation-id", "schema", "lookup":
		default:
			t.Fatalf("unexpected kind %q for %q", parsed.Kind, raw)
		}
		if parsed.Kind == "lookup" && parsed.Arg != "" && parsed.Arg[0] != '/' {
			t.Fatalf("lookup arg must start with slash, got %q", parsed.Arg)
		}
	})
}

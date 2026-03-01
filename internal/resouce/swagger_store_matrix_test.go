package resource

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

// TestParseSwaggerURIMatrix protects URI parser normalization and failure behavior.
func TestParseSwaggerURIMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    parsedSwaggerKey
		wantErr error
	}{
		{name: "all endpoints", raw: "swagger://endpoints", want: parsedSwaggerKey{Kind: "all-endpoints"}},
		{name: "method upper", raw: "swagger://endpoints/get", want: parsedSwaggerKey{Kind: "method", Arg: "GET"}},
		{name: "method trimmed", raw: "swagger://endpoints/%20post%20", want: parsedSwaggerKey{Kind: "method", Arg: "POST"}},
		{name: "operation id escaped", raw: "swagger://endpointByOperationId/get%20User", want: parsedSwaggerKey{Kind: "operation-id", Arg: "get User"}},
		{name: "operation id empty", raw: "swagger://endpointByOperationId", wantErr: ErrNotFound},
		{name: "schema escaped", raw: "swagger://schema/My%20Type", want: parsedSwaggerKey{Kind: "schema", Arg: "My Type"}},
		{name: "schema empty", raw: "swagger://schema", wantErr: ErrNotFound},
		{name: "lookup no slash", raw: "swagger://lookup/components/schemas/Pet", want: parsedSwaggerKey{Kind: "lookup", Arg: "/components/schemas/Pet"}},
		{name: "lookup with slash", raw: "swagger://lookup/%2Fcomponents%2Fschemas%2FPet", want: parsedSwaggerKey{Kind: "lookup", Arg: "/components/schemas/Pet"}},
		{name: "lookup empty", raw: "swagger://lookup", wantErr: ErrNotFound},
		{name: "unknown host", raw: "swagger://unknown/x", wantErr: ErrNotFound},
		{name: "unsupported scheme", raw: "http://example.com", wantErr: ErrNotFound},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSwaggerURI(tc.raw)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("parseSwaggerURI error=%v want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSwaggerURI unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseSwaggerURI got %+v want %+v", got, tc.want)
			}
		})
	}
}

// TestParseSwaggerNameMatrix protects name-parser behavior for explicit resource IDs.
func TestParseSwaggerNameMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    parsedSwaggerKey
		wantErr error
	}{
		{name: "all endpoints", raw: "swagger:endpoints", want: parsedSwaggerKey{Kind: "all-endpoints"}},
		{name: "method", raw: "swagger:endpoints:PATCH", want: parsedSwaggerKey{Kind: "method", Arg: "PATCH"}},
		{name: "operation id", raw: "swagger:endpointByOperationId:getUser", want: parsedSwaggerKey{Kind: "operation-id", Arg: "getUser"}},
		{name: "operation id missing", raw: "swagger:endpointByOperationId:", wantErr: ErrNotFound},
		{name: "schema", raw: "swagger:schema:Pet", want: parsedSwaggerKey{Kind: "schema", Arg: "Pet"}},
		{name: "schema missing", raw: "swagger:schema:", wantErr: ErrNotFound},
		{name: "lookup adds slash", raw: "swagger:lookup:components/schemas/Pet", want: parsedSwaggerKey{Kind: "lookup", Arg: "/components/schemas/Pet"}},
		{name: "lookup existing slash", raw: "swagger:lookup:/components/schemas/Pet", want: parsedSwaggerKey{Kind: "lookup", Arg: "/components/schemas/Pet"}},
		{name: "lookup missing", raw: "swagger:lookup:", wantErr: ErrNotFound},
		{name: "unknown", raw: "swagger:other:value", wantErr: ErrNotFound},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSwaggerName(tc.raw)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("parseSwaggerName error=%v want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSwaggerName unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseSwaggerName got %+v want %+v", got, tc.want)
			}
		})
	}
}

// TestSwaggerStoreGetKinds protects Get dispatcher for all supported key kinds.
func TestSwaggerStoreGetKinds(t *testing.T) {
	t.Parallel()

	fake := &fakeSwaggerStore{
		endpoints: []swagger.ResolvedOperation{{OperationID: "getPet", Method: "GET"}},
		schemasByName: map[string]any{
			"Pet": map[string]any{"type": "object"},
		},
		lookupByPointer: map[string]any{
			"/components/schemas/Pet": map[string]any{"type": "object"},
			"/components/schemas":     map[string]any{"Pet": map[string]any{}},
		},
	}
	store := NewSwaggerStore(fake)

	tests := []struct {
		name string
		id   string
	}{
		{name: "all endpoints uri", id: "swagger://endpoints"},
		{name: "method uri", id: "swagger://endpoints/get"},
		{name: "operation uri", id: "swagger://endpointByOperationId/getPet"},
		{name: "schema uri", id: "swagger://schema/Pet"},
		{name: "lookup uri", id: "swagger://lookup/components/schemas/Pet"},
		{name: "all endpoints name", id: "swagger:endpoints"},
		{name: "method name", id: "swagger:endpoints:GET"},
		{name: "operation name", id: "swagger:endpointByOperationId:getPet"},
		{name: "schema name", id: "swagger:schema:Pet"},
		{name: "lookup name", id: "swagger:lookup:/components/schemas/Pet"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			item, err := store.Get(context.Background(), tc.id)
			if err != nil {
				t.Fatalf("Get(%q) error: %v", tc.id, err)
			}
			if item.Text == "" {
				t.Fatalf("Get(%q) returned empty payload", tc.id)
			}
		})
	}
}

// TestSwaggerStoreListPropertyDeterministic protects deterministic ordering invariant for List.
func TestSwaggerStoreListPropertyDeterministic(t *testing.T) {
	t.Parallel()

	fake := &fakeSwaggerStore{
		endpoints: []swagger.ResolvedOperation{
			{OperationID: "zOp", Method: "GET"},
			{OperationID: "aOp", Method: "POST"},
			{OperationID: "mOp", Method: "DELETE"},
		},
		lookupByPointer: map[string]any{
			"/components/schemas": map[string]any{"Zed": map[string]any{}, "Alpha": map[string]any{}},
		},
	}
	store := NewSwaggerStore(fake)

	first, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("first list error: %v", err)
	}
	second, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("second list error: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("List must be deterministic between calls")
	}

	keys := make([]string, 0, len(first))
	for _, d := range first {
		key := d.URI
		if d.URITemplate != "" {
			key = d.URITemplate
		}
		keys = append(keys, key)
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatalf("List keys must be sorted: %v", keys)
	}
}

package resource

import (
	"context"
	"errors"
	"testing"

	"github.com/blanergol/mcp-swagger/internal/swagger"
)

type fakeSwaggerStore struct {
	endpoints         []swagger.ResolvedOperation
	schemasByName     map[string]any
	lookupByPointer   map[string]any
	listEndpointsErr  error
	listByMethodErr   error
	getByOperationErr error
	getSchemaErr      error
	lookupErr         error
	capturedMethod    string
}

func (s *fakeSwaggerStore) ListEndpoints(context.Context) ([]swagger.ResolvedOperation, error) {
	if s.listEndpointsErr != nil {
		return nil, s.listEndpointsErr
	}
	return append([]swagger.ResolvedOperation(nil), s.endpoints...), nil
}

func (s *fakeSwaggerStore) ListEndpointsByMethod(_ context.Context, method string) ([]swagger.ResolvedOperation, error) {
	s.capturedMethod = method
	if s.listByMethodErr != nil {
		return nil, s.listByMethodErr
	}
	out := make([]swagger.ResolvedOperation, 0)
	for _, ep := range s.endpoints {
		if ep.Method == method {
			out = append(out, ep)
		}
	}
	return out, nil
}

func (s *fakeSwaggerStore) GetEndpointByOperationID(_ context.Context, opID string) (swagger.ResolvedOperation, error) {
	if s.getByOperationErr != nil {
		return swagger.ResolvedOperation{}, s.getByOperationErr
	}
	for _, ep := range s.endpoints {
		if ep.OperationID == opID {
			return ep, nil
		}
	}
	return swagger.ResolvedOperation{}, swagger.ErrNotFound
}

func (s *fakeSwaggerStore) GetSchemaByName(_ context.Context, name string) (any, error) {
	if s.getSchemaErr != nil {
		return nil, s.getSchemaErr
	}
	if v, ok := s.schemasByName[name]; ok {
		return v, nil
	}
	return nil, swagger.ErrNotFound
}

func (s *fakeSwaggerStore) Lookup(_ context.Context, pointer string) (any, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	if v, ok := s.lookupByPointer[pointer]; ok {
		return v, nil
	}
	return nil, swagger.ErrNotFound
}

// TestParseSwaggerResourceKeyGuards protects URI/name parsing and normalization invariants.
func TestParseSwaggerResourceKeyGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    parsedSwaggerKey
		wantErr error
	}{
		{name: "all endpoints by URI", raw: "swagger://endpoints", want: parsedSwaggerKey{Kind: "all-endpoints"}},
		{name: "method URI is upper-cased", raw: "swagger://endpoints/get", want: parsedSwaggerKey{Kind: "method", Arg: "GET"}},
		{name: "operation ID URI is unescaped", raw: "swagger://endpointByOperationId/get%20Pet", want: parsedSwaggerKey{Kind: "operation-id", Arg: "get Pet"}},
		{name: "lookup URI prepends slash", raw: "swagger://lookup/components/schemas/Pet", want: parsedSwaggerKey{Kind: "lookup", Arg: "/components/schemas/Pet"}},
		{name: "lookup name form prepends slash", raw: "swagger:lookup:components/schemas/Pet", want: parsedSwaggerKey{Kind: "lookup", Arg: "/components/schemas/Pet"}},
		{name: "schema name form", raw: "swagger:schema:Pet", want: parsedSwaggerKey{Kind: "schema", Arg: "Pet"}},
		{name: "unsupported scheme", raw: "http://example.com", wantErr: ErrNotFound},
		{name: "empty value", raw: "", wantErr: ErrNotFound},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSwaggerResourceKey(tc.raw)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected error %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected parsed key: got %+v want %+v", got, tc.want)
			}
		})
	}
}

// TestSwaggerStoreGetErrorMappingGuards protects mapping of swagger.ErrNotFound to resource.ErrNotFound.
func TestSwaggerStoreGetErrorMappingGuards(t *testing.T) {
	t.Parallel()

	store := NewSwaggerStore(&fakeSwaggerStore{})
	_, err := store.Get(context.Background(), "swagger://schema/Missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected resource ErrNotFound, got %v", err)
	}
}

// TestSwaggerStoreGetMethodNormalizesToUpper protects method normalization contract for endpoint lists by method.
func TestSwaggerStoreGetMethodNormalizesToUpper(t *testing.T) {
	t.Parallel()

	fake := &fakeSwaggerStore{endpoints: []swagger.ResolvedOperation{{Method: "GET", OperationID: "getInventory"}}}
	store := NewSwaggerStore(fake)

	_, err := store.Get(context.Background(), "swagger://endpoints/get")
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if fake.capturedMethod != "GET" {
		t.Fatalf("expected normalized method GET, got %q", fake.capturedMethod)
	}
}

// TestSwaggerStoreListDeterministicAndSkipsEmptyOperationID protects descriptor determinism and filtering.
func TestSwaggerStoreListDeterministicAndSkipsEmptyOperationID(t *testing.T) {
	t.Parallel()

	fake := &fakeSwaggerStore{
		endpoints: []swagger.ResolvedOperation{
			{OperationID: "zOp", Method: "GET"},
			{OperationID: "", Method: "POST"},
			{OperationID: "aOp", Method: "GET"},
		},
		lookupByPointer: map[string]any{
			"/components/schemas": map[string]any{"Pet": map[string]any{}, "User": map[string]any{}},
		},
	}
	store := NewSwaggerStore(fake)

	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list returned unexpected error: %v", err)
	}
	if len(list) == 0 {
		t.Fatalf("expected non-empty descriptors list")
	}

	keys := make([]string, 0, len(list))
	seenZOp := false
	for _, d := range list {
		key := d.URI
		if d.URITemplate != "" {
			key = d.URITemplate
		}
		keys = append(keys, key)
		if d.ID == "swagger:endpointByOperationId:zOp" {
			seenZOp = true
		}
		if d.ID == "swagger:endpointByOperationId:" {
			t.Fatalf("empty operationId must not produce descriptor")
		}
	}

	if !seenZOp {
		t.Fatalf("expected operation descriptor for zOp")
	}

	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Fatalf("descriptors must be sorted by key: %q before %q", keys[i-1], keys[i])
		}
	}
}

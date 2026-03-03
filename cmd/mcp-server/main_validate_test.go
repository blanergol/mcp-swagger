package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blanergol/mcp-swagger/config"
	"github.com/blanergol/mcp-swagger/internal/netguard"
	"github.com/blanergol/mcp-swagger/internal/swagger"
)

type stubSwaggerStore struct {
	endpoints []swagger.ResolvedOperation
	err       error
}

func (s stubSwaggerStore) ListEndpoints(context.Context) ([]swagger.ResolvedOperation, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]swagger.ResolvedOperation(nil), s.endpoints...), nil
}

func (s stubSwaggerStore) ListEndpointsByMethod(context.Context, string) ([]swagger.ResolvedOperation, error) {
	return nil, errors.New("not implemented")
}

func (s stubSwaggerStore) GetEndpointByOperationID(context.Context, string) (swagger.ResolvedOperation, error) {
	return swagger.ResolvedOperation{}, errors.New("not implemented")
}

func (s stubSwaggerStore) GetSchemaByName(context.Context, string) (any, error) {
	return nil, errors.New("not implemented")
}

func (s stubSwaggerStore) Lookup(context.Context, string) (any, error) {
	return nil, errors.New("not implemented")
}

func TestValidateSwaggerTargetsIgnoresRelativeServerURLs(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		SwaggerPath: "https://petstore3.swagger.io/api/v3/openapi.json",
	}
	guard := netguard.New(netguard.Config{
		AllowedHosts:         []string{"petstore3.swagger.io"},
		BlockPrivateNetworks: false,
	})
	store := stubSwaggerStore{endpoints: []swagger.ResolvedOperation{
		{
			OperationID: "findPetsByStatus",
			Servers:     []string{"/api/v3"},
			BaseURL:     "/api/v3",
		},
	}}

	if err := validateSwaggerTargets(context.Background(), cfg, store, guard); err != nil {
		t.Fatalf("validateSwaggerTargets returned error for relative URLs: %v", err)
	}
}

func TestValidateSwaggerTargetsStillBlocksDisallowedAbsoluteServers(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		SwaggerPath: "https://petstore3.swagger.io/api/v3/openapi.json",
	}
	guard := netguard.New(netguard.Config{
		AllowedHosts:         []string{"petstore3.swagger.io"},
		BlockPrivateNetworks: false,
	})
	store := stubSwaggerStore{endpoints: []swagger.ResolvedOperation{
		{
			OperationID: "findPetsByStatus",
			Servers:     []string{"https://evil.example.com/v1"},
		},
	}}

	err := validateSwaggerTargets(context.Background(), cfg, store, guard)
	if err == nil {
		t.Fatalf("expected error for disallowed absolute swagger server")
	}
	if !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

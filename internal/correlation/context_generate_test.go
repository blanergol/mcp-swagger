package correlation

import (
	"context"
	"regexp"
	"testing"
)

// TestContextAndHeaderHelpersGuards protects context/header contracts for correlation IDs.
func TestContextAndHeaderHelpersGuards(t *testing.T) {
	t.Parallel()

	ctx := ContextWithID(context.Background(), "  req-123  ")
	id, ok := IDFromContext(ctx)
	if !ok || id != "req-123" {
		t.Fatalf("expected normalized correlation ID in context, got %q ok=%v", id, ok)
	}

	emptyCtx := ContextWithID(context.Background(), "   ")
	if _, ok := IDFromContext(emptyCtx); ok {
		t.Fatalf("empty IDs must not be stored in context")
	}

	if HeaderName("") != "X-Correlation-Id" {
		t.Fatalf("expected default correlation header")
	}
	if HeaderName("  X-Request-ID ") != "X-Request-ID" {
		t.Fatalf("expected configured header to be trimmed")
	}
}

// TestGenerateProducesUUIDv4LikeGuards validates generated ID format and UUIDv4 marker bits.
func TestGenerateProducesUUIDv4LikeGuards(t *testing.T) {
	t.Parallel()

	uuidV4Pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	for i := 0; i < 20; i++ {
		id := Generate()
		if !uuidV4Pattern.MatchString(id) {
			t.Fatalf("generated ID must be UUIDv4-like, got %q", id)
		}
	}
}

package confirmation

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestMemoryStoreRequestNormalizesAndReturnsDetachedCopy guards request normalization and clone contract.
func TestMemoryStoreRequestNormalizesAndReturnsDetachedCopy(t *testing.T) {
	t.Parallel()

	baseNow := time.Date(2026, time.February, 28, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore(0)
	store.now = func() time.Time { return baseNow }

	reqSummary := map[string]any{
		"nested": map[string]any{"flag": "original"},
	}
	rec, err := store.Request(context.Background(), Request{
		OperationID:            "  addPet  ",
		Method:                 " post ",
		FinalURL:               " https://example.com/pet ",
		PreparedRequestSummary: reqSummary,
		Reason:                 "  needs approval  ",
	})
	if err != nil {
		t.Fatalf("request returned unexpected error: %v", err)
	}

	if rec.OperationID != "addPet" {
		t.Fatalf("expected trimmed operationId, got %q", rec.OperationID)
	}
	if rec.Method != "POST" {
		t.Fatalf("expected upper-cased method, got %q", rec.Method)
	}
	if rec.FinalURL != "https://example.com/pet" {
		t.Fatalf("expected trimmed finalURL, got %q", rec.FinalURL)
	}
	if rec.Reason != "needs approval" {
		t.Fatalf("expected trimmed reason, got %q", rec.Reason)
	}
	if ttl := rec.ExpiresAt.Sub(rec.CreatedAt); ttl != defaultTTL {
		t.Fatalf("expected default TTL %s, got %s", defaultTTL, ttl)
	}

	returned := rec.PreparedRequestSummary.(map[string]any)
	returnedNested := returned["nested"].(map[string]any)
	returnedNested["flag"] = "changed-in-returned"

	confirmed, err := store.Confirm(context.Background(), rec.ID, true)
	if err != nil {
		t.Fatalf("confirm returned unexpected error: %v", err)
	}

	stored := confirmed.PreparedRequestSummary.(map[string]any)
	storedNested := stored["nested"].(map[string]any)
	if storedNested["flag"] != "original" {
		t.Fatalf("store must keep detached summary copy, got %#v", storedNested["flag"])
	}
}

// TestMemoryStoreApproveConsumeLifecycle guards one-time approved-consume lifecycle.
func TestMemoryStoreApproveConsumeLifecycle(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(5 * time.Minute)
	rec := mustRequest(t, store, Request{OperationID: "getInventory", Method: "GET", FinalURL: "https://api.example.com/inventory"})

	approved, err := store.Confirm(context.Background(), rec.ID, true)
	if err != nil {
		t.Fatalf("confirm returned unexpected error: %v", err)
	}
	if !approved.Approved {
		t.Fatalf("expected record to be approved")
	}

	consumed, err := store.ConsumeApproved(context.Background(), rec.ID, Check{
		OperationID: "getInventory",
		Method:      "GET",
		FinalURL:    "https://api.example.com/inventory",
	})
	if err != nil {
		t.Fatalf("consume returned unexpected error: %v", err)
	}
	if !consumed.Consumed {
		t.Fatalf("expected record to be marked consumed")
	}

	_, err = store.ConsumeApproved(context.Background(), rec.ID, Check{OperationID: "getInventory", Method: "GET", FinalURL: "https://api.example.com/inventory"})
	if !errors.Is(err, ErrConsumed) {
		t.Fatalf("expected ErrConsumed on second consume, got %v", err)
	}
}

// TestMemoryStoreConsumeValidationErrors guards negative consume paths (not approved and mismatch).
func TestMemoryStoreConsumeValidationErrors(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(5 * time.Minute)
	rec := mustRequest(t, store, Request{OperationID: "deletePet", Method: "DELETE", FinalURL: "https://api.example.com/pet/1"})

	_, err := store.ConsumeApproved(context.Background(), rec.ID, Check{OperationID: "deletePet", Method: "DELETE", FinalURL: "https://api.example.com/pet/1"})
	if !errors.Is(err, ErrNotApproved) {
		t.Fatalf("expected ErrNotApproved, got %v", err)
	}

	_, err = store.Confirm(context.Background(), rec.ID, true)
	if err != nil {
		t.Fatalf("confirm returned unexpected error: %v", err)
	}

	_, err = store.ConsumeApproved(context.Background(), rec.ID, Check{OperationID: "otherOperation", Method: "DELETE", FinalURL: "https://api.example.com/pet/1"})
	if !errors.Is(err, ErrMismatch) {
		t.Fatalf("expected ErrMismatch, got %v", err)
	}
}

// TestMemoryStoreExpirationGuards confirms expired records are rejected and removed.
func TestMemoryStoreExpirationGuards(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 28, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore(time.Second)
	store.now = func() time.Time { return now }

	rec := mustRequest(t, store, Request{OperationID: "op", Method: "GET", FinalURL: "https://example.com"})

	now = now.Add(2 * time.Second)
	_, err := store.Confirm(context.Background(), rec.ID, true)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}

	_, err = store.Confirm(context.Background(), rec.ID, true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after expired record cleanup, got %v", err)
	}
}

func mustRequest(t *testing.T, store *MemoryStore, req Request) Record {
	t.Helper()
	rec, err := store.Request(context.Background(), req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if rec.ID == "" {
		t.Fatalf("request returned empty confirmation ID")
	}
	return rec
}

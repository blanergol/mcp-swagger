package resource

import (
	"context"
	"errors"
	"testing"
)

type testStore struct {
	descriptors []Descriptor
	item        Item
	listErr     error
	getErr      error
}

func (s *testStore) List(context.Context) ([]Descriptor, error) { return s.descriptors, s.listErr }
func (s *testStore) Get(context.Context, string) (Item, error)  { return s.item, s.getErr }

// TestCompositeStoreListDeduplicatesByContractKey protects list dedup invariants.
func TestCompositeStoreListDeduplicatesByContractKey(t *testing.T) {
	t.Parallel()

	s1 := &testStore{descriptors: []Descriptor{
		{ID: "id-a", URI: "resource://a", Name: "a-first"},
		{ID: "id-tpl", URITemplate: "resource://tpl/{id}", Name: "tpl-first"},
		{ID: "id-only", Name: "id-only-first"},
	}}
	s2 := &testStore{descriptors: []Descriptor{
		{ID: "id-a-2", URI: "resource://a", Name: "a-second"},
		{ID: "id-tpl-2", URITemplate: "resource://tpl/{id}", Name: "tpl-second"},
		{ID: "id-only", Name: "id-only-second"},
		{ID: "id-b", URI: "resource://b", Name: "b-second"},
	}}

	store := NewCompositeStore(s1, s2)
	out, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list returned unexpected error: %v", err)
	}

	if len(out) != 4 {
		t.Fatalf("expected deduplicated 4 descriptors, got %d: %#v", len(out), out)
	}
	if out[0].Name != "a-first" || out[1].Name != "tpl-first" || out[2].Name != "id-only-first" {
		t.Fatalf("expected first-store descriptors to win duplicates, got %#v", out)
	}
}

// TestCompositeStoreGetSelectionAndErrorHandling protects get fallback/error behavior.
func TestCompositeStoreGetSelectionAndErrorHandling(t *testing.T) {
	t.Parallel()

	t.Run("returns first found after not found", func(t *testing.T) {
		store := NewCompositeStore(
			&testStore{getErr: ErrNotFound},
			&testStore{item: Item{Text: "ok"}},
		)
		got, err := store.Get(context.Background(), "x")
		if err != nil {
			t.Fatalf("expected successful fallback get, got %v", err)
		}
		if got.Text != "ok" {
			t.Fatalf("unexpected item from fallback store: %#v", got)
		}
	})

	t.Run("returns non-notfound error immediately", func(t *testing.T) {
		expected := errors.New("backend down")
		store := NewCompositeStore(
			&testStore{getErr: ErrNotFound},
			&testStore{getErr: expected},
			&testStore{item: Item{Text: "must-not-reach"}},
		)
		_, err := store.Get(context.Background(), "x")
		if !errors.Is(err, expected) {
			t.Fatalf("expected backend error passthrough, got %v", err)
		}
	})

	t.Run("empty composite returns not found", func(t *testing.T) {
		store := NewCompositeStore()
		_, err := store.Get(context.Background(), "x")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}
